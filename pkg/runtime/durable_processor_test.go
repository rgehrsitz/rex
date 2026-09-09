package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"rgehrsitz/rex/pkg/store"
)

type durableQueueProbe struct {
	event       store.DurableEvent
	attempts    int64
	maxAttempts int64
	terminal    string
	acked       int
	completed   int
	dead        int
	beginErr    error
	applyErr    error
	inputs      int
}

func (q *durableQueueProbe) Next(context.Context) (store.DurableEvent, error) { return q.event, nil }
func (q *durableQueueProbe) Begin(context.Context, store.DurableEvent, string) (store.JournalStatus, error) {
	if q.beginErr != nil {
		return store.JournalStatus{}, q.beginErr
	}
	q.attempts++
	return store.JournalStatus{Attempts: q.attempts, Terminal: q.terminal}, nil
}
func (q *durableQueueProbe) Acknowledge(context.Context, string) error { q.acked++; return nil }
func (q *durableQueueProbe) ApplyInput(context.Context, string, string, map[string]interface{}) error {
	q.inputs++
	return q.applyErr
}
func (q *durableQueueProbe) Complete(context.Context, string) error { q.completed++; return nil }
func (q *durableQueueProbe) DeadLetterEvent(context.Context, store.DurableEvent, string, int64, error) error {
	q.dead++
	q.terminal = "dead_lettered"
	return nil
}
func (q *durableQueueProbe) MaxAttempts() int64 { return q.maxAttempts }

func durableEngine(t *testing.T) (*Engine, *store.MemoryStore) {
	t.Helper()
	memory, err := store.NewMemoryStore(nil)
	require.NoError(t, err)
	coordinator, err := NewCoordinator(batchProgram(t, batchOne), memory, memory, DefaultLimits())
	require.NoError(t, err)
	return &Engine{coordinator: coordinator, programID: "program-a"}, memory
}

func TestProcessNextDurableCompletesValidEvent(t *testing.T) {
	engine, memory := durableEngine(t)
	queue := &durableQueueProbe{event: store.DurableEvent{ID: "1-0", Payload: `{"a":1,"b":1}`}, maxAttempts: 3}
	result, err := engine.ProcessNextDurable(context.Background(), queue)
	require.NoError(t, err)
	require.True(t, result.Processed)
	require.Equal(t, "1-0", result.EventID)
	require.Equal(t, 1, queue.completed)
	require.Equal(t, 1, queue.inputs)
	facts, err := memory.Snapshot()
	require.NoError(t, err)
	require.Equal(t, true, facts["out"])
}

func TestProcessNextDurableBoundsPoisonRetries(t *testing.T) {
	engine, _ := durableEngine(t)
	queue := &durableQueueProbe{event: store.DurableEvent{ID: "2-0", Payload: "invalid"}, maxAttempts: 3}
	for attempt := int64(1); attempt <= 3; attempt++ {
		result, err := engine.ProcessNextDurable(context.Background(), queue)
		if attempt < 3 {
			require.ErrorContains(t, err, "decode durable event")
			require.True(t, result.RetryPending)
			continue
		}
		require.NoError(t, err)
		require.True(t, result.DeadLettered)
	}
	require.Equal(t, 1, queue.dead)
	result, err := engine.ProcessNextDurable(context.Background(), queue)
	require.NoError(t, err)
	require.Equal(t, 1, queue.acked)
	require.False(t, result.Processed)
}

func TestProcessNextDurableStopsOnProgramMismatch(t *testing.T) {
	engine, _ := durableEngine(t)
	queue := &durableQueueProbe{
		event: store.DurableEvent{ID: "3-0", Payload: `{}`}, maxAttempts: 3,
		beginErr: fmtProgramMismatch(),
	}
	_, err := engine.ProcessNextDurable(context.Background(), queue)
	require.ErrorIs(t, err, store.ErrDurableProgramMismatch)
	require.Zero(t, queue.dead)
}

func TestProcessNextDurableNeverDeadLettersInfrastructureFailure(t *testing.T) {
	engine, _ := durableEngine(t)
	queue := &durableQueueProbe{
		event: store.DurableEvent{ID: "4-0", Payload: `{"a":1}`}, maxAttempts: 1,
		applyErr: errors.Join(store.ErrDurableInfrastructure, errors.New("output stream unavailable")),
	}
	result, err := engine.ProcessNextDurable(context.Background(), queue)
	require.ErrorIs(t, err, store.ErrDurableInfrastructure)
	require.True(t, result.RetryPending)
	require.Zero(t, queue.dead)
}

func fmtProgramMismatch() error {
	return errors.Join(store.ErrDurableProgramMismatch, errors.New("different program"))
}

func TestRealRedisDurablePoisonThenProgress(t *testing.T) {
	address := os.Getenv("REX_REDIS_TEST_ADDR")
	if address == "" {
		t.Skip("set REX_REDIS_TEST_ADDR to disposable standalone Redis")
	}
	ctx := context.Background()
	redisStore, err := store.NewRedisStore(ctx, store.RedisOptions{Addr: address})
	require.NoError(t, err)
	defer redisStore.Close()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	options := store.DurableOptions{
		Stream: "rex:m7:poison:input:" + suffix, Group: "rex", Consumer: "worker",
		OutputStream: "rex:m7:poison:output:" + suffix, DeadLetter: "rex:m7:poison:dead:" + suffix,
		Namespace: suffix, ClaimIdle: time.Millisecond, Block: time.Millisecond,
		JournalTTL: time.Hour, MaxAttempts: 2, LockTTL: time.Second,
	}
	queue, err := redisStore.OpenDurable(ctx, options)
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	defer client.Del(ctx, options.Stream, options.OutputStream, options.DeadLetter, "a", "b", "out")
	_, err = client.XAdd(ctx, &redis.XAddArgs{Stream: options.Stream, Values: map[string]interface{}{"payload": "invalid"}}).Result()
	require.NoError(t, err)
	coordinator, err := NewCoordinator(batchProgram(t, batchOne), redisStore, redisStore, DefaultLimits())
	require.NoError(t, err)
	engine := &Engine{coordinator: coordinator, programID: "program-poison"}
	result, err := engine.ProcessNextDurable(ctx, queue)
	require.Error(t, err)
	require.True(t, result.RetryPending)
	result, err = engine.ProcessNextDurable(ctx, queue)
	require.NoError(t, err)
	require.True(t, result.DeadLettered)
	require.Equal(t, int64(1), client.XLen(ctx, options.DeadLetter).Val())

	_, err = client.XAdd(ctx, &redis.XAddArgs{Stream: options.Stream, Values: map[string]interface{}{"payload": `{"a":1,"b":1}`}}).Result()
	require.NoError(t, err)
	result, err = engine.ProcessNextDurable(ctx, queue)
	require.NoError(t, err)
	require.True(t, result.Processed)
	value, err := redisStore.GetFactContext(ctx, "out")
	require.NoError(t, err)
	require.Equal(t, true, value)
}
