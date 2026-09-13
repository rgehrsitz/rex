package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type redisExchangeHook struct{ exchanges atomic.Int64 }

func (h *redisExchangeHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *redisExchangeHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		h.exchanges.Add(1)
		return next(ctx, cmd)
	}
}
func (h *redisExchangeHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		h.exchanges.Add(1)
		return next(ctx, cmds)
	}
}

func durableTestOptions(t *testing.T) DurableOptions {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	return DurableOptions{
		Stream: "rex:m7:input:" + suffix, Group: "rex-m7", Consumer: "worker-1",
		OutputStream: "rex:m7:output:" + suffix, DeadLetter: "rex:m7:dead:" + suffix,
		Namespace: suffix, ClaimIdle: time.Millisecond, Block: time.Millisecond,
		JournalTTL: time.Hour, MaxAttempts: 3, LockTTL: time.Second,
	}
}

func addDurableEvent(t *testing.T, durable *RedisDurable, payload string) DurableEvent {
	t.Helper()
	id, err := durable.client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: durable.options.Stream, Values: map[string]interface{}{"payload": payload},
	}).Result()
	require.NoError(t, err)
	event, err := durable.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, id, event.ID)
	return event
}

func TestRedisDurableCommitRecoveryAndSnapshotReplay(t *testing.T) {
	server, redisStore := setupMiniredis(t)
	defer server.Close()
	defer redisStore.Close()
	ctx := context.Background()
	options := durableTestOptions(t)
	durable, err := redisStore.OpenDurable(ctx, options)
	require.NoError(t, err)
	require.NoError(t, durable.AcquireOwnership(ctx))
	defer durable.ReleaseOwnership(ctx)

	event := addDurableEvent(t, durable, `{"temperature":30}`)
	pinned, err := durable.PinnedProgram(ctx, event.ID)
	require.NoError(t, err)
	require.Empty(t, pinned)
	status, err := durable.Begin(ctx, event, "program-a")
	require.NoError(t, err)
	require.Equal(t, int64(1), status.Attempts)
	pinned, err = durable.PinnedProgram(ctx, event.ID)
	require.NoError(t, err)
	require.Equal(t, "program-a", pinned)
	require.NoError(t, durable.ApplyInput(ctx, event.ID, "program-a", map[string]interface{}{"temperature": float64(30)}))
	require.NoError(t, durable.ApplyInput(ctx, event.ID, "program-a", map[string]interface{}{"temperature": float64(30)}))
	temperature, err := redisStore.GetFactContext(ctx, "temperature")
	require.NoError(t, err)
	require.Equal(t, float64(30), temperature)
	require.NoError(t, redisStore.SetFactContext(ctx, "limit", 10))
	eventCtx := WithEvaluationRound(WithDurableEvent(ctx, event.ID, "program-a"), 0)
	snapshot, err := redisStore.ReadSnapshot(eventCtx, []string{"limit"})
	require.NoError(t, err)
	require.Equal(t, float64(10), snapshot["limit"].Value)
	require.NoError(t, redisStore.SetFactContext(ctx, "limit", 99))
	replayed, err := redisStore.ReadSnapshot(eventCtx, []string{"limit"})
	require.NoError(t, err)
	require.Equal(t, snapshot, replayed)

	request := CommitRequest{ChainID: event.ID, Round: 0, Writes: []Write{{Key: "alarm", Value: true}}, Actions: []ActionIdentity{{Rule: "hot", Index: 0, Target: "alarm"}}}
	result, err := redisStore.Commit(eventCtx, request)
	require.NoError(t, err)
	require.Equal(t, Committed, result.Outcome)
	result, err = redisStore.Commit(eventCtx, request)
	require.NoError(t, err)
	require.Equal(t, Committed, result.Outcome)
	require.Equal(t, int64(1), redisStore.client.XLen(ctx, options.OutputStream).Val())
	outputs, err := redisStore.client.XRange(ctx, options.OutputStream, "-", "+").Result()
	require.NoError(t, err)
	require.Equal(t, stableDurableID("output", options.Namespace, event.ID, "program-a", "0"), outputs[0].Values["output_id"])
	var outputPayload struct {
		WriteIDs  map[string]string `json:"write_ids"`
		ActionIDs []string          `json:"action_ids"`
	}
	require.NoError(t, json.Unmarshal([]byte(outputs[0].Values["payload"].(string)), &outputPayload))
	require.Equal(t, stableDurableID("write", options.Namespace, event.ID, "program-a", "0", "alarm"), outputPayload.WriteIDs["alarm"])
	require.Equal(t, []string{stableDurableID("action", options.Namespace, event.ID, "program-a", "0", "hot", "0", "alarm")}, outputPayload.ActionIDs)
	alarm, err := redisStore.GetFactContext(ctx, "alarm")
	require.NoError(t, err)
	require.Equal(t, true, alarm)

	require.NoError(t, durable.Complete(ctx, event.ID))
	stats, err := durable.Stats(ctx)
	require.NoError(t, err)
	require.Zero(t, stats.Pending)
	status, err = durable.Begin(ctx, event, "program-a")
	require.NoError(t, err)
	require.Equal(t, "completed", status.Terminal)
	require.Equal(t, int64(1), status.Attempts)
}

