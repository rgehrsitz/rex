package runtime

// Opt-in measurement harness. The runner supplies a fresh, private Redis process;
// ordinary tests skip it. No production scheduling or concurrency is introduced.
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"
)

type profileSample struct {
	Partition  int    `json:"partition"`
	EventID    string `json:"event_id"`
	ServiceNS  int64  `json:"service_ns"`
	EndToEndNS int64  `json:"end_to_end_ns"`
	Attempts   int64  `json:"attempts"`
	Recovered  bool   `json:"recovered"`
}
type profileResult struct {
	AllocBytes    uint64          `json:"drain_alloc_bytes"`
	AllocObjects  uint64          `json:"drain_alloc_objects"`
	RedisCommands int64           `json:"drain_redis_commands"`
	Name          string          `json:"name"`
	Rules         int             `json:"rules_per_partition"`
	Affected      int             `json:"affected_rules"`
	Counts        []int           `json:"partition_events"`
	ElapsedNS     int64           `json:"elapsed_ns"`
	ProducerNS    int64           `json:"producer_ns"`
	FaultPathNS   int64           `json:"fault_path_ns"`
	Samples       []profileSample `json:"samples"`
}

// Fail immediately before completion, after effects have committed. A retry must
// replay the journal without duplicating output. Lease expiry is real Redis time.
type profileFaultQueue struct {
	*store.RedisDurable
	fault func(context.Context) error
}

func (q *profileFaultQueue) Complete(ctx context.Context, id string) error {
	if q.fault != nil {
		f := q.fault
		q.fault = nil
		return f(ctx)
	}
	return q.RedisDurable.Complete(ctx, id)
}

type profilePartition struct {
	engine    *Engine
	queue     *store.RedisDurable
	options   store.DurableOptions
	artifact  []byte
	ids       []string
	submitted []time.Time
}

