# REX-M8.8 concurrent partition experiment

Base: merged M8.7 PR #46, `5bf6852`. This milestone adds an opt-in experiment
and evidence only. It does not change `rexd`, production configuration, artifact
formats, dependencies, or supported single-partition behavior.

## Decision

Do not build or enable a production multi-partition supervisor from this
evidence. Two workers exceeded the predeclared throughput target for both
balanced workloads, but sparse and dense service p99 exceeded their one-worker
maximums. D13 requires throughput, latency, correctness, and command-count gates
to pass together.

The result is a measured no-go rather than a claim that concurrency can never
help. The CPU profile and Redis CPU ratios show a network/syscall-bound path with
remaining host and Redis headroom, while the latency tails and four-worker
efficiency show contention. A later milestone may reduce round-trip and
transaction costs, improve the experimental environment, and repeat D13 before
designing daemon supervision.

## Workload and method

Each case owns a fresh Redis 7.4.2 process with persistence disabled. One, two,
or four partitions share that process. Every partition has a distinct Redis
store, engine, durable queue, namespace, input/output/dead-letter streams,
immutable exact fact claim, lease, and serial processing goroutine. A common
gate starts the goroutines after a finite backlog is produced. Each independent
engine processes 100 warmup events before 1,000 measured events; warmup is
excluded from measurements. Warmup outputs are counted, and pending/dead-letter
checks cover both warmup and measured events.

Sparse cases affect one of 100 rules per partition. Dense cases affect all 100.
Balanced events are distributed evenly. The 90/10 case sends 90% to partition
zero and distributes the remainder across its siblings. Its two-worker speedup
cannot exceed about 1.11x without making the hot partition itself concurrent.
Completion-retry fails once after commit and before terminal completion.
Owner-loss expires partition zero's real lease after its first commit, proves
the stale owner cannot complete or reprocess the event, and recovers through a
successor while at least one sibling completes work.

Five repetitions cover all fifteen scenario/worker combinations, for 75 fresh
Redis/test-process cases. Case order is shuffled with saved seed 8808. The
one-worker controls are stable under D13's 20% coefficient-of-variation limit:
0.4% for sparse balanced and 0.5% for dense balanced. Host one-minute load was
2.1–6.3. The Apple M4 host exposes four performance and six efficiency cores;
four-worker results are diagnostic because four Go workers and Redis compete for
those four performance cores. `GOMAXPROCS=4`.

## Results

Values are ranges across five runs, with median throughput and median paired
speedup shown. Backlog completion begins before `XADD` and includes position in
the finite queue; it is diagnostic, not arrival latency.

| Scenario | Workers | Median events/s | Median speedup | Service p99 ms | Backlog p99 ms | Redis commands/event |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Sparse balanced | 1 | 2,092 | 1.00x | 0.595–0.605 | 470–477 | 44 |
| Sparse balanced | 2 | 3,476 | 1.65x | 0.749–25.411 | 284–1,733 | 44 |
| Sparse balanced | 4 | 4,825 | 2.32x | 1.006–2.385 | 199–444 | 44 |
| Dense balanced | 1 | 1,246 | 1.00x | 0.957–1.001 | 787–799 | 143 |
| Dense balanced | 2 | 1,949 | 1.56x | 1.366–2.893 | 504–890 | 143 |
| Dense balanced | 4 | 2,609 | 2.10x | 1.870–1.933 | 379–384 | 143 |
| Sparse 90/10 | 1 | 2,099 | 1.00x | 0.595–15.819 | 467–2,154 | 44 |
| Sparse 90/10 | 2 | 2,280 | 1.09x | 0.669–0.708 | 431–436 | 44 |
| Sparse 90/10 | 4 | 2,266 | 1.08x | 0.917–16.044 | 435–2,223 | 44 |

The automated [gate report](gates.json) records:

| Gate | Result |
| --- | --- |
| Stable sparse and dense one-worker controls | Pass |
| Two-worker sparse median paired speedup >= 1.5x | Pass: 1.65x |
| Two-worker dense median paired speedup >= 1.5x | Pass: 1.56x |
| Two-worker balanced service p99 <= one-worker maximum | Fail: sparse 25.411 vs 0.605 ms; dense 2.893 vs 1.001 ms |
| Two-worker skew hot-partition p99 <= one-worker maximum | Pass: 0.691 vs 15.819 ms; one skew control was disturbed |
| Non-fault Redis commands/event unchanged | Pass: 44 sparse, 143 dense |
| Overall D13 gate | **Fail** |

Redis CPU seconds divided by drain wall time rose from median 0.38 to 0.53 to
0.67 for sparse balanced and 0.28 to 0.39 to 0.52 for dense balanced at one,
two, and four workers. Measured allocations remained near 65.8 KB/event for
sparse balanced; dense allocation increased from a median 474.6 KB/event with
one worker to 486.1 with two and 490.0 with four. These process totals include
the Redis client and measurement bookkeeping.

Fault-path timing was 0.76–1.12 ms for one-worker completion retry and
114.9–117.0 ms for one-worker owner loss. With four workers it was 1.09–2.22 ms
and 113.9–128.5 ms respectively. Fault speedup is deliberately omitted because
fixed recovery work would bias it. Commands remained exact at 44.017/event for
completion retry and 44.055/event for owner loss. Every multi-worker case
recorded overlapping processing windows; every multi-worker owner-loss case
recorded sibling completions during recovery.

## Correctness and profile evidence

All 75 normal cases proved per-partition input/output order, exact output and
write counts, final facts, empty pending/dead-letter queues, one attempt for
ordinary events, two attempts without duplicate effects for recovered events,
ownership removal, stale-owner fencing, and successor recovery. A separate
100-event race build passed all fifteen scenario/worker combinations. Its
[summary](race-summary.json) and [environment](race-metadata.json) are retained;
race timing is explicitly non-comparable.

The four-worker sparse CPU run is described by
[`profile-metadata.json`](profile-metadata.json). Its [top report](profile-top.txt)
attributes 42.9% of samples to raw syscalls and 24.5% to `kevent`; Redis `Watch`
accounts for 38.8% cumulative time. Instrumented timing is non-comparable.

The retained [summary](summary.json) contains every run, partition window,
percentile, allocation, Redis CPU ratio, command count, and host load sample.
[`metadata.json`](metadata.json) records the original run revision and dirty
status. A clean checkout records a newer revision and clean status; the SHA-256
entries anchor the exact Go test and Python runner that produced the evidence.

## Scope limits

This measures processor-loop scaling on one macOS host and one loopback Redis
server. It omits daemon lease-renewal traffic and does not test or authorize a
production supervisor, readiness aggregation, routing, coordinated reload or
rollback, temporal artifacts, ownership migration, dynamic workers, open-loop
arrival capacity, Redis Cluster, or multiple Redis servers. Distinct artifacts
and exact disjoint ownership avoid the shared-artifact timer-key problem; they
do not solve it.

## Reproduction

```sh
python3 scripts/durable-concurrency/run.py \
  --redis-server /tmp/rex-m86-redis-build/redis-7.4.2/src/redis-server \
  --output /tmp/rex-m88-results --events 1000 --runs 5

python3 scripts/durable-concurrency/run.py \
  --redis-server /tmp/rex-m86-redis-build/redis-7.4.2/src/redis-server \
  --output /tmp/rex-m88-race --events 100 --runs 1 --race

python3 scripts/durable-concurrency/run.py \
  --redis-server /tmp/rex-m86-redis-build/redis-7.4.2/src/redis-server \
  --output /tmp/rex-m88-profile --events 1000 --runs 1 \
  --workers 4 --scenarios sparse-balanced --cpu-profile

go tool pprof -top -nodecount=50 \
  /tmp/rex-m88-profile/runtime.test \
  /tmp/rex-m88-profile/cpu-run-0-sparse-balanced-w4.pprof
```

Claude was consulted before implementation and again on the working harness.
Its recommendations established the experiment-only boundary and led to
stability gating, overlap and sibling-progress evidence, fixed per-partition
warmup, stronger recovery and lease-removal assertions, non-fault-only gates,
host CPU/load provenance, backlog-latency naming, and precise scope limits.