func TestDurableScriptPathCutsSteadyStateExchanges(t *testing.T) {
	measure := func(t *testing.T, mode string) int64 {
		redisStore := setupRealRedisStore(t, 11)
		ctx := context.Background()
		options := durableTestOptions(t)
		options.TransactionMode = mode
		options.OwnedFacts = []string{"input", "limit", "alarm"}
		durable, err := redisStore.OpenDurable(ctx, options)
		require.NoError(t, err)
		require.NoError(t, durable.AcquireOwnership(ctx))
		event := addDurableEvent(t, durable, `{"input":true}`)
		require.NoError(t, redisStore.SetFactContext(ctx, "limit", 10))

		hook := &redisExchangeHook{}
		redisStore.client.AddHook(hook)
		durable.client.AddHook(hook)
		_, err = durable.Begin(ctx, event, "program")
		require.NoError(t, err)
		require.NoError(t, durable.ApplyInput(ctx, event.ID, "program", map[string]interface{}{"input": true}))
		eventCtx := WithEvaluationRound(WithDurableEvent(ctx, event.ID, "program"), 0)
		_, err = redisStore.ReadSnapshot(eventCtx, []string{"limit"})
		require.NoError(t, err)
		result, err := redisStore.Commit(eventCtx, CommitRequest{Round: 0, Writes: []Write{{Key: "alarm", Value: true}}})
		require.NoError(t, err)
		require.Equal(t, Committed, result.Outcome)
		require.NoError(t, durable.Complete(ctx, event.ID))
		return hook.exchanges.Load()
	}

	script := measure(t, durableTransactionScript)
	watch := measure(t, durableTransactionWatch)
	require.LessOrEqual(t, script, int64(9))
	require.GreaterOrEqual(t, watch, int64(25))
	require.Less(t, script*2, watch)
}

func TestDurableScriptReloadsAfterCacheFlush(t *testing.T) {
	redisStore := setupRealRedisStore(t, 11)
	ctx := context.Background()
	durable, err := redisStore.OpenDurable(ctx, durableTestOptions(t))
	require.NoError(t, err)
	require.NoError(t, durable.client.ScriptFlush(ctx).Err())
	event := addDurableEvent(t, durable, `{}`)
	status, err := durable.Begin(ctx, event, "program")
	require.NoError(t, err)
	require.Equal(t, int64(1), status.Attempts)
}

