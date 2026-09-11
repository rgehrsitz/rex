package runtime

// Opt-in M8.8 experiment. The runner supplies a fresh, private Redis process;
// ordinary tests skip it. This proves concurrent partition behavior without
// adding a production supervisor or changing the supported daemon contract.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"
)

type concurrencySample struct {
	Partition  int    `json:"partition"`
	Sequence   int    `json:"sequence"`
	EventID    string `json:"event_id"`
	ServiceNS  int64  `json:"service_ns"`
	EndToEndNS int64  `json:"backlog_completion_ns"`
	Attempts   int64  `json:"attempts"`
	Recovered  bool   `json:"recovered"`
}

type concurrencyResult struct {
	Name          string              `json:"name"`
	Workers       int                 `json:"workers"`
	Rules         int                 `json:"rules_per_partition"`
	Affected      int                 `json:"affected_rules"`
	Counts        []int               `json:"partition_events"`
	ElapsedNS     int64               `json:"elapsed_ns"`
	ProducerNS    int64               `json:"producer_ns"`
	FaultPathNS   int64               `json:"fault_path_ns"`
	AllocBytes    uint64              `json:"drain_alloc_bytes"`
	AllocObjects  uint64              `json:"drain_alloc_objects"`
	RedisCommands int64               `json:"drain_redis_commands"`
	RedisCPU      float64             `json:"drain_redis_cpu_seconds"`
	Windows       []concurrencyWindow `json:"partition_windows"`
	FaultSiblings int64               `json:"fault_sibling_completions"`
	Samples       []concurrencySample `json:"samples"`
}

type concurrencyWindow struct {
	Partition    int   `json:"partition"`
	StartNS      int64 `json:"start_ns"`
	LastFinishNS int64 `json:"last_finish_ns"`
}

type concurrencyPartition struct {
	index       int
	engine      *Engine
	queue       *store.RedisDurable
	replacement *store.RedisStore
	options     store.DurableOptions
	artifact    []byte
	ids         []string
	submitted   []time.Time
	warmup      int
}

type concurrencyPartitionResult struct {
	samples       []concurrencySample
	faultPathNS   int64
	faultSiblings int64
	window        concurrencyWindow
}

