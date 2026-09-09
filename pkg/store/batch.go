package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const InternalStatePrefix = "__rex_temporal_"

func IsInternalKey(key string) bool { return strings.HasPrefix(key, InternalStatePrefix) }

type FactState string

const (
	Missing FactState = "missing"
	Null    FactState = "null"
	Invalid FactState = "invalid"
	Present FactState = "present"
)

type Fact struct {
	State FactState   `json:"state"`
	Value interface{} `json:"value,omitempty"`
}

func Scalar(value interface{}) bool {
	switch v := value.(type) {
	case nil, string, bool:
		return true
	case float64:
		return !math.IsNaN(v) && !math.IsInf(v, 0)
	}
	return false
}
func DecodeFact(data []byte) Fact {
	if data == nil {
		return Fact{State: Missing}
	}
	var value interface{}
	if json.Unmarshal(data, &value) != nil || !Scalar(value) {
		return Fact{State: Invalid}
	}
	if value == nil {
		return Fact{State: Null}
	}
	return Fact{State: Present, Value: value}
}

// SnapshotReader returns one round's point-in-time values; missing keys must
// be represented explicitly. Returned maps are owned by the caller.
type SnapshotReader interface {
	ReadSnapshot(context.Context, []string) (map[string]Fact, error)
}
type Write struct {
	Key      string      `json:"key"`
	Value    interface{} `json:"value"`
	Internal bool        `json:"internal,omitempty"`
	Delete   bool        `json:"delete,omitempty"`
}
type CommitRequest struct {
	ChainID string           `json:"chain_id"`
	Round   int              `json:"round"`
	Writes  []Write          `json:"writes"`
	Actions []ActionIdentity `json:"actions,omitempty"`
}

// ActionIdentity identifies a source action independently of its output value.
type ActionIdentity struct {
	Rule   string `json:"rule"`
	Index  int    `json:"index"`
	Target string `json:"target"`
}
type CommitOutcome string

const (
	Committed    CommitOutcome = "committed"
	NotCommitted CommitOutcome = "not_committed"
	Partial      CommitOutcome = "partial"
	Unknown      CommitOutcome = "unknown"
)

type CommitResult struct {
	Outcome CommitOutcome `json:"outcome"`
	Applied []string      `json:"applied,omitempty"`
}

const MaxCommitWrites = 4096

// Committer must not retry dispatched operations whose outcome is unknown.
type Committer interface {
	Commit(context.Context, CommitRequest) (CommitResult, error)
}

// InternalStateCleaner removes private state after its owning artifact is no
// longer retained for active execution, rollback, or durable recovery.
type InternalStateCleaner interface {
	DeleteInternal(context.Context, []string) error
}

// UnknownOutcomeResolver marks a committer whose durable marker resolves an
// unknown transaction outcome before the same event is retried.
type UnknownOutcomeResolver interface {
	ResolvesUnknownOnRetry() bool
}

func validateWrites(writes []Write) error {
	if len(writes) > MaxCommitWrites {
		return fmt.Errorf("commit exceeds write limit")
	}
	bytes := 0
	seen := map[string]bool{}
	for _, w := range writes {
		if w.Key == "" || len(w.Key) > 255 || !Scalar(w.Value) || seen[w.Key] || w.Internal != IsInternalKey(w.Key) || (w.Delete && (!w.Internal || w.Value != nil)) {
			return fmt.Errorf("invalid or duplicate commit target %q", w.Key)
		}
		if value, ok := w.Value.(string); ok && len(value) > MaxSnapshotBytes {
			return fmt.Errorf("commit exceeds byte limit")
		}
		data, err := json.Marshal(w)
		if err != nil {
			return err
		}
		bytes += len(data)
		if bytes > MaxSnapshotBytes {
			return fmt.Errorf("commit exceeds byte limit")
		}
		seen[w.Key] = true
	}
	return nil
}
func (s *MemoryStore) ReadSnapshot(ctx context.Context, keys []string) (map[string]Fact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed {
		return nil, ErrMemoryStoreClosed
	}
	out := make(map[string]Fact, len(keys))
	for _, k := range keys {
		var value interface{}
		var ok bool
		if IsInternalKey(k) {
			value, ok = s.internal[k]
		} else {
			value, ok = s.facts[k]
		}
		if !ok {
			out[k] = Fact{State: Missing}
			continue
		}
		b, err := json.Marshal(value)
		if err != nil {
			out[k] = Fact{State: Invalid}
		} else {
			out[k] = DecodeFact(b)
		}
	}
	return out, nil
}
func (s *MemoryStore) Commit(ctx context.Context, request CommitRequest) (CommitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	failed := CommitResult{Outcome: NotCommitted}
	if err := ctx.Err(); err != nil {
		return failed, err
	}
	if s.closed {
		return failed, ErrMemoryStoreClosed
	}
	if err := validateWrites(request.Writes); err != nil {
		return failed, err
	}
	if s.facts == nil {
		s.facts = make(map[string]interface{})
	}
	result := CommitResult{Outcome: Committed}
	for _, w := range request.Writes {
		if w.Internal {
			if s.internal == nil {
				s.internal = make(map[string]interface{})
			}
			if w.Delete {
				delete(s.internal, w.Key)
				continue
			}
			s.internal[w.Key] = w.Value
			continue
		}
		s.facts[w.Key] = w.Value
		s.publications = append(s.publications, FactUpdate{Key: w.Key, Value: w.Value})
		result.Applied = append(result.Applied, w.Key)
	}
	return result, nil
}

func (s *MemoryStore) DeleteInternal(ctx context.Context, keys []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return ErrMemoryStoreClosed
	}
	for _, key := range keys {
		if !IsInternalKey(key) {
			return fmt.Errorf("internal cleanup key %q is invalid", key)
		}
	}
	for _, key := range keys {
		delete(s.internal, key)
	}
	return nil
}
