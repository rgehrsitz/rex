package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestFactOwnershipValidation(t *testing.T) {
	for _, names := range [][]string{{}, {"a", "a"}, {""}, {InternalStatePrefix + "x"}, {"rex:durable:x"}} {
		_, err := NewFactOwnership(names)
		require.Error(t, err)
	}
	names := []string{"b", "a"}
	p, err := NewFactOwnership(names)
	require.NoError(t, err)
	names[0] = "changed"
	require.Equal(t, []string{"a", "b"}, p.Names())
	require.NoError(t, p.Validate("b"))
	require.Error(t, p.Validate("changed"))
	p, err = NewFactOwnership(nil)
	require.NoError(t, err)
	require.NoError(t, p.Validate("anything"))
}
func TestPartitionClaimsAndFencing(t *testing.T) {
	server, s := setupMiniredis(t)
	defer server.Close()
	defer s.Close()
	ctx := context.Background()
	options := durableTestOptions(t)
	options.OwnedFacts = []string{"input", "output", "gate"}
	d, err := s.OpenDurable(ctx, options)
	require.NoError(t, err)
	require.NoError(t, d.AcquireOwnership(ctx))
	require.ErrorContains(t, d.ApplyInput(ctx, "1-0", "p", map[string]interface{}{"other": true}), "outside partition")
	require.False(t, server.Exists("other"))
	require.NoError(t, d.ApplyInput(ctx, "1-0", "p", map[string]interface{}{"input": true}))
	eventCtx := WithEvaluationRound(WithDurableEvent(ctx, "1-0", "p"), 0)
	_, err = s.ReadSnapshot(eventCtx, []string{"other"})
	require.ErrorContains(t, err, "outside partition")
	_, err = s.Commit(eventCtx, CommitRequest{Writes: []Write{{Key: "other", Value: true}}})
	require.ErrorContains(t, err, "outside partition")
	require.NoError(t, d.ReleaseOwnership(ctx))
	require.True(t, server.Exists(ownershipRegistry), "claims outlive release")
	err = d.ApplyInput(ctx, "2-0", "p", map[string]interface{}{"input": false})
	require.ErrorIs(t, err, ErrDurableInfrastructure)
	_, err = s.Commit(WithDurableEvent(ctx, "2-0", "p"), CommitRequest{Writes: []Write{{Key: "output", Value: true}}})
	require.ErrorIs(t, err, ErrDurableInfrastructure)
	require.False(t, server.Exists("output"))
	other := &RedisStore{client: redis.NewClient(&redis.Options{Addr: server.Addr()})}
	defer other.Close()
	overlap := durableTestOptions(t)
	overlap.OwnedFacts = []string{"output"}
	_, err = other.OpenDurable(ctx, overlap)
	require.ErrorContains(t, err, "overlap")
	require.False(t, server.Exists(overlap.Stream), "rejected open must not create stream")
	overlap.OwnedFacts = []string{"different"}
	overlap.Stream = "gate"
	_, err = other.OpenDurable(ctx, overlap)
	require.ErrorContains(t, err, "overlap")
	require.False(t, server.Exists("gate"))
	overlap = options
	overlap.OwnedFacts = []string{"input", "output", "gate", "new"}
	_, err = other.OpenDurable(ctx, overlap)
	require.ErrorContains(t, err, "immutable")
	overlap = options
	overlap.Consumer = "successor"
	overlap.OwnedFacts = []string{"output", "gate", "input"}
	successor, err := other.OpenDurable(ctx, overlap)
	require.NoError(t, err)
	require.NoError(t, successor.AcquireOwnership(ctx))
	require.Error(t, d.RenewOwnership(ctx))
	require.NoError(t, successor.RenewOwnership(ctx))
	server.FastForward(2 * time.Second)
	require.Error(t, successor.RenewOwnership(ctx))
	require.True(t, server.Exists(ownershipRegistry))
}
func TestConcurrentPartitionClaims(t *testing.T) {
	server, initial := setupMiniredis(t)
	defer server.Close()
	defer initial.Close()
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := &RedisStore{client: redis.NewClient(&redis.Options{Addr: server.Addr()})}
			defer s.Close()
			options := durableTestOptions(t)
			options.OwnedFacts = []string{"shared"}
			<-start
			_, err := s.OpenDurable(context.Background(), options)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else {
			require.ErrorContains(t, err, "overlap")
		}
	}
	require.Equal(t, 1, success)
}
func TestLegacyManagedIsolation(t *testing.T) {
	for _, managedFirst := range []bool{false, true} {
		server, s := setupMiniredis(t)
		ctx := context.Background()
		first := durableTestOptions(t)
		if managedFirst {
			first.OwnedFacts = []string{"a"}
		}
		_, err := s.OpenDurable(ctx, first)
		require.NoError(t, err)
		other := &RedisStore{client: redis.NewClient(&redis.Options{Addr: server.Addr()})}
		second := durableTestOptions(t)
		if !managedFirst {
			second.OwnedFacts = []string{"b"}
		}
		_, err = other.OpenDurable(ctx, second)
		require.ErrorContains(t, err, "managed and legacy")
		other.Close()
		s.Close()
		server.Close()
	}
}

