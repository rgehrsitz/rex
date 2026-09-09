// rex/cmd/rexd/main.go

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"

	"rgehrsitz/rex/pkg/eventcontext"
	"rgehrsitz/rex/pkg/logging"
	"rgehrsitz/rex/pkg/observability"
	"rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"
	"rgehrsitz/rex/pkg/tooling"
)

// Config represents the application configuration
type Config struct {
	BytecodeFile            string
	LogLevel                string
	LogDestination          string
	LogTimeFormat           string
	TraceConditions         bool
	RedisAddress            string
	RedisUsername           string
	RedisPassword           string
	RedisDB                 int
	RedisChannels           []string
	RedisEventMode          string
	RedisDurable            store.DurableOptions
	RedisRetryBackoff       time.Duration
	RedisTLSEnabled         bool
	RedisTLSServerName      string
	RedisTLSCAFile          string
	RedisConnectTimeout     time.Duration
	RedisHealthInterval     time.Duration
	RedisHealthTimeout      time.Duration
	PriorityThreshold       int
	ScriptsEnabled          bool
	AllowLegacyV3           bool
	BatchLimits             runtime.Limits
	MaxActionsPerEvaluation int
	MaxEventHops            int
	ObservabilityEnabled    bool
	ObservabilityAddress    string
}

// RexDependencies represents the external dependencies of the application
type RexDependencies struct {
	Store  store.ContextStore
	Engine *runtime.Engine
}

// StoreFactory is an interface for creating a store
type StoreFactory interface {
	NewStore(context.Context, store.RedisOptions) (store.ContextStore, error)
}

// EngineFactory is an interface for creating an engine
type EngineFactory interface {
	NewEngine(bytecodeFile string, store store.ContextStore, priorityThreshold int) (*runtime.Engine, error)
}

type factUpdateProcessor interface {
	ProcessFactUpdateContext(ctx context.Context, factName string, factValue interface{}) error
}

type connectivityChecker interface {
	Ping(context.Context) error
}

type durableStore interface {
	OpenDurable(context.Context, store.DurableOptions) (*store.RedisDurable, error)
}

type durableOwnershipRenewer interface {
	OwnershipRenewInterval() time.Duration
	RenewOwnership(context.Context) error
}

var messageTraceSequence atomic.Uint64

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--dry-run" {
		os.Exit(tooling.RunCLI(context.Background(), append([]string{"simulate"}, os.Args[2:]...), os.Stdout, os.Stderr))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := run(ctx, os.Args, &RealStoreFactory{}, &RealEngineFactory{}); err != nil {
		logging.Logger.Fatal().Err(err).Msg("Application failed")
	}
}

func run(ctx context.Context, args []string, storeFactory StoreFactory, engineFactory EngineFactory) error {
	config, err := parseConfig(args)
	if err != nil {
		return fmt.Errorf("failed to parse configuration: %w", err)
	}

	if err := logging.ConfigureLogger(config.LogLevel, config.LogDestination); err != nil {
		return fmt.Errorf("failed to configure logger: %w", err)
	}

	deps, err := setupDependencies(ctx, config, storeFactory, engineFactory)
	if err != nil {
		return fmt.Errorf("failed to setup dependencies: %w", err)
	}
	defer deps.Store.Close()
	defer deps.Engine.Shutdown()

	metrics := observability.NewMetrics()
	deps.Engine.SetExecutionObserver(metrics)
	return runMainLoopWithObservability(ctx, deps, config, metrics)
}

