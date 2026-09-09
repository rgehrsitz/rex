package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestBatchAdapterContracts(t *testing.T) {
	server, redisStore := setupMiniredis(t)
	defer server.Close()
	defer redisStore.Close()
	memory, err := NewMemoryStore(nil)
	require.NoError(t, err)
	for name, adapter := range map[string]interface {
		ContextStore
		SnapshotReader
		Committer
	}{"memory": memory, "redis": redisStore} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, adapter.SetFactContext(ctx, "null", nil))
			require.NoError(t, adapter.SetFactContext(ctx, "invalid", map[string]interface{}{"x": 1}))
			require.NoError(t, adapter.SetFactContext(ctx, "value", true))
			facts, err := adapter.ReadSnapshot(ctx, []string{"missing", "null", "invalid", "value"})
			require.NoError(t, err)
			require.Equal(t, Missing, facts["missing"].State)
			require.Equal(t, Null, facts["null"].State)
			require.Equal(t, Invalid, facts["invalid"].State)
			require.Equal(t, Fact{State: Present, Value: true}, facts["value"])
			request := CommitRequest{ChainID: "contract", Writes: []Write{{Key: "out", Value: float64(1)}, {Key: "second", Value: true}}}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			result, err := adapter.Commit(canceled, request)
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, NotCommitted, result.Outcome)
			result, err = adapter.Commit(ctx, request)
			require.NoError(t, err)
			require.Equal(t, Committed, result.Outcome)
			require.Equal(t, []string{"out", "second"}, result.Applied)
			result, err = adapter.Commit(ctx, CommitRequest{Writes: []Write{{Key: "never", Value: true}, {Key: "never", Value: false}}})
			require.Error(t, err)
			require.Equal(t, NotCommitted, result.Outcome)
			value, err := adapter.GetFactContext(ctx, "never")
			require.NoError(t, err)
			require.Nil(t, value)
		})
	}
	require.Zero(t, redisStore.batchWriter().Options().MaxRetries, "dispatched v4 writes must never be automatically retried")
}

type replyFailureHook struct {
	command string
	calls   int
}

func (h *replyFailureHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *replyFailureHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *replyFailureHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if cmd.Name() == h.command && err == nil {
			h.calls++
			return io.ErrUnexpectedEOF
		}
		return err
	}
}
func checkRedisUnknown(t *testing.T, s *RedisStore, key string) {
	t.Helper()
	hook := &replyFailureHook{command: "set"}
	s.batchWriter().AddHook(hook)
	result, err := s.Commit(context.Background(), CommitRequest{ChainID: "lost-reply", Writes: []Write{{Key: key, Value: true}}})
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.Equal(t, Unknown, result.Outcome)
	require.Equal(t, 1, hook.calls)
	value, err := s.GetFactContext(context.Background(), key)
	require.NoError(t, err)
	require.Equal(t, true, value, "server accepted write despite caller seeing an error")
}
func TestRedisUnknownAndPartialOutcomes(t *testing.T) {
	server, s := setupMiniredis(t)
	defer server.Close()
	defer s.Close()
	checkRedisUnknown(t, s, "unknown")
	server2, s2 := setupMiniredis(t)
	defer server2.Close()
	defer s2.Close()
	hook := &replyFailureHook{command: "publish"}
	s2.batchWriter().AddHook(hook)
	result, err := s2.Commit(context.Background(), CommitRequest{ChainID: "publish", Writes: []Write{{Key: "out", Value: true}}})
	require.Error(t, err)
	require.Equal(t, Partial, result.Outcome)
	require.Equal(t, []string{"out"}, result.Applied)
}
func TestRedisEventSourceLifecycle(t *testing.T) {
	server, s := setupMiniredis(t)
	defer server.Close()
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source, err := s.OpenEvents(ctx, "input")
	require.NoError(t, err)
	require.NoError(t, s.client.Publish(ctx, "input", `{"a":1}`).Err())
	select {
	case event := <-source.Events():
		require.NoError(t, event.Err)
		require.Equal(t, `{"a":1}`, event.Payload)
	case <-time.After(time.Second):
		t.Fatal("event not delivered")
	}
	require.NoError(t, source.Close())
	require.NoError(t, source.Close())
	_, ok := <-source.Events()
	require.False(t, ok)
}