func TestDurableConcurrencyProfile(t *testing.T) {
	addr, token := os.Getenv("REX_CONCURRENCY_ADDR"), os.Getenv("REX_CONCURRENCY_TOKEN")
	if addr == "" || token == "" {
		t.Skip("use scripts/durable-concurrency/run.py")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	require.Equal(t, token, client.Get(ctx, "rex-concurrency-token").Val(), "runner must own this Redis instance")
	require.Equal(t, int64(1), client.DBSize(ctx).Val(), "measurement requires fresh Redis")

	events, err := strconv.Atoi(os.Getenv("REX_CONCURRENCY_EVENTS"))
	require.NoError(t, err)
	require.True(t, events >= 100 && events <= 10000 && events%100 == 0)
	workers, err := strconv.Atoi(os.Getenv("REX_CONCURRENCY_WORKERS"))
	require.NoError(t, err)
	require.Contains(t, []int{1, 2, 4}, workers)
	scenario := os.Getenv("REX_CONCURRENCY_SCENARIO")
	require.Contains(t, []string{"sparse-balanced", "dense-balanced", "sparse-skew", "completion-retry", "owner-loss"}, scenario)

	result := concurrencyResult{Name: scenario, Workers: workers, Rules: 100, Affected: 1, Counts: make([]int, workers)}
	if scenario == "dense-balanced" {
		result.Affected = 100
	}
	parts := make([]*concurrencyPartition, workers)
	for p := range parts {
		prefix := fmt.Sprintf("m88-%s-w%d-p%d", scenario, workers, p)
		rules := compiler.Ruleset{}
		for r := 0; r < result.Rules; r++ {
			trigger := fmt.Sprintf("%s.in.%d", prefix, r)
			if result.Affected == 100 {
				trigger = prefix + ".in.0"
			}
			rules.Rules = append(rules.Rules, compiler.Rule{
				Name:       fmt.Sprintf("r%d", r),
				Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{Fact: trigger, Operator: "EQ", Value: true}}},
				Actions:    []compiler.Action{{Type: "updateStore", Target: fmt.Sprintf("%s.out.%d", prefix, r), Value: true}},
			})
		}
		raw, err := json.Marshal(rules)
		require.NoError(t, err)
		artifact, err := compiler.CompileBatch(raw)
		require.NoError(t, err)
		program, err := LoadProgram(artifact)
		require.NoError(t, err)
		options := store.DurableOptions{
			Namespace: prefix, Stream: prefix + ":in", Group: "profile", Consumer: "first",
			OutputStream: prefix + ":out", DeadLetter: prefix + ":dead",
			OwnedFacts: append(program.OwnershipFacts(), prefix+".seq"), LockTTL: 10 * time.Minute,
			ClaimIdle: time.Millisecond, Block: time.Millisecond, JournalTTL: time.Hour,
			MaxAttempts: 3, OutputMaxLen: 100000,
		}
		backend, err := store.NewRedisStore(ctx, store.RedisOptions{Addr: addr})
		require.NoError(t, err)
		t.Cleanup(func() { _ = backend.Close() })
		queue, err := backend.OpenDurable(ctx, options)
		require.NoError(t, err)
		require.NoError(t, queue.AcquireOwnership(ctx))
		engine, err := NewEngineFromBytes(artifact, backend, 0)
		require.NoError(t, err)
		engine.SetConditionTracing(false)
		require.NoError(t, engine.ValidateFactOwnership(options.OwnedFacts))
		parts[p] = &concurrencyPartition{index: p, engine: engine, queue: queue, options: options, artifact: artifact}
	}
	defer func() {
		for _, part := range parts {
			if part.replacement != nil {
				_ = part.replacement.Close()
			}
		}
	}()

	// Give every independent engine the same warmup before recording work.
	for _, part := range parts {
		for i := 0; i < 100; i++ {
			payload, err := json.Marshal(map[string]interface{}{part.options.Namespace + ".in.0": true})
			require.NoError(t, err)
			id, err := client.XAdd(ctx, &redis.XAddArgs{Stream: part.options.Stream, Values: map[string]interface{}{"payload": string(payload)}}).Result()
			require.NoError(t, err)
			got, err := part.engine.ProcessNextDurable(ctx, part.queue)
			require.NoError(t, err)
			require.True(t, got.Processed)
			require.Equal(t, id, got.EventID)
			part.warmup++
		}
	}

	producerStart := time.Now()
	for i := 0; i < events; i++ {
		p := i % workers
		if scenario == "sparse-skew" && workers > 1 {
			p = 0
			if i%10 == 9 {
				p = 1 + (i/10)%(workers-1)
			}
		}
		part := parts[p]
		seq := len(part.ids)
		payload, err := json.Marshal(map[string]interface{}{
			part.options.Namespace + ".in.0": true,
			part.options.Namespace + ".seq":  seq,
		})
		require.NoError(t, err)
		submitted := time.Now()
		id, err := client.XAdd(ctx, &redis.XAddArgs{Stream: part.options.Stream, Values: map[string]interface{}{"payload": string(payload)}}).Result()
		require.NoError(t, err)
		part.ids = append(part.ids, id)
		part.submitted = append(part.submitted, submitted)
		result.Counts[p]++
	}
	result.ProducerNS = time.Since(producerStart).Nanoseconds()

	commands := func() int64 {
		info, err := client.Info(ctx, "stats").Result()
		require.NoError(t, err)
		for _, line := range strings.Split(info, "\n") {
			if strings.HasPrefix(line, "total_commands_processed:") {
				n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "total_commands_processed:")), 10, 64)
				require.NoError(t, err)
				return n
			}
		}
		t.Fatal("Redis command count unavailable")
		return 0
	}
	redisCPU := func() float64 {
		info, err := client.Info(ctx, "cpu").Result()
		require.NoError(t, err)
		var total float64
		var found int
		for _, line := range strings.Split(info, "\n") {
			if strings.HasPrefix(line, "used_cpu_sys:") || strings.HasPrefix(line, "used_cpu_user:") {
				value, err := strconv.ParseFloat(strings.TrimSpace(strings.SplitN(line, ":", 2)[1]), 64)
				require.NoError(t, err)
				total += value
				found++
			}
		}
		require.Equal(t, 2, found, "Redis CPU counters unavailable")
		return total
	}
	redisCPUStart := redisCPU()
	commandStart := commands()
	var before, after goruntime.MemStats
	goruntime.GC()
	goruntime.ReadMemStats(&before)
	startGate := make(chan struct{})
	partitionResults := make([]concurrencyPartitionResult, workers)
	errorsByPartition := make([]error, workers)
	completed := make([]atomic.Int64, workers)
	workerCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	var faultGate chan struct{}
	if workers > 1 && scenario == "owner-loss" {
		faultGate = make(chan struct{})
	}
	var wg sync.WaitGroup
	wg.Add(workers)
	start := time.Now()
	for p, part := range parts {
		go func(p int, part *concurrencyPartition) {
			defer wg.Done()
			<-startGate
			partitionResults[p], errorsByPartition[p] = runConcurrencyPartition(workerCtx, client, part, scenario, start, completed, faultGate)
			if errorsByPartition[p] != nil {
				stopWorkers()
			}
		}(p, part)
	}
	close(startGate)
	wg.Wait()
	result.ElapsedNS = time.Since(start).Nanoseconds()
	goruntime.ReadMemStats(&after)
	result.AllocBytes = after.TotalAlloc - before.TotalAlloc
	result.AllocObjects = after.Mallocs - before.Mallocs
	result.RedisCPU = redisCPU() - redisCPUStart
	// The starting command-count INFO and ending CPU INFO increment the total
	// after producing their responses, so both appear in the final observation.
	result.RedisCommands = commands() - commandStart - 2
	for p, runErr := range errorsByPartition {
		require.NoErrorf(t, runErr, "partition %d", p)
		result.Samples = append(result.Samples, partitionResults[p].samples...)
		result.Windows = append(result.Windows, partitionResults[p].window)
		if partitionResults[p].faultPathNS > result.FaultPathNS {
			result.FaultPathNS = partitionResults[p].faultPathNS
		}
		result.FaultSiblings += partitionResults[p].faultSiblings
	}
	if workers > 1 {
		maxStart, minLast := int64(0), result.Windows[0].LastFinishNS
		for _, window := range result.Windows {
			if window.StartNS > maxStart {
				maxStart = window.StartNS
			}
			if window.LastFinishNS < minLast {
				minLast = window.LastFinishNS
			}
		}
		require.LessOrEqual(t, maxStart, minLast, "partition processing windows did not overlap")
		if scenario == "owner-loss" {
			require.Positive(t, result.FaultSiblings, "no sibling completed work during owner recovery")
		}
	}

	for p, part := range parts {
		stats, err := part.queue.Stats(ctx)
		require.NoError(t, err)
		require.Zero(t, stats.Pending)
		require.Equal(t, int64(0), client.XLen(ctx, part.options.DeadLetter).Val())
		outputs, err := client.XRange(ctx, part.options.OutputStream, "-", "+").Result()
		require.NoError(t, err)
		require.Len(t, outputs, part.warmup+result.Counts[p])
		for i, output := range outputs[part.warmup:] {
			require.Equal(t, part.ids[i], output.Values["input_id"])
			var payload struct {
				WriteIDs map[string]string `json:"write_ids"`
			}
			require.NoError(t, json.Unmarshal([]byte(output.Values["payload"].(string)), &payload))
			require.Len(t, payload.WriteIDs, result.Affected)
		}
		require.Equal(t, strconv.Itoa(result.Counts[p]-1), client.Get(ctx, part.options.Namespace+".seq").Val())
		for r := 0; r < result.Affected; r++ {
			require.Equal(t, "true", client.Get(ctx, fmt.Sprintf("%s.out.%d", part.options.Namespace, r)).Val())
		}
		require.NoError(t, part.queue.ReleaseOwnership(ctx))
		require.Equal(t, int64(0), client.Exists(ctx, "rex:durable:"+part.options.Namespace+":owner").Val(), "ownership key remains after release")
	}
	require.False(t, t.Failed(), "do not publish partial measurement results")
	data, err := json.MarshalIndent([]concurrencyResult{result}, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(os.Getenv("REX_CONCURRENCY_OUTPUT"), append(data, '\n'), 0o644))
}