func parseConfig(args []string) (*Config, error) {
	configFile := flag.String("config", "", "Path to configuration file")
	flag.CommandLine.Parse(args[1:])

	viper.SetConfigType("json")
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.output", "console")
	viper.SetDefault("logging.time_format", "unixnano")
	viper.SetDefault("logging.trace_conditions", true)
	viper.SetDefault("redis.address", "localhost:6379")
	viper.SetDefault("redis.database", 0)
	viper.SetDefault("redis.channels", []string{"rex_updates"})
	viper.SetDefault("redis.event_mode", "pubsub")
	viper.SetDefault("redis.durable.output_stream", "rex_results_stream")
	viper.SetDefault("redis.durable.dead_letter_stream", "rex_dead_letter")
	viper.SetDefault("redis.durable.namespace", "default")
	viper.SetDefault("redis.durable.claim_idle", "30s")
	viper.SetDefault("redis.durable.block", "1s")
	viper.SetDefault("redis.durable.journal_ttl", "168h")
	viper.SetDefault("redis.durable.max_attempts", 5)
	viper.SetDefault("redis.durable.output_max_len", 100000)
	viper.SetDefault("redis.durable.dead_letter_max_len", 10000)
	viper.SetDefault("redis.durable.lock_ttl", "30s")
	viper.SetDefault("redis.durable.retry_backoff", "250ms")
	viper.SetDefault("redis.tls.enabled", false)
	viper.SetDefault("redis.connect_timeout", "5s")
	viper.SetDefault("redis.health_check_interval", "1s")
	viper.SetDefault("redis.health_check_timeout", "500ms")
	viper.SetDefault("engine.priority_threshold", 1)
	viper.SetDefault("engine.scripts_enabled", false)
	viper.SetDefault("engine.allow_legacy_v3", false)
	defaults := runtime.DefaultLimits()
	viper.SetDefault("engine.batch.event_bytes", defaults.EventBytes)
	viper.SetDefault("engine.batch.event_facts", defaults.EventFacts)
	viper.SetDefault("engine.batch.snapshot_bytes", defaults.SnapshotBytes)
	viper.SetDefault("engine.batch.actions_per_round", defaults.ActionsPerRound)
	viper.SetDefault("engine.batch.chain_actions", defaults.ChainActions)
	viper.SetDefault("engine.batch.chain_work", defaults.ChainWork)
	viper.SetDefault("engine.batch.rounds", defaults.Rounds)
	viper.SetDefault("engine.batch.staged_bytes", defaults.StagedBytes)
	viper.SetDefault("engine.max_actions_per_evaluation", runtime.DefaultMaxActionsPerEvaluation)
	viper.SetDefault("engine.max_event_hops", 16)
	viper.SetDefault("observability.enabled", false)
	viper.SetDefault("observability.address", "127.0.0.1:8080")
	viper.SetEnvPrefix("REX")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if *configFile == "" {
		viper.SetConfigName("rex_config")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.rex")
		viper.AddConfigPath("/etc/rex")
	} else {
		viper.SetConfigFile(*configFile)
	}

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok || *configFile != "" {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		logging.Logger.Info().Msg("No configuration file found, using defaults")
	}

	config := &Config{
		BytecodeFile:    viper.GetString("bytecode_file"),
		LogLevel:        viper.GetString("logging.level"),
		LogDestination:  viper.GetString("logging.output"),
		LogTimeFormat:   viper.GetString("logging.time_format"),
		TraceConditions: viper.GetBool("logging.trace_conditions"),
		RedisAddress:    viper.GetString("redis.address"),
		RedisUsername:   viper.GetString("redis.username"),
		RedisPassword:   viper.GetString("redis.password"),
		RedisDB:         viper.GetInt("redis.database"),
		RedisChannels:   configStringSlice("redis.channels"),
		RedisEventMode:  strings.ToLower(viper.GetString("redis.event_mode")),
		RedisDurable: store.DurableOptions{
			Stream: viper.GetString("redis.durable.stream"), Group: viper.GetString("redis.durable.group"),
			Consumer: viper.GetString("redis.durable.consumer"), OutputStream: viper.GetString("redis.durable.output_stream"),
			DeadLetter: viper.GetString("redis.durable.dead_letter_stream"), Namespace: viper.GetString("redis.durable.namespace"),
			ClaimIdle: viper.GetDuration("redis.durable.claim_idle"), Block: viper.GetDuration("redis.durable.block"),
			JournalTTL: viper.GetDuration("redis.durable.journal_ttl"), MaxAttempts: viper.GetInt64("redis.durable.max_attempts"),
			OutputMaxLen: viper.GetInt64("redis.durable.output_max_len"), DeadMaxLen: viper.GetInt64("redis.durable.dead_letter_max_len"),
			LockTTL: viper.GetDuration("redis.durable.lock_ttl"),
		},
		RedisRetryBackoff:       viper.GetDuration("redis.durable.retry_backoff"),
		RedisTLSEnabled:         viper.GetBool("redis.tls.enabled"),
		RedisTLSServerName:      viper.GetString("redis.tls.server_name"),
		RedisTLSCAFile:          viper.GetString("redis.tls.ca_file"),
		RedisConnectTimeout:     viper.GetDuration("redis.connect_timeout"),
		RedisHealthInterval:     viper.GetDuration("redis.health_check_interval"),
		RedisHealthTimeout:      viper.GetDuration("redis.health_check_timeout"),
		PriorityThreshold:       viper.GetInt("engine.priority_threshold"),
		ScriptsEnabled:          viper.GetBool("engine.scripts_enabled"),
		AllowLegacyV3:           viper.GetBool("engine.allow_legacy_v3"),
		BatchLimits:             runtime.Limits{EventBytes: viper.GetInt("engine.batch.event_bytes"), EventFacts: viper.GetInt("engine.batch.event_facts"), SnapshotBytes: viper.GetInt("engine.batch.snapshot_bytes"), ActionsPerRule: viper.GetInt("engine.max_actions_per_evaluation"), ActionsPerRound: viper.GetInt("engine.batch.actions_per_round"), ChainActions: viper.GetInt("engine.batch.chain_actions"), ChainWork: viper.GetInt("engine.batch.chain_work"), Rounds: viper.GetInt("engine.batch.rounds"), StagedBytes: viper.GetInt("engine.batch.staged_bytes")},
		MaxActionsPerEvaluation: viper.GetInt("engine.max_actions_per_evaluation"),
		MaxEventHops:            viper.GetInt("engine.max_event_hops"),
		ObservabilityEnabled:    viper.GetBool("observability.enabled"),
		ObservabilityAddress:    viper.GetString("observability.address"),
	}
	if config.RedisAddress == "" {
		return nil, fmt.Errorf("redis.address is required")
	}
	if config.RedisEventMode != "pubsub" && config.RedisEventMode != "streams" {
		return nil, fmt.Errorf("redis.event_mode must be pubsub or streams")
	}
	if config.RedisEventMode == "pubsub" && len(config.RedisChannels) == 0 {
		return nil, fmt.Errorf("redis.channels must contain at least one channel")
	}
	if config.RedisEventMode == "streams" {
		if config.RedisDurable.Stream == "" || config.RedisDurable.Group == "" || config.RedisDurable.Consumer == "" {
			return nil, fmt.Errorf("redis durable stream, group, and consumer are required in streams mode")
		}
		if config.RedisRetryBackoff <= 0 {
			return nil, fmt.Errorf("redis.durable.retry_backoff must be greater than zero")
		}
	}
	if config.RedisConnectTimeout <= 0 {
		return nil, fmt.Errorf("redis.connect_timeout must be greater than zero")
	}
	if config.RedisHealthInterval <= 0 || config.RedisHealthTimeout <= 0 {
		return nil, fmt.Errorf("Redis health check interval and timeout must be greater than zero")
	}
	return config, nil
}

