// rex/cmd/rexd/main.go

package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"

	"rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"
)

func main() {
	// Define command-line flags
	configFile := flag.String("config", "", "Path to configuration file")
	flag.Parse()

	// Set up Viper
	viper.SetConfigType("json")
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.destination", "console")
	viper.SetDefault("redis.address", "localhost:6379")
	viper.SetDefault("redis.database", 0)
	viper.SetDefault("redis.channels", []string{"rex_updates"})
	viper.SetDefault("engine.update_interval", 5)
	viper.SetDefault("dashboard.enabled", false)
	viper.SetDefault("dashboard.port", 8080)

	// Try to read the default config file if no config file is specified
	if *configFile == "" {
		viper.SetConfigName("rex_config")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.rex")
		viper.AddConfigPath("/etc/rex")
	} else {
		viper.SetConfigFile(*configFile)
	}

	// Read the configuration
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok && *configFile == "" {
			// Config file not found and not explicitly specified, use defaults
			fmt.Println("No configuration file found, using defaults")
		} else {
			// Config file was explicitly specified or error other than file not found
			fmt.Printf("Error reading config file: %s\n", err)
			os.Exit(1)
		}
	}

	// Now you can use viper.GetString(), viper.GetInt(), etc. to get configuration values

	bytecodeFile := viper.GetString("bytecode_file")
	logLevel := viper.GetString("logging.level")
	logDest := viper.GetString("logging.destination")
	redisAddr := viper.GetString("redis.address")
	redisPassword := viper.GetString("redis.password")
	redisDB := viper.GetInt("redis.database")
	redisChannels := viper.GetStringSlice("redis.channels")

	// Todo: enable the following when dash functionaility is ready
	// updateInterval := viper.GetInt("engine.update_interval")
	// dashboardEnabled := viper.GetBool("dashboard.enabled")
	// dashboardPort := viper.GetInt("dashboard.port")

	// Set up logging
	setUpLogging(logLevel, logDest)

	// Initialize Redis store
	redisStore := store.NewRedisStore(redisAddr, redisPassword, redisDB)

	// Initialize engine
	engine, err := runtime.NewEngineFromFile(bytecodeFile, redisStore)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize engine")
	}

	// Set up Redis subscription
	pubsub := redisStore.Subscribe(redisChannels...)
	if pubsub == nil {
		log.Fatal().Msg("Failed to subscribe to Redis channels")
	}
	defer pubsub.Close()

	// // Todo: Start dashboard if enabled
	// if dashboardEnabled {
	// 	go startStatusDashboard(dashboardPort)
	// }

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Main event loop
	log.Info().Msg("REX runtime engine started")
	for {
		select {
		case msg := <-pubsub.Channel():
			log.Info().Str("channel", msg.Channel).Str("payload", msg.Payload).Msg("Received message")
			// Process the message and update facts
			// This is a placeholder - you'll need to implement the actual message processing logic
			engine.ProcessFactUpdate(msg.Channel, msg.Payload)
		case <-sigChan:
			log.Info().Msg("Shutting down REX runtime engine")
			pubsub.Close()
			return
		case <-time.After(5 * time.Second):
			// Periodic tasks, if any
			log.Debug().Msg("Performing periodic tasks")
		}
	}
}

func setUpLogging(level, dest string) {
	// Set log level
	switch level {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	// Set log destination
	switch dest {
	case "file":
		logFile, err := os.OpenFile("rex.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to open log file")
		}
		log.Logger = log.Output(logFile)
	case "console":
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
	default:
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})
	}
}
