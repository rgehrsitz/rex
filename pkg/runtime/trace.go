package runtime

import (
	"context"

	"github.com/rs/zerolog"

	"rgehrsitz/rex/pkg/logging"
)

type traceIDContextKey struct{}

// WithTraceID returns a context that associates work with one event trace.
// Callers should use one ID for every fact update decoded from the same event.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDContextKey{}, traceID)
}

// TraceIDFromContext returns the trace ID associated with ctx, if any.
func TraceIDFromContext(ctx context.Context) string {
	traceID, _ := ctx.Value(traceIDContextKey{}).(string)
	return traceID
}

func traceLogger(ctx context.Context) zerolog.Logger {
	traceID := TraceIDFromContext(ctx)
	if traceID == "" {
		return logging.Logger
	}

	return logging.Logger.With().Str("trace_id", traceID).Logger()
}