func configStringSlice(key string) []string {
	values := viper.GetStringSlice(key)
	result := make([]string, 0, len(values))
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				result = append(result, item)
			}
		}
	}
	return result
}

func setupDependencies(ctx context.Context, config *Config, storeFactory StoreFactory, engineFactory EngineFactory) (*RexDependencies, error) {
	// Reject the retired capability before allocating a store or engine.
	if config.ScriptsEnabled {
		return nil, fmt.Errorf("scripts are no longer supported; migrate to declarative v4 rules")
	}
	tlsConfig, err := buildRedisTLSConfig(config)
	if err != nil {
		return nil, err
	}
	connectTimeout := config.RedisConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 5 * time.Second
	}
	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	redisStore, err := storeFactory.NewStore(connectCtx, store.RedisOptions{
		Addr: config.RedisAddress, Username: config.RedisUsername, Password: config.RedisPassword,
		DB: config.RedisDB, TLSConfig: tlsConfig, DialTimeout: connectTimeout,
		ReadTimeout: connectTimeout, WriteTimeout: connectTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize store: %w", err)
	}

	engine, err := engineFactory.NewEngine(config.BytecodeFile, redisStore, config.PriorityThreshold)
	if err != nil {
		_ = redisStore.Close()
		return nil, fmt.Errorf("failed to initialize engine: %w", err)
	}
	if engine.BytecodeVersion() == 3 && !config.AllowLegacyV3 {
		_ = redisStore.Close()
		return nil, fmt.Errorf("v3 requires engine.allow_legacy_v3; recompile for v4")
	}
	if config.RedisEventMode == "streams" && engine.BytecodeVersion() != 4 {
		_ = redisStore.Close()
		return nil, fmt.Errorf("Redis Streams durable processing requires a v4 artifact")
	}
	if engine.BytecodeVersion() == 4 {
		if err := engine.SetBatchLimits(config.BatchLimits); err != nil {
			_ = redisStore.Close()
			return nil, err
		}
	}
	if err := engine.SetScriptsEnabled(config.ScriptsEnabled); err != nil {
		_ = redisStore.Close()
		return nil, err
	}
	engine.SetConditionTracing(config.TraceConditions)
	if config.MaxActionsPerEvaluation <= 0 {
		_ = redisStore.Close()
		return nil, fmt.Errorf("engine.max_actions_per_evaluation must be greater than zero")
	}
	if config.MaxEventHops < 0 {
		_ = redisStore.Close()
		return nil, fmt.Errorf("engine.max_event_hops must be zero or greater")
	}
	engine.SetMaxActionsPerEvaluation(config.MaxActionsPerEvaluation)

	return &RexDependencies{
		Store:  redisStore,
		Engine: engine,
	}, nil
}