func TestEnableOwnershipRefusesPendingHistory(t *testing.T) {
	server, s := setupMiniredis(t)
	defer server.Close()
	defer s.Close()
	ctx := context.Background()
	options := durableTestOptions(t)
	// Simulate a pre-M8.6 stream with a delivered event and no registry entry.
	require.NoError(t, s.client.XGroupCreateMkStream(ctx, options.Stream, options.Group, "0").Err())
	require.NoError(t, s.client.XAdd(ctx, &redis.XAddArgs{Stream: options.Stream, Values: map[string]interface{}{"payload": `{"input":1}`}}).Err())
	_, err := s.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: options.Group, Consumer: options.Consumer, Streams: []string{options.Stream, ">"}, Count: 1}).Result()
	require.NoError(t, err)
	options.OwnedFacts = []string{"input"}
	_, err = s.OpenDurable(ctx, options)
	require.ErrorContains(t, err, "pending events")
	require.False(t, server.Exists(ownershipRegistry))
}

type ownershipLossHook struct {
	client *redis.Client
	key    string
	fired  bool
}

func (h *ownershipLossHook) DialHook(next redis.DialHook) redis.DialHook          { return next }
func (h *ownershipLossHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook { return next }
func (h *ownershipLossHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if !h.fired {
			for _, cmd := range cmds {
				if cmd.Name() == "exec" {
					h.fired = true
					if err := h.client.Del(ctx, h.key).Err(); err != nil {
						return err
					}
					break
				}
			}
		}
		return next(ctx, cmds)
	}
}
func TestRealRedisManagedOwnershipRecovery(t *testing.T) {
	address := realRedisAddress(t)
	ctx := context.Background()
	// Separate DB keeps legacy integration fixtures outside this managed domain.
	client := redis.NewClient(&redis.Options{Addr: address, DB: 15, MaxRetries: -1})
	s := &RedisStore{client: client}
	defer s.Close()
	options := durableTestOptions(t)
	input := "input:" + options.Namespace
	output := "output:" + options.Namespace
	options.OwnedFacts = []string{input, output}
	d, err := s.OpenDurable(ctx, options)
	require.NoError(t, err)
	defer client.HDel(ctx, ownershipRegistry, options.Namespace)
	defer client.Del(ctx, options.Stream, options.OutputStream, options.DeadLetter, d.ownerKey(), input, output)
	require.NoError(t, d.AcquireOwnership(ctx))
	event := addDurableEvent(t, d, `{"`+input+`":true}`)
	defer client.Del(ctx, d.journalKey(event.ID))
	_, err = d.Begin(ctx, event, "program")
	require.NoError(t, err)
	require.NoError(t, d.ApplyInput(ctx, event.ID, "program", map[string]interface{}{input: true}))
	eventCtx := WithDurableEvent(ctx, event.ID, "program")
	request := CommitRequest{Writes: []Write{{Key: output, Value: true}}}
	hook := &ownershipLossHook{client: client, key: d.ownerKey()}
	d.client.AddHook(hook)
	result, err := s.Commit(eventCtx, request)
	require.ErrorIs(t, err, ErrDurableInfrastructure)
	require.NotEqual(t, Committed, result.Outcome)
	require.True(t, hook.fired)
	require.Equal(t, int64(0), client.Exists(ctx, output).Val())
	require.Equal(t, int64(0), client.XLen(ctx, options.OutputStream).Val())
	secondClient := redis.NewClient(&redis.Options{Addr: address, DB: 15, MaxRetries: -1})
	second := &RedisStore{client: secondClient}
	defer second.Close()
	successor, err := second.OpenDurable(ctx, options)
	require.NoError(t, err)
	require.NoError(t, successor.AcquireOwnership(ctx))
	defer successor.ReleaseOwnership(ctx)
	require.NoError(t, successor.ApplyInput(ctx, event.ID, "program", map[string]interface{}{input: true}))
	replyHook := &lostTransactionReplyHook{}
	successor.client.AddHook(replyHook)
	result, err = second.Commit(eventCtx, request)
	require.Error(t, err)
	require.Equal(t, Unknown, result.Outcome)
	result, err = second.Commit(eventCtx, request)
	require.NoError(t, err)
	require.Equal(t, Committed, result.Outcome)
	require.Equal(t, int64(1), client.XLen(ctx, options.OutputStream).Val())
	// Renewal and terminal completion must retry benign watched-key changes.
	registryRefresh := &beforeOwnershipExecHook{once: true, before: func(ctx context.Context) error {
		return client.HSet(ctx, ownershipRegistry, options.Namespace, successor.claim()).Err()
	}}
	successor.client.AddHook(registryRefresh)
	require.NoError(t, successor.RenewOwnership(ctx))
	require.Equal(t, 1, registryRefresh.calls)
	leaseRefresh := &beforeOwnershipExecHook{once: true, before: func(ctx context.Context) error {
		return client.PExpire(ctx, successor.ownerKey(), options.LockTTL).Err()
	}}
	successor.client.AddHook(leaseRefresh)
	require.NoError(t, successor.Complete(ctx, event.ID))
	require.Equal(t, 1, leaseRefresh.calls)
	pending, err := client.XPending(ctx, options.Stream, options.Group).Result()
	require.NoError(t, err)
	require.Zero(t, pending.Count)
}

