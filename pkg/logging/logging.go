// rex/pkg/logging/logging.go

package logging

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
)

var Logger zerolog.Logger

func init() {
	logLevel := zerolog.InfoLevel // Default log level
	// Configure logger
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	// Parse log level from environment variable or command-line flag
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		if level, err := zerolog.ParseLevel(envLevel); err == nil {
			logLevel = level
		}
	}

	// Configure the logger
	zerolog.SetGlobalLevel(logLevel)
	Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
}

func ConfigureLogger(logLevel, logOutput string) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		log.Fatal().Err(err).Msg("Invalid log level")
	}
	zerolog.SetGlobalLevel(level)

	switch logOutput {
	case "console":
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "3:04PM"})
	case "file":
		file, err := os.Create("logs.txt")
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to create log file")
		}
		log.Logger = log.Output(file)
	default:
		log.Fatal().Msg("Invalid log output option")
	}
}
