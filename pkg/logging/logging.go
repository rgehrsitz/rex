// rex/pkg/logging/logging.go

package logging

import (
	"os"

	"github.com/rs/zerolog"
)

var Logger zerolog.Logger

func init() {
	logLevel := zerolog.InfoLevel
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		if level, err := zerolog.ParseLevel(envLevel); err == nil {
			logLevel = level
		}
	}

	zerolog.SetGlobalLevel(logLevel)
	Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
}

func ConfigureLogger(logLevel, logOutput string) error {
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		return NewError(ErrorTypeParse, "Invalid log level", err, map[string]interface{}{"log_level": logLevel})
	}
	zerolog.SetGlobalLevel(level)

	switch logOutput {
	case "console":
		Logger = Logger.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "3:04PM"})
	case "file":
		file, err := os.Create("logs.txt")
		if err != nil {
			return NewError(ErrorTypeRuntime, "Failed to create log file", err, nil)
		}
		Logger = Logger.Output(file)
	default:
		return NewError(ErrorTypeParse, "Invalid log output option", nil, map[string]interface{}{"log_output": logOutput})
	}

	return nil
}
