// rex/pkg/logging/errors_test.go

package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestNewError(t *testing.T) {
	tests := []struct {
		name        string
		errType     ErrorType
		message     string
		err         error
		fields      map[string]interface{}
		expectedMsg string
	}{
		{
			name:        "Parse error",
			errType:     ErrorTypeParse,
			message:     "Failed to parse",
			err:         errors.New("syntax error"),
			fields:      map[string]interface{}{"line": 10},
			expectedMsg: "PARSE: Failed to parse",
		},
		{
			name:        "Compile error",
			errType:     ErrorTypeCompile,
			message:     "Failed to compile",
			err:         nil,
			fields:      nil,
			expectedMsg: "COMPILE: Failed to compile",
		},
		{
			name:        "Runtime error",
			errType:     ErrorTypeRuntime,
			message:     "Runtime error occurred",
			err:         errors.New("division by zero"),
			fields:      map[string]interface{}{"function": "calculate"},
			expectedMsg: "RUNTIME: Runtime error occurred",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rexErr := NewError(tt.errType, tt.message, tt.err, tt.fields)

			assert.Equal(t, tt.errType, rexErr.Type)
			assert.Equal(t, tt.message, rexErr.Message)
			assert.Equal(t, tt.err, rexErr.Err)
			assert.Equal(t, tt.fields, rexErr.Fields)
			assert.Equal(t, tt.expectedMsg, rexErr.Error())

			if tt.err != nil {
				assert.Equal(t, tt.err, rexErr.Unwrap())
			} else {
				assert.Nil(t, rexErr.Unwrap())
			}
		})
	}
}

func TestLogError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected map[string]interface{}
	}{
		{
			name: "RexError with all fields",
			err: &RexError{
				Type:    ErrorTypeRuntime,
				Message: "Test error",
				Err:     errors.New("underlying error"),
				Fields: map[string]interface{}{
					"key1": "value1",
					"key2": 42,
				},
			},
			expected: map[string]interface{}{
				"error":      "underlying error",
				"error_type": "RUNTIME",
				"message":    "Test error",
				"key1":       "value1",
				"key2":       float64(42),
				"level":      "error",
			},
		},
		{
			name: "RexError without underlying error",
			err: &RexError{
				Type:    ErrorTypeParse,
				Message: "Parse error",
				Fields: map[string]interface{}{
					"line": 10,
				},
			},
			expected: map[string]interface{}{
				"error_type": "PARSE",
				"message":    "Parse error",
				"line":       float64(10),
				"level":      "error",
			},
		},
		{
			name: "Standard error",
			err:  errors.New("standard error"),
			expected: map[string]interface{}{
				"error":   "standard error",
				"message": "standard error",
				"level":   "error",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			mockLogger := zerolog.New(&buf)

			LogError(mockLogger, tt.err)

			var logged map[string]interface{}
			err := json.Unmarshal(buf.Bytes(), &logged)
			assert.NoError(t, err)

			// Check that all expected fields are present
			for k, v := range tt.expected {
				assert.Equal(t, v, logged[k], "Mismatch for key %s", k)
			}

			// Check that no unexpected fields are present
			for k := range logged {
				_, expected := tt.expected[k]
				if !expected && k != "time" {
					t.Errorf("Unexpected key in logged data: %s", k)
				}
			}

			// Optionally check for the presence of a timestamp
			if _, hasTime := logged["time"]; hasTime {
				assert.Contains(t, logged, "time", "Timestamp should be present if included")
			}
		})
	}
}
