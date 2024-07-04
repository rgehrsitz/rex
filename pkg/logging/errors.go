// rex/pkg/logging/errors.go

package logging

import (
	"fmt"

	"github.com/rs/zerolog"
)

type ErrorType string

const (
	ErrorTypeParse   ErrorType = "PARSE"
	ErrorTypeCompile ErrorType = "COMPILE"
	ErrorTypeRuntime ErrorType = "RUNTIME"
	ErrorTypeStore   ErrorType = "STORE"
)

type RexError struct {
	Type    ErrorType
	Message string
	Err     error
	Fields  map[string]interface{}
}

func (e *RexError) Error() string {
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func (e *RexError) Unwrap() error {
	return e.Err
}

func NewError(errType ErrorType, message string, err error, fields map[string]interface{}) *RexError {
	return &RexError{
		Type:    errType,
		Message: message,
		Err:     err,
		Fields:  fields,
	}
}

func LogError(logger zerolog.Logger, err error) {
	rexErr, ok := err.(*RexError)
	if !ok {
		logger.Error().Err(err).Msg(err.Error())
		return
	}

	event := logger.Error().Err(rexErr.Err).
		Str("error_type", string(rexErr.Type)).
		Str("message", rexErr.Message)

	for k, v := range rexErr.Fields {
		event = event.Interface(k, v)
	}

	event.Msg(rexErr.Message) // Use the RexError's message here
}
