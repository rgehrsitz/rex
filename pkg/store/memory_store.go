package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// ErrMemoryStoreClosed is returned by operations after Close.
var ErrMemoryStoreClosed = errors.New("memory store closed")

// FactUpdate is a successful in-memory publication. Consumers explicitly drain
// publications; the store does not recursively evaluate rules or route channels.
type FactUpdate struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// MemoryStore is a concurrency-safe ContextStore for JSON facts. Values are
// copied through JSON, matching Redis numeric decoding and preventing aliases.
// The zero value is ready to use.
// SetAndPublish is atomic here; this does not imply Redis delivery guarantees.
type MemoryStore struct {
	mu           sync.Mutex
	facts        map[string]interface{}
	internal     map[string]interface{}
	publications []FactUpdate
	closed       bool
}

var _ ContextStore = (*MemoryStore)(nil)

func NewMemoryStore(initial map[string]interface{}) (*MemoryStore, error) {
	s := &MemoryStore{facts: make(map[string]interface{}), internal: make(map[string]interface{})}
	for k, v := range initial {
		if err := s.SetFactContext(context.Background(), k, v); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func copyJSON(value interface{}) (interface{}, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out interface{}
	err = json.Unmarshal(b, &out)
	return out, err
}

func (s *MemoryStore) Close() error { s.mu.Lock(); defer s.mu.Unlock(); s.closed = true; return nil }
func (s *MemoryStore) write(ctx context.Context, key string, value interface{}, publish bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return ErrMemoryStoreClosed
	}
	if IsInternalKey(key) {
		return fmt.Errorf("fact key %q uses reserved internal state prefix", key)
	}
	value, err := copyJSON(value)
	if err != nil {
		return err
	}
	if s.facts == nil {
		s.facts = make(map[string]interface{})
	}
	s.facts[key] = value
	if publish {
		s.publications = append(s.publications, FactUpdate{Key: key, Value: value})
	}
	return nil
}
func (s *MemoryStore) SetFactContext(ctx context.Context, key string, value interface{}) error {
	return s.write(ctx, key, value, false)
}
func (s *MemoryStore) SetAndPublishFactContext(ctx context.Context, key string, value interface{}) error {
	return s.write(ctx, key, value, true)
}
func (s *MemoryStore) GetFactContext(ctx context.Context, key string) (interface{}, error) {
	if IsInternalKey(key) {
		return nil, fmt.Errorf("fact key %q uses reserved internal state prefix", key)
	}
	values, err := s.MGetFactsContext(ctx, key)
	return values[key], err
}
func (s *MemoryStore) MGetFactsContext(ctx context.Context, keys ...string) (map[string]interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed {
		return nil, ErrMemoryStoreClosed
	}
	for _, key := range keys {
		if IsInternalKey(key) {
			return nil, fmt.Errorf("fact key %q uses reserved internal state prefix", key)
		}
	}
	out := make(map[string]interface{}, len(keys))
	for _, k := range keys {
		value, err := copyJSON(s.facts[k])
		if err != nil {
			return nil, fmt.Errorf("copy fact %q: %w", k, err)
		}
		out[k] = value
	}
	return out, nil
}

// Snapshot returns an independent copy, including after Close, for inspection.
func (s *MemoryStore) Snapshot() (map[string]interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]interface{}, len(s.facts))
	for key, value := range s.facts {
		copied, err := copyJSON(value)
		if err != nil {
			return nil, fmt.Errorf("snapshot fact %q: %w", key, err)
		}
		out[key] = copied
	}
	return out, nil
}

// DrainPublications returns successful publications in write order and clears
// the queue only after every value is copied successfully. It remains available
// after Close for final inspection.
func (s *MemoryStore) DrainPublications() ([]FactUpdate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]FactUpdate, len(s.publications))
	for i, event := range s.publications {
		out[i].Key = event.Key
		value, err := copyJSON(event.Value)
		if err != nil {
			return nil, fmt.Errorf("copy publication %d for %q: %w", i, event.Key, err)
		}
		out[i].Value = value
	}
	s.publications = nil
	return out, nil
}