func TestDurableTransactionModesKeepPersistentFormat(t *testing.T) {
	type persistentState struct {
		journal map[string]string
		output  map[string]interface{}
	}
	run := func(t *testing.T, mode string) persistentState {
		redisStore := setupRealRedisStore(t, 12)
		ctx := context.Background()
		options := DurableOptions{
			Stream: "compat:in", Group: "compat", Consumer: "worker", OutputStream: "compat:out", DeadLetter: "compat:dead",
			Namespace: "compat", OwnedFacts: []string{"input", "limit", "alarm"}, TransactionMode: mode,
			ClaimIdle: time.Millisecond, Block: time.Millisecond, JournalTTL: time.Hour, MaxAttempts: 3, LockTTL: time.Second,
		}
		durable, err := redisStore.OpenDurable(ctx, options)
		require.NoError(t, err)
		require.NoError(t, durable.AcquireOwnership(ctx))
		id, err := durable.client.XAdd(ctx, &redis.XAddArgs{Stream: options.Stream, ID: "1-0", Values: map[string]interface{}{"payload": `{"input":true}`}}).Result()
		require.NoError(t, err)
		event, err := durable.Next(ctx)
		require.NoError(t, err)
		require.Equal(t, id, event.ID)
		_, err = durable.Begin(ctx, event, "program")
		require.NoError(t, err)
		require.NoError(t, durable.ApplyInput(ctx, event.ID, "program", map[string]interface{}{"input": true}))
		require.NoError(t, redisStore.SetFactContext(ctx, "limit", 10))
		eventCtx := WithEvaluationRound(WithDurableEvent(ctx, event.ID, "program"), 0)
		_, err = redisStore.ReadSnapshot(eventCtx, []string{"limit"})
		require.NoError(t, err)
		_, err = redisStore.Commit(eventCtx, CommitRequest{ChainID: "chain", Round: 0, Writes: []Write{{Key: "alarm", Value: true}}, Actions: []ActionIdentity{{Rule: "rule", Target: "alarm"}}})
		require.NoError(t, err)
		require.NoError(t, durable.Complete(ctx, event.ID))
		journal, err := redisStore.client.HGetAll(ctx, durable.journalKey(event.ID)).Result()
		require.NoError(t, err)
		outputs, err := redisStore.client.XRange(ctx, options.OutputStream, "-", "+").Result()
		require.NoError(t, err)
		require.Len(t, outputs, 1)
		return persistentState{journal: journal, output: outputs[0].Values}
	}

	require.Equal(t, run(t, durableTransactionWatch), run(t, durableTransactionScript))
}

func TestDurableTransactionModesRecoverEachOther(t *testing.T) {
	for _, modes := range [][2]string{{durableTransactionWatch, durableTransactionScript}, {durableTransactionScript, durableTransactionWatch}} {
		t.Run(modes[0]+"-to-"+modes[1], func(t *testing.T) {
			first := setupRealRedisStore(t, 13)
			ctx := context.Background()
			options := durableTestOptions(t)
			options.OwnedFacts = []string{"input", "output"}
			options.TransactionMode = modes[0]
			durable, err := first.OpenDurable(ctx, options)
			require.NoError(t, err)
			require.NoError(t, durable.AcquireOwnership(ctx))
			event := addDurableEvent(t, durable, `{"input":true}`)
			_, err = durable.Begin(ctx, event, "program")
			require.NoError(t, err)
			require.NoError(t, durable.ApplyInput(ctx, event.ID, "program", map[string]interface{}{"input": true}))
			eventCtx := WithEvaluationRound(WithDurableEvent(ctx, event.ID, "program"), 0)
			request := CommitRequest{Round: 0, Writes: []Write{{Key: "output", Value: true}}}
			_, err = first.Commit(eventCtx, request)
			require.NoError(t, err)
			require.NoError(t, durable.ReleaseOwnership(ctx))
			require.NoError(t, first.Close())

			secondClient := redis.NewClient(&redis.Options{Addr: realRedisAddress(t), DB: 13, MaxRetries: -1})
			second := &RedisStore{client: secondClient}
			defer second.Close()
			options.Consumer = "successor"
			options.TransactionMode = modes[1]
			recovered, err := second.OpenDurable(ctx, options)
			require.NoError(t, err)
			require.NoError(t, recovered.AcquireOwnership(ctx))
			status, err := recovered.Begin(ctx, event, "program")
			require.NoError(t, err)
			require.Equal(t, int64(2), status.Attempts)
			require.NoError(t, recovered.ApplyInput(ctx, event.ID, "program", map[string]interface{}{"input": true}))
			result, err := second.Commit(eventCtx, request)
			require.NoError(t, err)
			require.Equal(t, Committed, result.Outcome)
			require.NoError(t, recovered.Complete(ctx, event.ID))
			require.Equal(t, int64(1), second.client.XLen(ctx, options.OutputStream).Val())
		})
	}
}

