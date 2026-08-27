package runtime

import (
	"context"

	"github.com/rs/zerolog"

	"rgehrsitz/rex/pkg/eventcontext"
	"rgehrsitz/rex/pkg/logging"
)

// WithTraceID returns a context that associates work with one event trace.
// Callers should use one ID for every fact update decoded from the same event.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return eventcontext.WithMetadata(ctx, eventcontext.Metadata{TraceID: traceID})
}

// TraceIDFromContext returns the trace ID associated with ctx, if any.
func TraceIDFromContext(ctx context.Context) string {
	metadata, _ := eventcontext.MetadataFromContext(ctx)
	return metadata.TraceID
}

// EventHopFromContext returns the derived-event hop count associated with ctx.
func EventHopFromContext(ctx context.Context) int {
	metadata, _ := eventcontext.MetadataFromContext(ctx)
	return metadata.Hop
}

func traceLogger(ctx context.Context) zerolog.Logger {
	traceID := TraceIDFromContext(ctx)
	if traceID == "" {
		return logging.Logger
	}

	return logging.Logger.With().Str("trace_id", traceID).Logger()
}