func TestLegacyMarkerDoesNotGrowWithNamespaces(t *testing.T) {
	server, s := setupMiniredis(t)
	defer server.Close()
	defer s.Close()
	ctx := context.Background()
	for i := 0; i < 300; i++ {
		d := &RedisDurable{client: s.client, options: durableTestOptions(t)}
		require.NoError(t, d.reserveFactOwnership(ctx, false))
	}
	require.Equal(t, int64(1), s.client.HLen(ctx, ownershipRegistry).Val())
	require.Equal(t, "legacy", s.client.HGet(ctx, ownershipRegistry, legacyOwnershipMarker).Val())
}
func TestManagedPreflightAndOldOwner(t *testing.T) {
	server, s := setupMiniredis(t)
	defer server.Close()
	defer s.Close()
	ctx := context.Background()
	options := durableTestOptions(t)
	options.OwnedFacts = []string{"fact"}
	require.NoError(t, s.client.Set(ctx, options.OutputStream, "bad", 0).Err())
	_, err := s.OpenDurable(ctx, options)
	require.ErrorContains(t, err, "has type string")
	require.False(t, server.Exists(ownershipRegistry))
	require.NoError(t, s.client.Del(ctx, options.OutputStream).Err())
	ownerKey := "rex:durable:" + options.Namespace + ":owner"
	require.NoError(t, s.client.Set(ctx, ownerKey, "old-binary", time.Minute).Err())
	_, err = s.OpenDurable(ctx, options)
	require.ErrorIs(t, err, ErrDurableOwnership)
	require.False(t, server.Exists(ownershipRegistry))
}
func TestSuccessorFencesEveryManagedEffect(t *testing.T) {
	server, s := setupMiniredis(t)
	defer server.Close()
	defer s.Close()
	ctx := context.Background()
	options := durableTestOptions(t)
	options.OwnedFacts = []string{"fact"}
	d, err := s.OpenDurable(ctx, options)
	require.NoError(t, err)
	require.NoError(t, d.AcquireOwnership(ctx))
	event := addDurableEvent(t, d, `{"fact":true}`)
	_, err = d.Begin(ctx, event, "p")
	require.NoError(t, err)
	server.FastForward(2 * time.Second)
	other := &RedisStore{client: redis.NewClient(&redis.Options{Addr: server.Addr()})}
	defer other.Close()
	successor, err := other.OpenDurable(ctx, options)
	require.NoError(t, err)
	require.NoError(t, successor.AcquireOwnership(ctx))
	_, err = d.Begin(ctx, event, "p")
	require.ErrorIs(t, err, ErrDurableOwnership)
	require.ErrorIs(t, d.Complete(ctx, event.ID), ErrDurableOwnership)
	require.ErrorIs(t, d.Acknowledge(ctx, event.ID), ErrDurableOwnership)
	require.ErrorIs(t, d.DeadLetterEvent(ctx, event, "p", 3, context.Canceled), ErrDurableOwnership)
	eventCtx := WithDurableEvent(ctx, event.ID, "p")
	_, err = s.ReadSnapshot(eventCtx, []string{"fact"})
	require.ErrorIs(t, err, ErrDurableOwnership)
	_, err = s.Commit(eventCtx, CommitRequest{Writes: []Write{{Key: "fact", Value: true}}})
	require.ErrorIs(t, err, ErrDurableOwnership)
	timer := InternalStatePrefix + "test"
	require.NoError(t, s.client.Set(ctx, timer, "state", 0).Err())
	require.ErrorIs(t, s.DeleteInternal(ctx, []string{timer}), ErrDurableOwnership)
	require.True(t, server.Exists(timer))
	require.NoError(t, other.DeleteInternal(ctx, []string{timer}))
	require.False(t, server.Exists(timer))
	require.NoError(t, successor.Complete(ctx, event.ID))
}
func TestConcurrentLegacyManagedOpen(t *testing.T) {
	server, s := setupMiniredis(t)
	defer server.Close()
	defer s.Close()
	ctx := context.Background()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, managed := range []bool{false, true} {
		go func(managed bool) {
			other := &RedisStore{client: redis.NewClient(&redis.Options{Addr: server.Addr()})}
			defer other.Close()
			options := durableTestOptions(t)
			if managed {
				options.OwnedFacts = []string{"fact"}
			}
			<-start
			_, err := other.OpenDurable(ctx, options)
			results <- err
		}(managed)
	}
	close(start)
	success := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else {
			require.ErrorContains(t, err, "managed and legacy")
		}
	}
	require.Equal(t, 1, success)
}