func TestRealRedisScriptACLPreflightPreventsPartialCommit(t *testing.T) {
	address := realRedisAddress(t)
	ctx := context.Background()
	admin := redis.NewClient(&redis.Options{Addr: address, DB: 14, MaxRetries: -1})
	defer admin.Close()
	require.NoError(t, admin.FlushDB(ctx).Err())
	defer admin.FlushDB(ctx)
	options := durableTestOptions(t)
	user := "rex-m89-" + options.Namespace
	password := "m89-test-password"
	require.NoError(t, admin.Do(ctx, "ACL", "SETUSER", user, "on", ">"+password, "~*", "+@all", "-xadd").Err())
	defer admin.Do(ctx, "ACL", "DELUSER", user)

	client := redis.NewClient(&redis.Options{Addr: address, DB: 14, Username: user, Password: password, MaxRetries: -1})
	redisStore := &RedisStore{client: client}
	defer redisStore.Close()
	options.TransactionMode = durableTransactionScript
	options.OwnedFacts = []string{"input", "output"}
	durable, err := redisStore.OpenDurable(ctx, options)
	require.NoError(t, err)
	require.NoError(t, durable.AcquireOwnership(ctx))
	id, err := admin.XAdd(ctx, &redis.XAddArgs{Stream: options.Stream, Values: map[string]interface{}{"payload": `{"input":true}`}}).Result()
	require.NoError(t, err)
	event, err := durable.Next(ctx)
	require.NoError(t, err)
	require.Equal(t, id, event.ID)
	_, err = durable.Begin(ctx, event, "program")
	require.NoError(t, err)
	require.NoError(t, durable.ApplyInput(ctx, event.ID, "program", map[string]interface{}{"input": true}))
	eventCtx := WithEvaluationRound(WithDurableEvent(ctx, event.ID, "program"), 0)
	result, err := redisStore.Commit(eventCtx, CommitRequest{Round: 0, Writes: []Write{{Key: "output", Value: true}}})
	require.ErrorIs(t, err, errDurablePreflight)
	require.Equal(t, NotCommitted, result.Outcome)
	require.Zero(t, admin.Exists(ctx, "output").Val())
	require.Zero(t, admin.XLen(ctx, options.OutputStream).Val())
	require.False(t, admin.HExists(ctx, durable.journalKey(event.ID), "commit:0").Val())
}

func TestRedisDurableInternalCommitIsAtomicAndSilent(t *testing.T) {
	server, redisStore := setupMiniredis(t)
	defer server.Close()
	defer redisStore.Close()
	ctx := context.Background()
	options := durableTestOptions(t)
	durable, err := redisStore.OpenDurable(ctx, options)
	require.NoError(t, err)
	event := addDurableEvent(t, durable, `{"hot":true}`)
	_, err = durable.Begin(ctx, event, "program-temporal")
	require.NoError(t, err)
	firstTime := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	pinned, err := durable.PinProcessingTime(ctx, event.ID, firstTime)
	require.NoError(t, err)
	require.Equal(t, firstTime, pinned)
	pinned, err = durable.PinProcessingTime(ctx, event.ID, firstTime.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, firstTime, pinned)
	eventCtx := WithEvaluationRound(WithDurableEvent(ctx, event.ID, "program-temporal"), 0)
	tracker := "__rex_temporal_test"
	request := CommitRequest{ChainID: event.ID, Round: 0, Writes: []Write{{Key: tracker, Value: "2026-09-09T12:00:00Z", Internal: true}}}

	result, err := redisStore.Commit(eventCtx, request)
	require.NoError(t, err)
	require.Equal(t, Committed, result.Outcome)
	require.Empty(t, result.Applied)
	require.Equal(t, int64(0), redisStore.client.XLen(ctx, options.OutputStream).Val())
	state, err := redisStore.ReadSnapshot(eventCtx, []string{tracker})
	require.NoError(t, err)
	require.Equal(t, Fact{State: Present, Value: "2026-09-09T12:00:00Z"}, state[tracker])

	result, err = redisStore.Commit(eventCtx, request)
	require.NoError(t, err)
	require.Equal(t, Committed, result.Outcome)
	require.Empty(t, result.Applied)
	require.Equal(t, int64(0), redisStore.client.XLen(ctx, options.OutputStream).Val())

	resetCtx := WithEvaluationRound(WithDurableEvent(ctx, event.ID, "program-temporal"), 1)
	result, err = redisStore.Commit(resetCtx, CommitRequest{ChainID: event.ID, Round: 1, Writes: []Write{{Key: tracker, Internal: true, Delete: true}}})
	require.NoError(t, err)
	require.Equal(t, Committed, result.Outcome)
	state, err = redisStore.ReadSnapshot(resetCtx, []string{tracker})
	require.NoError(t, err)
	require.Equal(t, Missing, state[tracker].State)
	require.Equal(t, int64(0), redisStore.client.XLen(ctx, options.OutputStream).Val())
}