func buildRedisTLSConfig(config *Config) (*tls.Config, error) {
	if !config.RedisTLSEnabled {
		if config.RedisTLSServerName != "" || config.RedisTLSCAFile != "" {
			return nil, fmt.Errorf("redis.tls.enabled must be true when TLS server_name or ca_file is configured")
		}
		return nil, nil
	}
	serverName := config.RedisTLSServerName
	if serverName == "" {
		var err error
		serverName, _, err = net.SplitHostPort(config.RedisAddress)
		if err != nil {
			return nil, fmt.Errorf("derive Redis TLS server name from address %q: %w", config.RedisAddress, err)
		}
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	if config.RedisTLSCAFile == "" {
		return tlsConfig, nil
	}
	pem, err := os.ReadFile(config.RedisTLSCAFile)
	if err != nil {
		return nil, fmt.Errorf("read Redis TLS CA file: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("Redis TLS CA file contains no valid certificates")
	}
	tlsConfig.RootCAs = roots
	return tlsConfig, nil
}

func runMainLoop(ctx context.Context, deps *RexDependencies, config *Config) error {
	return runMainLoopWithObservability(ctx, deps, config, nil)
}

func runMainLoopWithObservability(ctx context.Context, deps *RexDependencies, config *Config, metrics *observability.Metrics) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if metrics == nil {
		metrics = observability.NewMetrics()
	}
	deps.Engine.SetExecutionObserver(metrics)

	server, err := startObservabilityServer(config, metrics)
	if err != nil {
		return err
	}
	if server != nil {
		defer shutdownObservabilityServer(server)
	}
	checker, ok := deps.Store.(connectivityChecker)
	if !ok {
		return fmt.Errorf("store does not provide connectivity checks")
	}
	if config.RedisEventMode == "streams" {
		return runDurableMainLoop(ctx, deps, config, metrics, checker)
	}

	subscriber, ok := deps.Store.(store.EventSubscriber)
	if !ok {
		return fmt.Errorf("store does not provide an EventSubscriber")
	}
	source, err := subscriber.OpenEvents(ctx, config.RedisChannels...)
	if err != nil {
		return fmt.Errorf("failed to subscribe to Redis channels: %w", err)
	}
	defer source.Close()
	healthTimeout := config.RedisHealthTimeout
	if healthTimeout <= 0 {
		healthTimeout = 500 * time.Millisecond
	}
	healthCtx, cancelHealth := context.WithTimeout(ctx, healthTimeout)
	err = checker.Ping(healthCtx)
	cancelHealth()
	if err != nil {
		return fmt.Errorf("Redis readiness check after subscription: %w", err)
	}
	metrics.SetRedisReady(true)
	metrics.SetSubscriptionReady(true)
	defer func() {
		metrics.SetSubscriptionReady(false)
		metrics.SetRedisReady(false)
	}()
	monitorCtx, stopMonitor := context.WithCancel(ctx)
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		monitorRedisConnectivity(monitorCtx, checker, metrics, config.RedisHealthInterval, config.RedisHealthTimeout)
	}()
	defer func() {
		stopMonitor()
		<-monitorDone
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	logging.Logger.Info().Msg("REX runtime engine started")
	return consumeEvents(ctx, deps.Engine, source.Events(), sigChan, metrics, config.MaxEventHops)
}

func runDurableMainLoop(ctx context.Context, deps *RexDependencies, config *Config, metrics *observability.Metrics, checker connectivityChecker) error {
	opener, ok := deps.Store.(durableStore)
	if !ok {
		return fmt.Errorf("store does not provide durable Redis Streams processing")
	}
	queue, err := opener.OpenDurable(ctx, config.RedisDurable)
	if err != nil {
		return fmt.Errorf("open durable Redis stream: %w", err)
	}
	if err := queue.AcquireOwnership(ctx); err != nil {
		return fmt.Errorf("acquire durable partition: %w", err)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), config.RedisHealthTimeout)
		defer cancel()
		if err := queue.ReleaseOwnership(releaseCtx); err != nil {
			logging.Logger.Error().Err(err).Msg("Failed to release durable partition")
		}
	}()

	healthCtx, cancelHealth := context.WithTimeout(ctx, config.RedisHealthTimeout)
	err = checker.Ping(healthCtx)
	cancelHealth()
	if err != nil {
		return fmt.Errorf("Redis readiness check after opening durable stream: %w", err)
	}
	metrics.SetRedisReady(true)
	metrics.SetSubscriptionReady(true)
	defer func() {
		metrics.SetSubscriptionReady(false)
		metrics.SetRedisReady(false)
	}()
	if stats, err := queue.Stats(ctx); err == nil {
		metrics.SetDurableBacklog(stats.Pending, stats.Lag)
	}
	statsInterval := config.RedisHealthInterval
	if statsInterval <= 0 {
		statsInterval = time.Second
	}
	nextStats := time.Now().Add(statsInterval)

	processCtx, stopProcessing := context.WithCancel(ctx)
	defer stopProcessing()
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		monitorRedisConnectivity(processCtx, checker, metrics, config.RedisHealthInterval, config.RedisHealthTimeout)
	}()
	defer func() {
		stopProcessing()
		<-monitorDone
	}()
	leaseErr := make(chan error, 1)
	go monitorDurableOwnership(processCtx, stopProcessing, queue, leaseErr)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)
	logging.Logger.Info().Str("stream", config.RedisDurable.Stream).Str("group", config.RedisDurable.Group).Msg("REX durable runtime engine started")

	for {
		select {
		case <-sigChan:
			return nil
		case err := <-leaseErr:
			metrics.SetSubscriptionReady(false)
			return fmt.Errorf("durable partition lease lost: %w", err)
		case <-processCtx.Done():
			select {
			case err := <-leaseErr:
				return fmt.Errorf("durable partition lease lost: %w", err)
			default:
				return nil
			}
		default:
		}

		started := time.Now()
		result, processErr := deps.Engine.ProcessNextDurable(processCtx, queue)
		if processCtx.Err() != nil {
			continue
		}
		if result.EventID != "" {
			metrics.RecordEvent(time.Since(started), processErr)
		}
		if result.RetryPending {
			metrics.RecordDurableRetry()
		}
		if result.Recovered {
			metrics.RecordDurableRecovery()
		}
		if result.DeadLettered {
			metrics.RecordDurableDeadLetter()
		}
		if !time.Now().Before(nextStats) {
			if stats, err := queue.Stats(processCtx); err == nil {
				metrics.SetDurableBacklog(stats.Pending, stats.Lag)
			}
			nextStats = time.Now().Add(statsInterval)
		}
		if processErr == nil {
			metrics.SetSubscriptionReady(true)
			continue
		}
		if errors.Is(processErr, store.ErrDurableProgramMismatch) || errors.Is(processErr, store.ErrDurableReconciliation) {
			metrics.SetSubscriptionReady(false)
			return processErr
		}
		if result.EventID == "" || errors.Is(processErr, store.ErrDurableInfrastructure) {
			metrics.SetSubscriptionReady(false)
			metrics.RecordEventSourceError()
		}
		logging.Logger.Error().Err(processErr).Str("input_id", result.EventID).Int64("attempt", result.Attempts).Msg("Durable event processing failed")
		timer := time.NewTimer(config.RedisRetryBackoff)
		select {
		case <-processCtx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func monitorDurableOwnership(ctx context.Context, cancel context.CancelFunc, queue durableOwnershipRenewer, failures chan<- error) {
	interval := queue.OwnershipRenewInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := queue.RenewOwnership(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				select {
				case failures <- err:
				case <-ctx.Done():
					return
				}
				cancel()
				return
			}
		}
	}
}