func TestRealRedisConcurrentManagedClaims(t *testing.T) {
	address := realRedisAddress(t)
	ctx := context.Background()
	admin := redis.NewClient(&redis.Options{Addr: address, DB: 15})
	defer admin.Close()
	options := []DurableOptions{durableTestOptions(t), durableTestOptions(t)}
	shared := "shared:" + options[0].Namespace
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, opt := range options {
		opt.OwnedFacts = []string{shared}
		defer admin.HDel(ctx, ownershipRegistry, opt.Namespace)
		defer admin.Del(ctx, opt.Stream, opt.OutputStream, opt.DeadLetter)
		go func(o DurableOptions) {
			s := &RedisStore{client: redis.NewClient(&redis.Options{Addr: address, DB: 15, MaxRetries: -1})}
			defer s.Close()
			<-start
			_, err := s.OpenDurable(ctx, o)
			results <- err
		}(opt)
	}
	close(start)
	success := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else {
			require.ErrorContains(t, err, "overlap")
		}
	}
	require.Equal(t, 1, success)
}

func TestDisjointClaimsAndIdempotentRestart(t *testing.T) {
	server, s := setupMiniredis(t)
	defer server.Close()
	defer s.Close()
	ctx := context.Background()
	first := durableTestOptions(t)
	first.OwnedFacts = []string{"east"}
	d, err := s.OpenDurable(ctx, first)
	require.NoError(t, err)
	require.NoError(t, d.AcquireOwnership(ctx))
	other := &RedisStore{client: redis.NewClient(&redis.Options{Addr: server.Addr()})}
	defer other.Close()
	second := durableTestOptions(t)
	second.OwnedFacts = []string{"west"}
	two, err := other.OpenDurable(ctx, second)
	require.NoError(t, err)
	require.NoError(t, two.AcquireOwnership(ctx))
	require.NoError(t, d.ApplyInput(ctx, "1-0", "p", map[string]interface{}{"east": true}))
	require.NoError(t, two.ApplyInput(ctx, "1-0", "p2", map[string]interface{}{"west": false}))
	// The registry is watched by active partitions: reopening an unchanged
	// claim must not modify it and cause unrelated transactions to abort.
	restart := &RedisStore{client: redis.NewClient(&redis.Options{Addr: server.Addr()})}
	defer restart.Close()
	err = s.client.Watch(ctx, func(tx *redis.Tx) error {
		_, err := restart.OpenDurable(ctx, second)
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error { pipe.Set(ctx, "probe", true, 0); return nil })
		return err
	}, ownershipRegistry)
	require.NoError(t, err)
}