func TestRedisDurableRejectsProgramDriftAndUnsafeOutputType(t *testing.T) {
	server, redisStore := setupMiniredis(t)
	defer server.Close()
	defer redisStore.Close()
	ctx := context.Background()
	options := durableTestOptions(t)
	durable, err := redisStore.OpenDurable(ctx, options)
	require.NoError(t, err)
	event := addDurableEvent(t, durable, `{"a":true}`)
	_, err = durable.Begin(ctx, event, "program-a")
	require.NoError(t, err)
	_, err = durable.Begin(ctx, event, "program-b")
	require.ErrorIs(t, err, ErrDurableProgramMismatch)
	require.ErrorContains(t, durable.ApplyInput(ctx, event.ID, "program-a", map[string]interface{}{options.OutputStream: true}), "reserved")

	require.NoError(t, redisStore.client.Set(ctx, options.OutputStream, "wrong-type", 0).Err())
	eventCtx := WithEvaluationRound(WithDurableEvent(ctx, event.ID, "program-a"), 0)
	result, err := redisStore.Commit(eventCtx, CommitRequest{Round: 0, Writes: []Write{{Key: "must-not-exist", Value: true}}})
	require.ErrorIs(t, err, errDurablePreflight)
	require.Equal(t, NotCommitted, result.Outcome)
	require.False(t, server.Exists("must-not-exist"))
}

func TestRedisDurableRejectsInvalidLimitsAndOverlappingStreams(t *testing.T) {
	server, redisStore := setupMiniredis(t)
	defer server.Close()
	defer redisStore.Close()
	options := durableTestOptions(t)
	options.MaxAttempts = -1
	_, err := redisStore.OpenDurable(context.Background(), options)
	require.ErrorContains(t, err, "positive")
	options = durableTestOptions(t)
	options.OutputStream = options.Stream
	_, err = redisStore.OpenDurable(context.Background(), options)
	require.ErrorContains(t, err, "must be distinct")
	options = durableTestOptions(t)
	options.Namespace = "invalid:namespace*"
	_, err = redisStore.OpenDurable(context.Background(), options)
	require.ErrorContains(t, err, "ASCII letters")
	options = durableTestOptions(t)
	options.TransactionMode = "invalid"
	_, err = redisStore.OpenDurable(context.Background(), options)
	require.ErrorContains(t, err, "transaction mode")
}

func TestRedisDurableStoreCanOpenOnlyOnePartition(t *testing.T) {
	server, redisStore := setupMiniredis(t)
	defer server.Close()
	defer redisStore.Close()
	_, err := redisStore.OpenDurable(context.Background(), durableTestOptions(t))
	require.NoError(t, err)
	_, err = redisStore.OpenDurable(context.Background(), durableTestOptions(t))
	require.ErrorContains(t, err, "already open")
}