func TestDurableProfile(t *testing.T) {
	addr, token := os.Getenv("REX_PROFILE_ADDR"), os.Getenv("REX_PROFILE_TOKEN")
	if addr == "" || token == "" {
		t.Skip("use scripts/durable-profile/run.py")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	require.Equal(t, token, client.Get(ctx, "rex-profile-token").Val(), "runner must own this Redis instance")
	require.Equal(t, int64(1), client.DBSize(ctx).Val(), "measurement requires fresh Redis")
	count, err := strconv.Atoi(os.Getenv("REX_PROFILE_EVENTS"))
	require.NoError(t, err)
	require.True(t, count >= 100 && count <= 10000 && count%100 == 0)
	selected := os.Getenv("REX_PROFILE_SCENARIO")
	results := []profileResult{}
	for scenarioIndex, scenario := range []struct {
		name        string
		dense, skew bool
		fault       string
	}{
		{name: "sparse-balanced"}, {name: "dense-balanced", dense: true},
		{name: "sparse-skew", skew: true}, {name: "completion-retry", fault: "retry"},
		{name: "owner-loss", fault: "owner"},
	} {
		if selected != "" && scenario.name != selected {
			continue
		}
		t.Run(scenario.name, func(t *testing.T) {
			result := profileResult{Name: scenario.name, Rules: 100, Affected: 1, Counts: make([]int, 4)}
			warmupCounts := make([]int, 4)
			if scenario.dense {
				result.Affected = 100
			}
			parts := make([]profilePartition, 4)
			for p := range parts {
				prefix := fmt.Sprintf("m87s%d-p%d", scenarioIndex, p)
				rules := compiler.Ruleset{}
				for r := 0; r < 100; r++ {
					trigger := fmt.Sprintf("%s.in.%d", prefix, r)
					if scenario.dense {
						trigger = prefix + ".in.0"
					}
					rules.Rules = append(rules.Rules, compiler.Rule{Name: fmt.Sprintf("r%d", r), Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{Fact: trigger, Operator: "EQ", Value: true}}}, Actions: []compiler.Action{{Type: "updateStore", Target: fmt.Sprintf("%s.out.%d", prefix, r), Value: true}}})
				}
				raw, err := json.Marshal(rules)
				require.NoError(t, err)
				artifact, err := compiler.CompileBatch(raw)
				require.NoError(t, err)
				program, err := LoadProgram(artifact)
				require.NoError(t, err)
				options := store.DurableOptions{Namespace: prefix, Stream: prefix + ":in", Group: "profile", Consumer: "first", OutputStream: prefix + ":out", DeadLetter: prefix + ":dead", OwnedFacts: append(program.OwnershipFacts(), prefix+".seq"), LockTTL: 10 * time.Minute, ClaimIdle: time.Millisecond, Block: time.Millisecond, JournalTTL: time.Hour, MaxAttempts: 3, OutputMaxLen: 100000}

				rs, err := store.NewRedisStore(ctx, store.RedisOptions{Addr: addr})
				require.NoError(t, err)
				t.Cleanup(func() { _ = rs.Close() })
				q, err := rs.OpenDurable(ctx, options)
				require.NoError(t, err)
				require.NoError(t, q.AcquireOwnership(ctx))
				engine, err := NewEngineFromBytes(artifact, rs, 0)
				require.NoError(t, err)
				engine.SetConditionTracing(false)
				require.NoError(t, engine.ValidateFactOwnership(options.OwnedFacts))
				parts[p] = profilePartition{engine: engine, queue: q, options: options, artifact: artifact}
			}
			// Exercise connections, scripts, journals, and the evaluator before the
			// measured finite backlog. Warmup events remain independently verified.
			for i := 0; i < 100; i++ {
				p := i % len(parts)
				part := &parts[p]
				payload, err := json.Marshal(map[string]interface{}{part.options.Namespace + ".in.0": true})
				require.NoError(t, err)
				id, err := client.XAdd(ctx, &redis.XAddArgs{Stream: part.options.Stream, Values: map[string]interface{}{"payload": string(payload)}}).Result()
				require.NoError(t, err)
				got, err := part.engine.ProcessNextDurable(ctx, part.queue)
				require.NoError(t, err)
				require.True(t, got.Processed)
				require.Equal(t, id, got.EventID)
				warmupCounts[p]++
			}
			// Finite backlog, all submissions timed before drain. This is deliberately
			// not an open-loop arrival model or a measurement of daemon ingress.
			producerStart := time.Now()
			for i := 0; i < count; i++ {
				p := i % 4
				if scenario.skew {
					p = 0
					if i%10 == 9 {
						p = 1 + (i/10)%3
					}
				}
				part := &parts[p]
				seq := len(part.ids)
				payload, err := json.Marshal(map[string]interface{}{fmt.Sprintf("%s.in.0", part.options.Namespace): true, part.options.Namespace + ".seq": seq})
				require.NoError(t, err)
				submitted := time.Now()
				id, err := client.XAdd(ctx, &redis.XAddArgs{Stream: part.options.Stream, Values: map[string]interface{}{"payload": string(payload)}}).Result()
				require.NoError(t, err)
				part.ids = append(part.ids, id)
				part.submitted = append(part.submitted, submitted)
				result.Counts[p]++
			}
			result.ProducerNS = time.Since(producerStart).Nanoseconds()
			// Every partition is processed by the same serial round-robin driver.
			// No aggregate parallel speedup is implied by partitioning this workload.
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
			commandStart := commands()
			var before, after goruntime.MemStats
			goruntime.GC()
			goruntime.ReadMemStats(&before)
			offsets := make([]int, 4)
			done := 0
			start := time.Now()
			for done < count {
				for p := range parts {
					part := &parts[p]
					i := offsets[p]
					if i == len(part.ids) {
						continue
					}
					serviceStart := time.Now()
					var got DurableProcessResult
					var err error
					if scenario.fault != "" && p == 0 && i == 0 {
						faultStart := time.Now()
						q := &profileFaultQueue{RedisDurable: part.queue}
						q.fault = func(ctx context.Context) error {
							if scenario.fault == "retry" {
								return fmt.Errorf("injected pre-completion failure")
							}
							// Expire the real owner lease only now, avoiding expiry during setup.
							require.NoError(t, client.PExpire(ctx, "rex:durable:"+part.options.Namespace+":owner", 100*time.Millisecond).Err())
							time.Sleep(110 * time.Millisecond)
							return part.queue.Complete(ctx, part.ids[i])
						}
						got, err = part.engine.ProcessNextDurable(ctx, q)
						require.Error(t, err)
						require.True(t, got.RetryPending)
						if scenario.fault == "owner" {
							require.ErrorIs(t, err, store.ErrDurableOwnership)
							rs, openErr := store.NewRedisStore(ctx, store.RedisOptions{Addr: addr})
							require.NoError(t, openErr)
							t.Cleanup(func() { _ = rs.Close() })
							options := part.options
							options.Consumer = "successor"
							successor, openErr := rs.OpenDurable(ctx, options)
							require.NoError(t, openErr)
							require.NoError(t, successor.AcquireOwnership(ctx))
							require.ErrorIs(t, part.queue.RenewOwnership(ctx), store.ErrDurableOwnership)
							staleResult, staleErr := part.engine.ProcessNextDurable(ctx, part.queue)
							require.ErrorIs(t, staleErr, store.ErrDurableOwnership)
							require.Equal(t, part.ids[i], staleResult.EventID)
							part.queue = successor
							part.engine, openErr = NewEngineFromBytes(part.artifact, rs, 0)
							require.NoError(t, openErr)
							part.engine.SetConditionTracing(false)
							require.NoError(t, part.engine.ValidateFactOwnership(options.OwnedFacts))
						}
						got, err = part.engine.ProcessNextDurable(ctx, part.queue)
						result.FaultPathNS = time.Since(faultStart).Nanoseconds()
						require.True(t, got.Recovered)
						require.Equal(t, int64(2), got.Attempts)
					} else {
						got, err = part.engine.ProcessNextDurable(ctx, part.queue)
					}
					finished := time.Now()
					require.NoError(t, err)
					require.True(t, got.Processed)
					require.Equal(t, part.ids[i], got.EventID)
					if scenario.fault == "" || p != 0 || i != 0 {
						require.Equal(t, int64(1), got.Attempts)
						require.False(t, got.Recovered)
					}
					result.Samples = append(result.Samples, profileSample{p, got.EventID, finished.Sub(serviceStart).Nanoseconds(), finished.Sub(part.submitted[i]).Nanoseconds(), got.Attempts, got.Recovered})
					offsets[p]++
					done++
				}
			}
			result.ElapsedNS = time.Since(start).Nanoseconds()
			goruntime.ReadMemStats(&after)
			result.AllocBytes = after.TotalAlloc - before.TotalAlloc
			result.AllocObjects = after.Mallocs - before.Mallocs
			// The start INFO increments the total after producing its response, so
			// that measurement command appears in the second observation.
			result.RedisCommands = commands() - commandStart - 1
			for p, part := range parts {
				stats, err := part.queue.Stats(ctx)
				require.NoError(t, err)
				require.Zero(t, stats.Pending)
				require.Equal(t, int64(0), client.XLen(ctx, part.options.DeadLetter).Val())
				// One output stream record per committed round, including all 100 dense
				// writes. Retries must not add another output record.
				outputs, err := client.XRange(ctx, part.options.OutputStream, "-", "+").Result()
				require.NoError(t, err)
				require.Len(t, outputs, warmupCounts[p]+result.Counts[p])
				for i, out := range outputs[warmupCounts[p]:] {
					require.Equal(t, part.ids[i], out.Values["input_id"])
					var payload struct {
						WriteIDs map[string]string `json:"write_ids"`
					}
					require.NoError(t, json.Unmarshal([]byte(out.Values["payload"].(string)), &payload))
					require.Len(t, payload.WriteIDs, result.Affected)
				}
				require.Equal(t, strconv.Itoa(result.Counts[p]-1), client.Get(ctx, part.options.Namespace+".seq").Val())
				for r := 0; r < result.Affected; r++ {
					require.Equal(t, "true", client.Get(ctx, fmt.Sprintf("%s.out.%d", part.options.Namespace, r)).Val())
				}
				require.NoError(t, part.queue.ReleaseOwnership(ctx))
			}
			results = append(results, result)
		})
	}
	if selected != "" {
		require.Len(t, results, 1, "unknown profile scenario")
	}
	require.False(t, t.Failed(), "do not publish partial measurement results")
	data, err := json.MarshalIndent(results, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(os.Getenv("REX_PROFILE_OUTPUT"), append(data, '\n'), 0644))
}
