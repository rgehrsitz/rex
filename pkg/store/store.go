// rex/pkg/compiler/store/store.go

package store

import "context"

// Store is the legacy, context-free fact-store API. New runtime code should use
// ContextStore so a caller can cancel in-flight Redis operations.
type Store interface {
	Close() error
	SetFact(key string, value interface{}) error
	SetAndPublishFact(key string, value interface{}) error
	GetFact(key string) (interface{}, error)
	MGetFacts(keys ...string) (map[string]interface{}, error)
}

// ContextStore is the fact-store API used by the runtime. It gives each
// operation a caller-owned context for cancellation and deadlines.
type ContextStore interface {
	Close() error
	SetFactContext(ctx context.Context, key string, value interface{}) error
	SetAndPublishFactContext(ctx context.Context, key string, value interface{}) error
	GetFactContext(ctx context.Context, key string) (interface{}, error)
	MGetFactsContext(ctx context.Context, keys ...string) (map[string]interface{}, error)
}
