// rex/cmd/rexd/main.go

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"

	"rgehrsitz/rex/pkg/logging"
	"rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"
)

// Config represents the application configuration
type Config struct {
	BytecodeFile      string
	LogLevel          string
	LogDestination    string
	LogTimeFormat     string
	RedisAddress      string
	RedisPassword     string
	RedisDB           int
	RedisChannels     []string
	PriorityThreshold int
	ScriptsEnabled    bool
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

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := run(ctx, os.Args, &RealStoreFactory{}, &RealEngineFactory{}); err != nil {
		log.Fatal().Err(err).Msg("Application failed")
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

	return runMainLoop(ctx, deps, config)
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
		log.Info().Msg("No configuration file found, using defaults")
	}

	return &Config{
		BytecodeFile:      viper.GetString("bytecode_file"),
		LogLevel:          viper.GetString("logging.level"),
		LogDestination:    viper.GetString("logging.output"),
		LogTimeFormat:     viper.GetString("logging.time_format"),
		RedisAddress:      viper.GetString("redis.address"),
		RedisPassword:     viper.GetString("redis.password"),
		RedisDB:           viper.GetInt("redis.database"),
		RedisChannels:     viper.GetStringSlice("redis.channels"),
		PriorityThreshold: viper.GetInt("engine.priority_threshold"),
		ScriptsEnabled:    viper.GetBool("engine.scripts_enabled"),
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

	return &RexDependencies{
		Store:  store,
		Engine: engine,
	}, nil
}

func runMainLoop(ctx context.Context, deps *RexDependencies, config *Config) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	redisStore, ok := deps.Store.(*store.RedisStore)
	if !ok {
		return fmt.Errorf("store is not a RedisStore")
	}

	pubsub, err := redisStore.Subscribe(ctx, config.RedisChannels...)
	if err != nil {
		return fmt.Errorf("failed to subscribe to Redis channels: %w", err)
	}
	defer pubsub.Close()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	log.Info().Msg("REX runtime engine started")
	return consumeMessages(ctx, deps.Engine, pubsub.Channel(), sigChan)
}

func consumeMessages(ctx context.Context, engine *runtime.Engine, messages <-chan *redis.Message, signals <-chan os.Signal) error {
	for {
		select {
		case msg, ok := <-messages:
			if !ok {
				return nil
			}
			if msg == nil {
				continue
			}
			if err := processMessage(ctx, engine, msg); err != nil {
				log.Error().Err(err).Msg("Failed to process message")
			}
		case <-signals:
			log.Info().Msg("Shutting down REX runtime engine")
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

func processMessage(ctx context.Context, engine *runtime.Engine, msg *redis.Message) error {
	log.Info().Str("channel", msg.Channel).Str("payload", msg.Payload).Msg("Received message")

	// Try to parse the payload as JSON
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(msg.Payload), &jsonData); err == nil {
		// Handle JSON payload
		keys := sortedKeys(jsonData)
		for _, key := range keys {
			value := jsonData[key]
			// Process each key-value pair in the JSON object
			if err := engine.ProcessFactUpdateContext(ctx, key, value); err != nil {
				return err
			}
		}
		return nil
	}

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

	return engine.ProcessFactUpdateContext(ctx, key, typedValue)
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
