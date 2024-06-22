// rex/pkg/logging/logging.go

package logging

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
)

var Logger zerolog.Logger

// init initializes the logger by configuring the log level and setting up the logger instance.
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

// ConfigureLogger configures the logger with the specified log level and output.
// The log level determines the verbosity of the logs, while the log output specifies
// where the logs should be written to (console or file).
//
// Parameters:
//   - logLevel: The log level to set for the logger. Valid values are "debug", "info",
//     "warn", "error", and "fatal".
//   - logOutput: The log output option. Valid values are "console" and "file".
//
// Example usage:
//
//	ConfigureLogger("info", "console")
//	ConfigureLogger("debug", "file")
//
// Note: This function will modify the global logger instance.
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
