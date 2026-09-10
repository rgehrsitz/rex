package runtime

import (
	"context"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"
	"testing"
	"time"
)

func TestOwnershipIncludesAllPublicFacts(t *testing.T) {
	source := []byte(`{"facts":{"a":{"type":"boolean"},"b":{"type":"boolean"},"out":{"type":"boolean"},"unused":{"type":"string"}},"rules":[{"name":"r","emit":"on_change","conditions":{"all":[{"any":[{"fact":"a","operator":"EQ","value":true,"for":"1s"},{"fact":"b","operator":"EQ","value":true}]}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`)
	artifact, err := compiler.CompileBatch(source)
	require.NoError(t, err)
	program, err := LoadProgram(artifact)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b", "out", "unused"}, program.OwnershipFacts())
	memory, err := store.NewMemoryStore(nil)
	require.NoError(t, err)
	e, err := NewEngineFromBytes(artifact, memory, 0)
	require.NoError(t, err)
	require.NoError(t, e.ValidateFactOwnership(program.OwnershipFacts()))
	require.ErrorContains(t, e.ValidateFactOwnership([]string{"a", "b", "out"}), "unused")
	require.NoError(t, e.ValidateFactOwnership(nil))
}

// An incompatible pinned artifact is an infrastructure problem, not poison.
type rejectingOwnershipQueue struct {
	DurableQueue
	began bool
}

func (q *rejectingOwnershipQueue) ValidateProgramFacts(string, []string) error {
	return store.ErrDurableOwnership
}
func (q *rejectingOwnershipQueue) Begin(context.Context, store.DurableEvent, string) (store.JournalStatus, error) {
	q.began = true
	return store.JournalStatus{}, nil
}
func TestPinnedOwnershipMismatchStaysPending(t *testing.T) {
	artifact, err := compiler.CompileBatch([]byte(batchOne))
	require.NoError(t, err)
	memory, err := store.NewMemoryStore(nil)
	require.NoError(t, err)
	e, err := NewEngineFromBytes(artifact, memory, 0)
	require.NoError(t, err)
	q := &rejectingOwnershipQueue{}
	result, err := e.processDurableEvent(context.Background(), q, store.DurableEvent{ID: "1-0", Payload: `{"a":1}`}, nil)
	require.ErrorIs(t, err, store.ErrDurableInfrastructure)
	require.True(t, result.RetryPending)
	require.False(t, result.DeadLettered)
	require.False(t, q.began)
}

func TestManagedQueueRejectsInputAndPinnedMismatch(t *testing.T) {
	server := miniredis.RunT(t)
	ctx := context.Background()
	redisStore, err := store.NewRedisStore(ctx, store.RedisOptions{Addr: server.Addr()})
	require.NoError(t, err)
	defer redisStore.Close()
	options := store.DurableOptions{Namespace: "owned", Stream: "in", Group: "g", Consumer: "c", OutputStream: "out-stream", DeadLetter: "dead", OwnedFacts: []string{"a", "b", "out"}, MaxAttempts: 1, Block: time.Millisecond}
	queue, err := redisStore.OpenDurable(ctx, options)
	require.NoError(t, err)
	require.NoError(t, queue.AcquireOwnership(ctx))
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	artifact, err := compiler.CompileBatch([]byte(batchOne))
	require.NoError(t, err)
	engine, err := NewEngineFromBytes(artifact, redisStore, 0)
	require.NoError(t, err)
	require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: "in", Values: map[string]interface{}{"payload": `{"a":1,"foreign":true}`}}).Err())
	result, err := engine.ProcessNextDurable(ctx, queue)
	require.NoError(t, err)
	require.True(t, result.DeadLettered)
	require.False(t, server.Exists("a"))
	require.False(t, server.Exists("foreign"))
	require.Equal(t, int64(1), client.XLen(ctx, "dead").Val())
	// A real queued event pinned to an incompatible artifact must stay pending.
	bad, err := compiler.CompileBatch([]byte(`{"rules":[{"name":"bad","conditions":{"all":[{"fact":"foreign","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`))
	require.NoError(t, err)
	incompatible, err := NewEngineFromBytes(bad, redisStore, 0)
	require.NoError(t, err)
	id, err := client.XAdd(ctx, &redis.XAddArgs{Stream: "in", Values: map[string]interface{}{"payload": `{"a":1}`}}).Result()
	require.NoError(t, err)
	event, err := queue.Next(ctx)
	require.NoError(t, err)
	require.Equal(t, id, event.ID)
	_, err = queue.Begin(ctx, event, incompatible.ProgramID())
	require.NoError(t, err)
	manager, err := NewEngineManager(engine)
	require.NoError(t, err)
	require.NoError(t, manager.Register(incompatible))
	result, err = manager.ProcessNextDurable(ctx, queue)
	require.ErrorIs(t, err, store.ErrDurableInfrastructure)
	require.True(t, result.RetryPending)
	require.False(t, result.DeadLettered)
	require.Equal(t, int64(1), client.XLen(ctx, "dead").Val())
	pending, err := client.XPending(ctx, "in", "g").Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), pending.Count)
}
