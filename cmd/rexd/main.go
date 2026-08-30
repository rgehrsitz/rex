// rex/cmd/rexd/main.go

package main

import (
	"context"
	"encoding/json"
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
)

// Config represents the application configuration
type Config struct {
	BytecodeFile            string
	LogLevel                string
	LogDestination          string
	LogTimeFormat           string
	RedisAddress            string
	RedisPassword           string
	RedisDB                 int
	RedisChannels           []string
	PriorityThreshold       int
	ScriptsEnabled          bool
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
	NewStore(addr, password string, db int) store.ContextStore
}

// EngineFactory is an interface for creating an engine
type EngineFactory interface {
	NewEngine(bytecodeFile string, store store.ContextStore, priorityThreshold int) (*runtime.Engine, error)
}

type factUpdateProcessor interface {
	ProcessFactUpdateContext(ctx context.Context, factName string, factValue interface{}) error
}

var messageTraceSequence atomic.Uint64

func main() {
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

	deps, err := setupDependencies(config, storeFactory, engineFactory)
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
	viper.SetDefault("redis.address", "localhost:6379")
	viper.SetDefault("redis.database", 0)
	viper.SetDefault("redis.channels", []string{"rex_updates"})
	viper.SetDefault("engine.priority_threshold", 1)
	viper.SetDefault("engine.scripts_enabled", false)
	viper.SetDefault("engine.max_actions_per_evaluation", runtime.DefaultMaxActionsPerEvaluation)
	viper.SetDefault("engine.max_event_hops", 16)
	viper.SetDefault("observability.enabled", false)
	viper.SetDefault("observability.address", "127.0.0.1:8080")

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

	return &Config{
		BytecodeFile:            viper.GetString("bytecode_file"),
		LogLevel:                viper.GetString("logging.level"),
		LogDestination:          viper.GetString("logging.output"),
		LogTimeFormat:           viper.GetString("logging.time_format"),
		RedisAddress:            viper.GetString("redis.address"),
		RedisPassword:           viper.GetString("redis.password"),
		RedisDB:                 viper.GetInt("redis.database"),
		RedisChannels:           viper.GetStringSlice("redis.channels"),
		PriorityThreshold:       viper.GetInt("engine.priority_threshold"),
		ScriptsEnabled:          viper.GetBool("engine.scripts_enabled"),
		MaxActionsPerEvaluation: viper.GetInt("engine.max_actions_per_evaluation"),
		MaxEventHops:            viper.GetInt("engine.max_event_hops"),
		ObservabilityEnabled:    viper.GetBool("observability.enabled"),
		ObservabilityAddress:    viper.GetString("observability.address"),
	}, nil
}

func setupDependencies(config *Config, storeFactory StoreFactory, engineFactory EngineFactory) (*RexDependencies, error) {
	store := storeFactory.NewStore(config.RedisAddress, config.RedisPassword, config.RedisDB)

	engine, err := engineFactory.NewEngine(config.BytecodeFile, store, config.PriorityThreshold)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("failed to initialize engine: %w", err)
	}
	engine.SetScriptsEnabled(config.ScriptsEnabled)
	if config.MaxActionsPerEvaluation <= 0 {
		_ = store.Close()
		return nil, fmt.Errorf("engine.max_actions_per_evaluation must be greater than zero")
	}
	if config.MaxEventHops < 0 {
		_ = store.Close()
		return nil, fmt.Errorf("engine.max_event_hops must be zero or greater")
	}
	engine.SetMaxActionsPerEvaluation(config.MaxActionsPerEvaluation)

	return &RexDependencies{
		Store:  store,
		Engine: engine,
	}, nil
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

	redisStore, ok := deps.Store.(*store.RedisStore)
	if !ok {
		return fmt.Errorf("store is not a RedisStore")
	}

	pubsub, err := redisStore.Subscribe(ctx, config.RedisChannels...)
	if err != nil {
		return fmt.Errorf("failed to subscribe to Redis channels: %w", err)
	}
	defer pubsub.Close()
	metrics.SetReady(true)
	defer metrics.SetReady(false)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	logging.Logger.Info().Msg("REX runtime engine started")
	return consumeMessagesWithOptions(ctx, deps.Engine, pubsub.Channel(), sigChan, metrics, config.MaxEventHops)
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
	facts, metadata, enveloped, err := eventcontext.DecodeFactEvent([]byte(msg.Payload))
	if err != nil {
		if json.Valid([]byte(msg.Payload)) {
			return fmt.Errorf("invalid JSON fact event: %w", err)
		}
		return processLegacyMessage(ctx, engine, msg)
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

func (f *RealStoreFactory) NewStore(addr, password string, db int) store.ContextStore {
	return store.NewRedisStore(addr, password, db)
}

// RealEngineFactory implements EngineFactory
type RealEngineFactory struct{}

func (f *RealEngineFactory) NewEngine(bytecodeFile string, store store.ContextStore, priorityThreshold int) (*runtime.Engine, error) {
	return runtime.NewEngineFromFile(bytecodeFile, store, priorityThreshold)
}
