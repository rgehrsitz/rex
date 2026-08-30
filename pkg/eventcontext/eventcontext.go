// Package eventcontext carries Rex event metadata across transport and runtime
// boundaries without coupling the store package to the runtime package.
package eventcontext

import "context"

type metadataContextKey struct{}

// Metadata identifies an event chain and its number of derived hops.
type Metadata struct {
	TraceID string `json:"trace_id"`
	Hop     int    `json:"hop"`
}

// WithMetadata returns a context that carries event metadata.
func WithMetadata(ctx context.Context, metadata Metadata) context.Context {
	return context.WithValue(ctx, metadataContextKey{}, metadata)
}

// MetadataFromContext returns event metadata and whether it was present.
func MetadataFromContext(ctx context.Context) (Metadata, bool) {
	metadata, ok := ctx.Value(metadataContextKey{}).(Metadata)
	return metadata, ok
}