func runConcurrencyPartition(ctx context.Context, client *redis.Client, part *concurrencyPartition, scenario string, drainStart time.Time, completed []atomic.Int64, faultGate chan struct{}) (concurrencyPartitionResult, error) {
	result := concurrencyPartitionResult{
		samples: make([]concurrencySample, 0, len(part.ids)),
		window:  concurrencyWindow{Partition: part.index},
	}
	if faultGate != nil && part.index != 0 {
		select {
		case <-faultGate:
		case <-ctx.Done():
			return result, ctx.Err()
		}
	}
	result.window.StartNS = time.Since(drainStart).Nanoseconds()
	for sequence, id := range part.ids {
		serviceStart := time.Now()
		var got DurableProcessResult
		var err error
		if part.index == 0 && sequence == 0 && (scenario == "completion-retry" || scenario == "owner-loss") {
			faultStart := time.Now()
			var siblingStart int64
			for p := range completed {
				if p != part.index {
					siblingStart += completed[p].Load()
				}
			}
			if faultGate != nil {
				close(faultGate)
			}
			faultQueue := &profileFaultQueue{RedisDurable: part.queue}
			faultQueue.fault = func(faultCtx context.Context) error {
				if scenario == "completion-retry" {
					return fmt.Errorf("injected pre-completion failure")
				}
				if err := client.PExpire(faultCtx, "rex:durable:"+part.options.Namespace+":owner", 100*time.Millisecond).Err(); err != nil {
					return err
				}
				time.Sleep(110 * time.Millisecond)
				return part.queue.Complete(faultCtx, id)
			}
			got, err = part.engine.ProcessNextDurable(ctx, faultQueue)
			if err == nil || !got.RetryPending {
				return result, fmt.Errorf("faulted event did not remain pending: result=%+v err=%v", got, err)
			}
			if scenario == "owner-loss" {
				if !errors.Is(err, store.ErrDurableOwnership) {
					return result, fmt.Errorf("owner loss returned %w", err)
				}
				replacement, openErr := store.NewRedisStore(ctx, store.RedisOptions{Addr: client.Options().Addr})
				if openErr != nil {
					return result, openErr
				}
				options := part.options
				options.Consumer = "successor"
				successor, openErr := replacement.OpenDurable(ctx, options)
				if openErr != nil {
					_ = replacement.Close()
					return result, openErr
				}
				if openErr = successor.AcquireOwnership(ctx); openErr != nil {
					_ = replacement.Close()
					return result, openErr
				}
				if renewErr := part.queue.RenewOwnership(ctx); !errors.Is(renewErr, store.ErrDurableOwnership) {
					_ = replacement.Close()
					return result, fmt.Errorf("stale owner renewed unexpectedly: %v", renewErr)
				}
				staleResult, staleErr := part.engine.ProcessNextDurable(ctx, part.queue)
				if !errors.Is(staleErr, store.ErrDurableOwnership) || staleResult.EventID != id {
					_ = replacement.Close()
					return result, fmt.Errorf("stale owner was not fenced: result=%+v err=%v", staleResult, staleErr)
				}
				engine, openErr := NewEngineFromBytes(part.artifact, replacement, 0)
				if openErr != nil {
					_ = replacement.Close()
					return result, openErr
				}
				engine.SetConditionTracing(false)
				if openErr = engine.ValidateFactOwnership(options.OwnedFacts); openErr != nil {
					_ = replacement.Close()
					return result, openErr
				}
				part.queue, part.engine, part.replacement = successor, engine, replacement
			}
			got, err = part.engine.ProcessNextDurable(ctx, part.queue)
			result.faultPathNS = time.Since(faultStart).Nanoseconds()
			for p := range completed {
				if p != part.index {
					result.faultSiblings += completed[p].Load()
				}
			}
			result.faultSiblings -= siblingStart
			if err != nil || !got.Processed || got.DeadLettered || !got.Recovered || got.Attempts != 2 {
				return result, fmt.Errorf("fault recovery failed: result=%+v err=%v", got, err)
			}
		} else {
			got, err = part.engine.ProcessNextDurable(ctx, part.queue)
			if err != nil || !got.Processed || got.Attempts != 1 || got.Recovered {
				return result, fmt.Errorf("ordinary processing failed: result=%+v err=%v", got, err)
			}
		}
		finished := time.Now()
		result.window.LastFinishNS = finished.Sub(drainStart).Nanoseconds()
		if got.EventID != id {
			return result, fmt.Errorf("event order changed: got %s, want %s", got.EventID, id)
		}
		result.samples = append(result.samples, concurrencySample{
			Partition: part.index, Sequence: sequence, EventID: got.EventID,
			ServiceNS: finished.Sub(serviceStart).Nanoseconds(), EndToEndNS: finished.Sub(part.submitted[sequence]).Nanoseconds(),
			Attempts: got.Attempts, Recovered: got.Recovered,
		})
		completed[part.index].Add(1)
	}
	return result, nil
}