type beforeOwnershipExecHook struct {
	before func(context.Context) error
	calls  int
	once   bool
}

func (h *beforeOwnershipExecHook) DialHook(next redis.DialHook) redis.DialHook          { return next }
func (h *beforeOwnershipExecHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook { return next }
func (h *beforeOwnershipExecHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			if cmd.Name() == "exec" && (!h.once || h.calls == 0) {
				h.calls++
				if err := h.before(ctx); err != nil {
					return err
				}
				break
			}
		}
		return next(ctx, cmds)
	}
}
func TestUnchangedAcquireIgnoresProducerWrites(t *testing.T) {
	for _, managed := range []bool{false, true} {
		server, s := setupMiniredis(t)
		ctx := context.Background()
		options := durableTestOptions(t)
		if managed {
			options.OwnedFacts = []string{"fact"}
		}
		d, err := s.OpenDurable(ctx, options)
		require.NoError(t, err)
		hook := &beforeOwnershipExecHook{before: func(ctx context.Context) error {
			for _, key := range []string{options.Stream, options.OutputStream, options.DeadLetter} {
				if err := s.client.XAdd(ctx, &redis.XAddArgs{Stream: key, Values: map[string]interface{}{"payload": "{}"}}).Err(); err != nil {
					return err
				}
			}
			return nil
		}}
		d.client.AddHook(hook)
		require.NoError(t, d.AcquireOwnership(ctx))
		require.Equal(t, 1, hook.calls)
		s.Close()
		server.Close()
	}
}
func TestManagedRenewalRetriesRegistryChange(t *testing.T) {
	server, s := setupMiniredis(t)
	defer server.Close()
	defer s.Close()
	ctx := context.Background()
	options := durableTestOptions(t)
	options.OwnedFacts = []string{"fact"}
	d, err := s.OpenDurable(ctx, options)
	require.NoError(t, err)
	require.NoError(t, d.AcquireOwnership(ctx))
	hook := &beforeOwnershipExecHook{once: true, before: func(ctx context.Context) error {
		return s.client.HSet(ctx, ownershipRegistry, options.Namespace, d.claim()).Err()
	}}
	d.client.AddHook(hook)
	require.NoError(t, d.RenewOwnership(ctx))
	require.Equal(t, 1, hook.calls)
	require.Equal(t, d.ownerID, s.client.Get(ctx, d.ownerKey()).Val())
}
