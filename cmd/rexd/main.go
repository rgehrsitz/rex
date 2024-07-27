// rex/cmd/rexd/main.go

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
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
	BytecodeFile                string
	LogLevel                    string
	LogDestination              string
	LogTimeFormat               string
	RedisAddress                string
	RedisPassword               string
	RedisDB                     int
	RedisChannels               []string
	PriorityThreshold           int
	EnablePerformanceMonitoring bool
	EngineInterval              int
}

// RexDependencies represents the external dependencies of the application
type RexDependencies struct {
	Store  store.Store
	Engine *runtime.Engine
}

// StoreFactory is an interface for creating a store
type StoreFactory interface {
	NewStore(addr, password string, db int) store.Store
}

// EngineFactory is an interface for creating an engine
type EngineFactory interface {
	NewEngine(bytecodeFile string, store store.Store, priorityThreshold int, enablePerformanceMonitoring bool) (*runtime.Engine, error)
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
	viper.SetDefault("engine.enable_performance_monitoring", false)
	viper.SetDefault("engine.update_interval", 5)

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
		BytecodeFile:                viper.GetString("bytecode_file"),
		LogLevel:                    viper.GetString("logging.level"),
		LogDestination:              viper.GetString("logging.output"),
		LogTimeFormat:               viper.GetString("logging.time_format"),
		RedisAddress:                viper.GetString("redis.address"),
		RedisPassword:               viper.GetString("redis.password"),
		RedisDB:                     viper.GetInt("redis.database"),
		RedisChannels:               viper.GetStringSlice("redis.channels"),
		PriorityThreshold:           viper.GetInt("engine.priority_threshold"),
		EnablePerformanceMonitoring: viper.GetBool("engine.enable_performance_monitoring"),
		EngineInterval:              viper.GetInt("engine.update_interval"),
	}, nil
}

func setupDependencies(config *Config, storeFactory StoreFactory, engineFactory EngineFactory) (*RexDependencies, error) {
	store := storeFactory.NewStore(config.RedisAddress, config.RedisPassword, config.RedisDB)

	engine, err := engineFactory.NewEngine(config.BytecodeFile, store, config.PriorityThreshold, config.EnablePerformanceMonitoring)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize engine: %w", err)
	}

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

	pubsub := redisStore.Subscribe(config.RedisChannels...)
	defer pubsub.Close()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Info().Msg("REX runtime engine started")

	for {
		select {
		case msg := <-pubsub.Channel():
			if err := processMessage(deps.Engine, msg); err != nil {
				log.Error().Err(err).Msg("Failed to process message")
			}
		case <-sigChan:
			log.Info().Msg("Shutting down REX runtime engine")
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

func processMessage(engine *runtime.Engine, msg *redis.Message) error {
	logging.Logger.Info().Str("channel", msg.Channel).Str("payload", msg.Payload).Msg("Received message")

	parts := strings.Split(msg.Payload, "=")
	if len(parts) != 2 {
		return fmt.Errorf("invalid payload format: %s", msg.Payload)
	}

	key := parts[0]
	value := parts[1]

	var typedValue interface{}
	if value == "true" || value == "false" {
		typedValue = value == "true"
	} else if num, err := strconv.ParseFloat(value, 64); err == nil {
		typedValue = num
	} else {
		typedValue = value
	}

	engine.ProcessFactUpdate(key, typedValue)
	return nil
}

// RealStoreFactory implements StoreFactory
type RealStoreFactory struct{}

func (f *RealStoreFactory) NewStore(addr, password string, db int) store.Store {
	return store.NewRedisStore(addr, password, db)
}

// RealEngineFactory implements EngineFactory
type RealEngineFactory struct{}

func (f *RealEngineFactory) NewEngine(bytecodeFile string, store store.Store, priorityThreshold int, enablePerformanceMonitoring bool) (*runtime.Engine, error) {
	return runtime.NewEngineFromFile(bytecodeFile, store, priorityThreshold, enablePerformanceMonitoring)
}
