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
	require.Equal(t, float64(1), s.Snapshot()["nested"].(map[string]interface{})["n"])
	require.NoError(t, s.SetAndPublishFactContext(ctx, "out", map[string]interface{}{"x": 1}))
	events := s.DrainPublications()
	require.Len(t, events, 1)
	events[0].Value.(map[string]interface{})["x"] = 2
	require.Equal(t, float64(1), s.Snapshot()["out"].(map[string]interface{})["x"])
	require.Empty(t, s.DrainPublications())
	require.Error(t, s.SetAndPublishFactContext(ctx, "bad", math.NaN()))
	require.NotContains(t, s.Snapshot(), "bad")
	require.Empty(t, s.DrainPublications())
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
			_ = s.Snapshot()
		}(i)
	}
	wg.Wait()
	require.Len(t, s.DrainPublications(), 20)
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