func startObservabilityServer(config *Config, metrics *observability.Metrics) (*http.Server, error) {
	if !config.ObservabilityEnabled {
		return nil, nil
	}

	listener, err := net.Listen("tcp", config.ObservabilityAddress)
	if err != nil {
		return nil, fmt.Errorf("listen for observability endpoints on %s: %w", config.ObservabilityAddress, err)
	}

	server := &http.Server{
		Addr:              listener.Addr().String(),
		Handler:           metrics.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logging.Logger.Error().Err(err).Str("address", config.ObservabilityAddress).Msg("Observability server stopped unexpectedly")
		}
	}()

	logging.Logger.Info().Str("address", config.ObservabilityAddress).Msg("Observability endpoints enabled")
	return server, nil
}

func shutdownObservabilityServer(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logging.Logger.Error().Err(err).Msg("Failed to stop observability server")
	}
}

func consumeMessages(ctx context.Context, engine *runtime.Engine, messages <-chan *redis.Message, signals <-chan os.Signal) error {
	return consumeMessagesWithOptions(ctx, engine, messages, signals, nil, 16)
}

func consumeMessagesWithMetrics(ctx context.Context, engine *runtime.Engine, messages <-chan *redis.Message, signals <-chan os.Signal, metrics *observability.Metrics) error {
	return consumeMessagesWithOptions(ctx, engine, messages, signals, metrics, 16)
}

