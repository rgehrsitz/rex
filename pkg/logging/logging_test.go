// rex/pkg/logging/logging_test.go

package logging

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestConfigureLogger(t *testing.T) {
	tests := []struct {
		name          string
		logLevel      string
		logOutput     string
		expectedError string
		checkFunc     func(t *testing.T)
	}{
		{
			name:      "Debug level to console",
			logLevel:  "debug",
			logOutput: "console",
			checkFunc: func(t *testing.T) {
				assert.Equal(t, zerolog.DebugLevel, zerolog.GlobalLevel())
			},
		},
		{
			name:      "Info level to console",
			logLevel:  "info",
			logOutput: "console",
			checkFunc: func(t *testing.T) {
				assert.Equal(t, zerolog.InfoLevel, zerolog.GlobalLevel())
			},
		},
		{
			name:      "Warn level to console",
			logLevel:  "warn",
			logOutput: "console",
			checkFunc: func(t *testing.T) {
				assert.Equal(t, zerolog.WarnLevel, zerolog.GlobalLevel())
			},
		},
		{
			name:      "Error level to console",
			logLevel:  "error",
			logOutput: "console",
			checkFunc: func(t *testing.T) {
				assert.Equal(t, zerolog.ErrorLevel, zerolog.GlobalLevel())
			},
		},
		{
			name:          "Invalid level returns error",
			logLevel:      "invalid",
			logOutput:     "console",
			expectedError: "Invalid log level",
		},
		{
			name:      "Debug level to file",
			logLevel:  "debug",
			logOutput: "file",
			checkFunc: func(t *testing.T) {
				assert.Equal(t, zerolog.DebugLevel, zerolog.GlobalLevel())
				_, err := os.Stat("logs.txt")
				assert.NoError(t, err)
			},
		},
		{
			name:          "Invalid output option returns error",
			logLevel:      "info",
			logOutput:     "invalid",
			expectedError: "Invalid log output option",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ConfigureLogger(tt.logLevel, tt.logOutput)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				tt.checkFunc(t)
			}
		})
	}

	// Clean up the log file
	os.Remove("logs.txt")
}
