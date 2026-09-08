package store

import (
	"context"
	"github.com/stretchr/testify/require"
	"math"
	"sync"
	"testing"
)

func TestMemoryStoreOwnershipAndLifecycle(t *testing.T) {
	ctx := context.Background()
	original := map[string]interface{}{"nested": map[string]interface{}{"n": 1}}
	s, err := NewMemoryStore(original)
	require.NoError(t, err)
	original["nested"].(map[string]interface{})["n"] = 2
	value, err := s.GetFactContext(ctx, "nested")
	require.NoError(t, err)
	require.Equal(t, float64(1), value.(map[string]interface{})["n"])
	value.(map[string]interface{})["n"] = 3
	require.Equal(t, float64(1), snapshot(t, s)["nested"].(map[string]interface{})["n"])
	require.NoError(t, s.SetAndPublishFactContext(ctx, "out", map[string]interface{}{"x": 1}))
	events := drain(t, s)
	require.Len(t, events, 1)
	events[0].Value.(map[string]interface{})["x"] = 2
	require.Equal(t, float64(1), snapshot(t, s)["out"].(map[string]interface{})["x"])
	require.Empty(t, drain(t, s))
	require.Error(t, s.SetAndPublishFactContext(ctx, "bad", math.NaN()))
	require.NotContains(t, snapshot(t, s), "bad")
	require.Empty(t, drain(t, s))
	values, err := s.MGetFactsContext(ctx, "absent")
	require.NoError(t, err)
	require.Contains(t, values, "absent")
	require.Nil(t, values["absent"])
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, s.SetAndPublishFactContext(canceled, "out", 2), context.Canceled)
	require.NoError(t, s.Close())
	require.NoError(t, s.Close())
	_, err = s.GetFactContext(ctx, "out")
	require.ErrorIs(t, err, ErrMemoryStoreClosed)
	require.ErrorIs(t, s.SetFactContext(ctx, "out", 2), ErrMemoryStoreClosed)
}

func TestMemoryStoreConcurrentPublications(t *testing.T) {
	s, err := NewMemoryStore(nil)
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.SetAndPublishFactContext(context.Background(), "out", i)
			_ = snapshot(t, s)
		}(i)
	}
	wg.Wait()
	require.Len(t, drain(t, s), 20)
}

func TestMemoryStoreMatchesRedisJSONFacts(t *testing.T) {
	server, redis := setupMiniredis(t)
	defer server.Close()
	defer redis.Close()
	memory, err := NewMemoryStore(nil)
	require.NoError(t, err)
	defer memory.Close()
	ctx := context.Background()
	for key, value := range map[string]interface{}{"number": 7, "null": nil, "object": map[string]interface{}{"list": []interface{}{true, "x", 3}}} {
		require.NoError(t, redis.SetFactContext(ctx, key, value))
		require.NoError(t, memory.SetFactContext(ctx, key, value))
	}
	keys := []string{"number", "null", "object", "missing"}
	want, err := redis.MGetFactsContext(ctx, keys...)
	require.NoError(t, err)
	got, err := memory.MGetFactsContext(ctx, keys...)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func snapshot(t *testing.T, s *MemoryStore) map[string]interface{} {
	t.Helper()
	values, err := s.Snapshot()
	require.NoError(t, err)
	return values
}
func drain(t *testing.T, s *MemoryStore) []FactUpdate {
	t.Helper()
	events, err := s.DrainPublications()
	require.NoError(t, err)
	return events
}
func TestMemoryStoreZeroValue(t *testing.T) {
	var s MemoryStore
	require.Empty(t, snapshot(t, &s))
	require.Empty(t, drain(t, &s))
	require.NoError(t, s.SetAndPublishFactContext(context.Background(), "a", 1))
	require.Equal(t, float64(1), snapshot(t, &s)["a"])
	require.Len(t, drain(t, &s), 1)
	require.NoError(t, s.Close())
	require.Equal(t, float64(1), snapshot(t, &s)["a"])
}
func TestMemoryStoreReportsCopyErrors(t *testing.T) {
	// The public write path rejects this; inject corrupted internal state to
	// verify defensive inspection behavior rather than silently returning nil.
	s := &MemoryStore{facts: map[string]interface{}{"bad": math.NaN()}, publications: []FactUpdate{{Key: "bad", Value: math.NaN()}}}
	values, err := s.MGetFactsContext(context.Background(), "bad")
	require.ErrorContains(t, err, "copy fact")
	require.Nil(t, values)
	values, err = s.Snapshot()
	require.ErrorContains(t, err, "snapshot fact")
	require.Nil(t, values)
	events, err := s.DrainPublications()
	require.ErrorContains(t, err, "copy publication")
	require.Nil(t, events)
	require.Len(t, s.publications, 1, "a failed copy must not drain the queue")
	s.publications[0].Value = true
	require.Len(t, drain(t, s), 1)
}