func consumeMessagesWithOptions(ctx context.Context, engine *runtime.Engine, messages <-chan *redis.Message, signals <-chan os.Signal, metrics *observability.Metrics, maxEventHops int) error {
	for {
		select {
		case msg, ok := <-messages:
			if !ok {
				return nil
			}
			if msg == nil {
				continue
			}
			started := time.Now()
			err := processMessageWithMaxEventHops(ctx, engine, msg, maxEventHops)
			if metrics != nil {
				metrics.RecordEvent(time.Since(started), err)
			}
			if err != nil {
				logging.Logger.Error().Err(err).Msg("Failed to process message")
			}
		case <-signals:
			logging.Logger.Info().Msg("Shutting down REX runtime engine")
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

func processMessage(ctx context.Context, engine factUpdateProcessor, msg *redis.Message) error {
	return processMessageWithMaxEventHops(ctx, engine, msg, 16)
}

func processMessageWithMaxEventHops(ctx context.Context, engine factUpdateProcessor, msg *redis.Message, maxEventHops int) error {
	if len(msg.Payload) > store.MaxEventBytes {
		return fmt.Errorf("event exceeds byte limit")
	}
	facts, metadata, enveloped, err := eventcontext.DecodeFactEvent([]byte(msg.Payload))
	if err != nil {
		if json.Valid([]byte(msg.Payload)) {
			return fmt.Errorf("invalid JSON fact event: %w", err)
		}
		return processLegacyMessage(ctx, engine, msg)
	}
	if batch, ok := engine.(*runtime.Engine); ok && batch.BytecodeVersion() == 4 && metadata.Kind == "committed_output" {
		return nil
	}
	if metadata.Kind != "" {
		return fmt.Errorf("unsupported event kind %q", metadata.Kind)
	}
	if !enveloped {
		metadata = eventcontext.Metadata{TraceID: nextMessageTraceID()}
	}
	if metadata.Hop > maxEventHops {
		return fmt.Errorf("event %q exceeded maximum hop count of %d", metadata.TraceID, maxEventHops)
	}

	ctx = eventcontext.WithMetadata(ctx, metadata)
	traceID := metadata.TraceID
	keys := sortedKeys(facts)
	format := "json"
	if enveloped {
		format = "rex_envelope"
	}

	logging.Logger.Info().
		Str("trace_id", traceID).
		Int("event_hop", metadata.Hop).
		Str("event", "fact_event_received").
		Str("channel", msg.Channel).
		Msg("Received fact event")
	logging.Logger.Info().
		Str("trace_id", traceID).
		Int("event_hop", metadata.Hop).
		Str("event", "fact_event_decoded").
		Str("format", format).
		Strs("fact_names", keys).
		Msg("Decoded fact event")

	if batch, ok := engine.(interface {
		ProcessBatchContext(context.Context, map[string]interface{}) error
	}); ok {
		return batch.ProcessBatchContext(ctx, facts)
	}
	for _, key := range keys {
		if err := engine.ProcessFactUpdateContext(ctx, key, facts[key]); err != nil {
			logging.Logger.Error().
				Err(err).
				Str("trace_id", traceID).
				Str("event", "fact_update_failed").
				Str("fact_name", key).
				Msg("Failed to process fact update")
			return err
		}
	}
	return nil
}

func processLegacyMessage(ctx context.Context, engine factUpdateProcessor, msg *redis.Message) error {
	ctx = runtime.WithTraceID(ctx, nextMessageTraceID())
	traceID := runtime.TraceIDFromContext(ctx)

	logging.Logger.Info().
		Str("trace_id", traceID).
		Int("event_hop", runtime.EventHopFromContext(ctx)).
		Str("event", "fact_event_received").
		Str("channel", msg.Channel).
		Msg("Received fact event")

	// Accept legacy key=value messages during the JSON-event migration.
	parts := strings.SplitN(msg.Payload, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid payload format: %s", msg.Payload)
	}

	key := parts[0]
	value := parts[1]

	var typedValue interface{}
	if err := json.Unmarshal([]byte(value), &typedValue); err != nil {
		if number, err := strconv.ParseFloat(value, 64); err == nil {
			typedValue = number
		} else {
			typedValue = value
		}
	}

	logging.Logger.Info().
		Str("trace_id", traceID).
		Str("event", "fact_event_decoded").
		Str("format", "legacy").
		Strs("fact_names", []string{key}).
		Msg("Decoded fact event")

	if err := engine.ProcessFactUpdateContext(ctx, key, typedValue); err != nil {
		logging.Logger.Error().
			Err(err).
			Str("trace_id", traceID).
			Str("event", "fact_update_failed").
			Str("fact_name", key).
			Msg("Failed to process fact update")
		return err
	}

	return nil
}

func nextMessageTraceID() string {
	return fmt.Sprintf("event-%d", messageTraceSequence.Add(1))
}

func sortedKeys(values map[string]interface{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// RealStoreFactory implements StoreFactory
type RealStoreFactory struct{}

func (f *RealStoreFactory) NewStore(ctx context.Context, options store.RedisOptions) (store.ContextStore, error) {
	return store.NewRedisStore(ctx, options)
}

// RealEngineFactory implements EngineFactory
type RealEngineFactory struct{}

func (f *RealEngineFactory) NewEngine(bytecodeFile string, store store.ContextStore, priorityThreshold int) (*runtime.Engine, error) {
	return runtime.NewEngineFromFile(bytecodeFile, store, priorityThreshold)
}

func consumeEvents(ctx context.Context, engine *runtime.Engine, events <-chan store.Event, signals <-chan os.Signal, metrics *observability.Metrics, maxHops int) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-signals:
			return nil
		case event, ok := <-events:
			if !ok {
				return nil
			}
			if event.State == store.SubscriptionDisconnected {
				if metrics != nil {
					if metrics.SetSubscriptionReady(false) {
						metrics.RecordRedisDisconnect()
					}
					metrics.RecordEventSourceError()
				}
				logging.Logger.Error().Err(event.Err).Msg("Redis event subscription disconnected")
				continue
			}
			if event.State == store.SubscriptionConnected {
				if metrics != nil && metrics.SetSubscriptionReady(true) {
					metrics.RecordRedisReconnect()
				}
				logging.Logger.Info().Msg("Redis event subscription restored")
				continue
			}
			started := time.Now()
			err := event.Err
			if err == nil {
				err = processMessageWithMaxEventHops(ctx, engine, &redis.Message{Channel: event.Channel, Payload: event.Payload}, maxHops)
			}
			if metrics != nil {
				metrics.RecordEvent(time.Since(started), err)
			}
			if err != nil {
				logging.Logger.Error().Err(err).Msg("Failed to process event")
			}
			if errors.Is(err, runtime.ErrReconciliationRequired) {
				return err
			}
		}
	}
}

func monitorRedisConnectivity(ctx context.Context, checker connectivityChecker, metrics *observability.Metrics, interval, timeout time.Duration) {
	if metrics == nil {
		return
	}
	if interval <= 0 {
		interval = time.Second
	}
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkCtx, cancel := context.WithTimeout(ctx, timeout)
			err := checker.Ping(checkCtx)
			cancel()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				if metrics.SetRedisReady(false) {
					metrics.RecordRedisDisconnect()
					logging.Logger.Error().Err(err).Msg("Redis connectivity check failed")
				}
				continue
			}
			if metrics.SetRedisReady(true) {
				metrics.RecordRedisReconnect()
				logging.Logger.Info().Msg("Redis connectivity restored")
			}
		}
	}
}