func TestRedisDurableDeadLetterIsIdempotent(t *testing.T) {
	server, redisStore := setupMiniredis(t)
	defer server.Close()
	defer redisStore.Close()
	ctx := context.Background()
	options := durableTestOptions(t)
	durable, err := redisStore.OpenDurable(ctx, options)
	require.NoError(t, err)
	event := addDurableEvent(t, durable, "invalid")
	status, err := durable.Begin(ctx, event, "program-a")
	require.NoError(t, err)
	processErr := errors.New("poison")
	require.NoError(t, durable.DeadLetterEvent(ctx, event, "program-a", status.Attempts, processErr))
	require.NoError(t, durable.DeadLetterEvent(ctx, event, "program-a", status.Attempts, processErr))
	err = durable.Complete(ctx, event.ID)
	require.ErrorIs(t, err, ErrDurableReconciliation)
	require.NotErrorIs(t, err, ErrDurableInfrastructure)
	require.Equal(t, int64(1), redisStore.client.XLen(ctx, options.DeadLetter).Val())
	stats, err := durable.Stats(ctx)
	require.NoError(t, err)
	require.Zero(t, stats.Pending)
}

func TestRedisDurableOwnershipLease(t *testing.T) {
	server, first := setupMiniredis(t)
	defer server.Close()
	defer first.Close()
	second, err := NewRedisStore(context.Background(), RedisOptions{Addr: server.Addr()})
	require.NoError(t, err)
	defer second.Close()
	options := durableTestOptions(t)
	one, err := first.OpenDurable(context.Background(), options)
	require.NoError(t, err)
	two, err := second.OpenDurable(context.Background(), options)
	require.NoError(t, err)
	require.NoError(t, one.AcquireOwnership(context.Background()))
	require.ErrorIs(t, two.AcquireOwnership(context.Background()), ErrDurableOwnership)
	require.NoError(t, one.RenewOwnership(context.Background()))
	require.NoError(t, one.ReleaseOwnership(context.Background()))
	require.NoError(t, two.AcquireOwnership(context.Background()))
}

func TestRedisDurableDoesNotPassOlderPendingWork(t *testing.T) {
	server, redisStore := setupMiniredis(t)
	defer server.Close()
	defer redisStore.Close()
	ctx := context.Background()
	options := durableTestOptions(t)
	durable, err := redisStore.OpenDurable(ctx, options)
	require.NoError(t, err)
	firstID, err := durable.client.XAdd(ctx, &redis.XAddArgs{Stream: options.Stream, Values: map[string]interface{}{"payload": `{"order":1}`}}).Result()
	require.NoError(t, err)
	secondID, err := durable.client.XAdd(ctx, &redis.XAddArgs{Stream: options.Stream, Values: map[string]interface{}{"payload": `{"order":2}`}}).Result()
	require.NoError(t, err)
	first, err := durable.Next(ctx)
	require.NoError(t, err)
	require.Equal(t, firstID, first.ID)
	pending, err := durable.Next(ctx)
	require.NoError(t, err)
	require.Equal(t, firstID, pending.ID)
	require.True(t, pending.Recovered)
	require.NoError(t, durable.Complete(ctx, firstID))
	second, err := durable.Next(ctx)
	require.NoError(t, err)
	require.Equal(t, secondID, second.ID)
}

type lostTransactionReplyHook struct{ failed bool }

func (h *lostTransactionReplyHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *lostTransactionReplyHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, command redis.Cmder) error {
		err := next(ctx, command)
		if !h.failed && err == nil && (command.Name() == "evalsha" || command.Name() == "eval") {
			h.failed = true
			return io.ErrUnexpectedEOF
		}
		return err
	}
}
func (h *lostTransactionReplyHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, commands []redis.Cmder) error {
		err := next(ctx, commands)
		if !h.failed && err == nil {
			for _, command := range commands {
				if command.Name() == "exec" {
					h.failed = true
					return io.ErrUnexpectedEOF
				}
			}
		}
		return err
	}
}