func TestRedisEventSourceDoesNotTreatIdleReadAsDisconnect(t *testing.T) {
	server, err := miniredis.Run()
	require.NoError(t, err)
	defer server.Close()
	s, err := NewRedisStore(context.Background(), RedisOptions{
		Addr: server.Addr(), ReadTimeout: 25 * time.Millisecond,
	})
	require.NoError(t, err)
	defer s.Close()
	source, err := s.OpenEvents(context.Background(), "input")
	require.NoError(t, err)
	defer source.Close()

	// PubSub.Receive uses an explicit zero timeout in go-redis v9.22.0, so the
	// command client's ReadTimeout must not turn an idle subscription into an outage.
	select {
	case event := <-source.Events():
		t.Fatalf("idle subscription emitted an event: %+v", event)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, s.client.Publish(context.Background(), "input", `{"after_idle":true}`).Err())
	select {
	case event := <-source.Events():
		require.NoError(t, event.Err)
		require.Equal(t, `{"after_idle":true}`, event.Payload)
	case <-time.After(time.Second):
		t.Fatal("event not delivered after idle period")
	}
}

func TestRedisEventSourceReportsDisconnectAndRecovers(t *testing.T) {
	server, s := setupMiniredis(t)
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source, err := s.OpenEvents(ctx, "input")
	require.NoError(t, err)
	defer source.Close()

	server.Close()
	select {
	case event := <-source.Events():
		require.ErrorContains(t, event.Err, "receive Redis event")
	case <-time.After(3 * time.Second):
		t.Fatal("subscription did not report disconnect")
	}
	require.NoError(t, server.Restart())
	defer server.Close()

	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case event := <-source.Events():
			if event.State == SubscriptionConnected {
				continue
			}
			if event.Err == nil {
				require.Equal(t, `{"recovered":true}`, event.Payload)
				return
			}
		case <-ticker.C:
			_ = s.client.Publish(ctx, "input", `{"recovered":true}`).Err()
		case <-deadline:
			t.Fatal("subscription did not recover")
		}
	}
}

// Opt-in: the runner must provide an isolated standalone Redis address. Keys
// are unique and cleaned; no FLUSHDB/FLUSHALL or persisted services are used.
func TestRealRedisBatchOutcomes(t *testing.T) {
	address := os.Getenv("REX_REDIS_TEST_ADDR")
	if address == "" {
		t.Skip("set REX_REDIS_TEST_ADDR to disposable standalone Redis")
	}
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1})
	require.NoError(t, client.Ping(ctx).Err())
	s := &RedisStore{client: client}
	defer s.Close()
	key := fmt.Sprintf("rex:m4:%d", time.Now().UnixNano())
	defer client.Del(ctx, key)
	checkRedisUnknown(t, s, key)
	s2 := &RedisStore{client: redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1})}
	defer s2.Close()
	hook := &replyFailureHook{command: "publish"}
	s2.batchWriter().AddHook(hook)
	result, err := s2.Commit(ctx, CommitRequest{ChainID: "real-partial", Writes: []Write{{Key: key, Value: false}}})
	require.Error(t, err)
	require.Equal(t, Partial, result.Outcome)
	value, err := s2.GetFactContext(ctx, key)
	require.NoError(t, err)
	require.Equal(t, false, value)
	// Connection refusal is conservatively Unknown once a SET was dispatched.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closed := listener.Addr().String()
	require.NoError(t, listener.Close())
	disconnected := &RedisStore{client: redis.NewClient(&redis.Options{Addr: closed, DialTimeout: 100 * time.Millisecond, MaxRetries: -1})}
	defer disconnected.Close()
	result, err = disconnected.Commit(ctx, CommitRequest{Writes: []Write{{Key: key, Value: true}}})
	require.Error(t, err)
	require.Equal(t, Unknown, result.Outcome)
	require.False(t, errors.Is(err, context.Canceled))
}

func TestRedisRejectsOversizedOutputBeforeWrites(t *testing.T) {
	server, adapter := setupMiniredis(t)
	defer server.Close()
	defer adapter.Close()
	result, err := adapter.Commit(context.Background(), CommitRequest{ChainID: "oversized", Writes: []Write{{Key: "never", Value: strings.Repeat("x", MaxEventBytes)}}})
	require.ErrorContains(t, err, "output notification exceeds")
	require.Equal(t, NotCommitted, result.Outcome)
	require.False(t, server.Exists("never"))
}