func TestRealRedisDurableResolvesLostCommitReply(t *testing.T) {
	address := realRedisAddress(t)
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1})
	require.NoError(t, client.Ping(ctx).Err())
	redisStore := &RedisStore{client: client}
	defer redisStore.Close()
	options := durableTestOptions(t)
	defer client.Del(ctx, options.Stream, options.OutputStream, options.DeadLetter, "durable-real-output")
	durable, err := redisStore.OpenDurable(ctx, options)
	require.NoError(t, err)
	event := addDurableEvent(t, durable, `{"a":true}`)
	_, err = durable.Begin(ctx, event, "program-real")
	require.NoError(t, err)
	hook := &lostTransactionReplyHook{}
	redisStore.batchWriter().AddHook(hook)
	eventCtx := WithEvaluationRound(WithDurableEvent(ctx, event.ID, "program-real"), 0)
	request := CommitRequest{Round: 0, Writes: []Write{{Key: "durable-real-output", Value: true}}}
	result, err := redisStore.Commit(eventCtx, request)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.Equal(t, Unknown, result.Outcome)
	result, err = redisStore.Commit(eventCtx, request)
	require.NoError(t, err)
	require.Equal(t, Committed, result.Outcome)
	require.True(t, hook.failed)
	require.Equal(t, int64(1), client.XLen(ctx, options.OutputStream).Val())
}

func TestRealRedisDurableRestartBoundaries(t *testing.T) {
	address := realRedisAddress(t)
	ctx := context.Background()
	for _, boundary := range []string{"before_commit", "after_commit_before_ack"} {
		t.Run(boundary, func(t *testing.T) {
			options := durableTestOptions(t)
			factKey := "rex:m7:fact:" + options.Namespace
			firstClient := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1})
			first := &RedisStore{client: firstClient}
			durable, err := first.OpenDurable(ctx, options)
			require.NoError(t, err)
			event := addDurableEvent(t, durable, `{"input":1}`)
			_, err = durable.Begin(ctx, event, "program-restart")
			require.NoError(t, err)
			require.NoError(t, durable.ApplyInput(ctx, event.ID, "program-restart", map[string]interface{}{"input": float64(1)}))
			eventCtx := WithEvaluationRound(WithDurableEvent(ctx, event.ID, "program-restart"), 0)
			request := CommitRequest{Round: 0, Writes: []Write{{Key: factKey, Value: boundary}}}
			if boundary == "after_commit_before_ack" {
				result, err := first.Commit(eventCtx, request)
				require.NoError(t, err)
				require.Equal(t, Committed, result.Outcome)
			}
			require.NoError(t, first.Close())

			secondClient := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1})
			second := &RedisStore{client: secondClient}
			defer second.Close()
			defer secondClient.Del(ctx, options.Stream, options.OutputStream, options.DeadLetter, factKey, "input", "rex:durable:"+options.Namespace+":event:"+event.ID)
			secondOptions := options
			secondOptions.Consumer = "worker-2"
			time.Sleep(2 * options.ClaimIdle)
			recovered, err := second.OpenDurable(ctx, secondOptions)
			require.NoError(t, err)
			replayed, err := recovered.Next(ctx)
			require.NoError(t, err)
			require.Equal(t, event.ID, replayed.ID)
			require.Equal(t, event.Payload, replayed.Payload)
			require.True(t, replayed.Recovered)
			_, err = recovered.Begin(ctx, replayed, "program-restart")
			require.NoError(t, err)
			require.NoError(t, recovered.ApplyInput(ctx, event.ID, "program-restart", map[string]interface{}{"input": float64(1)}))
			result, err := second.Commit(eventCtx, request)
			require.NoError(t, err)
			require.Equal(t, Committed, result.Outcome)
			require.NoError(t, recovered.Complete(ctx, event.ID))
			require.Equal(t, int64(1), secondClient.XLen(ctx, options.OutputStream).Val())
			stats, err := recovered.Stats(ctx)
			require.NoError(t, err)
			require.Zero(t, stats.Pending)
		})
	}
}

func realRedisAddress(t *testing.T) string {
	t.Helper()
	address := os.Getenv("REX_REDIS_TEST_ADDR")
	if address == "" {
		t.Skip("set REX_REDIS_TEST_ADDR to disposable standalone Redis")
	}
	return address
}

func setupRealRedisStore(t *testing.T, db int) *RedisStore {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: realRedisAddress(t), DB: db, MaxRetries: -1})
	require.NoError(t, client.FlushDB(context.Background()).Err())
	t.Cleanup(func() {
		_ = client.FlushDB(context.Background()).Err()
		_ = client.Close()
	})
	return &RedisStore{client: client}
}
