# REX-M8.10 durable bottleneck attribution

REX-M8.10 adds opt-in attribution to the existing D13 harness and resolves the
remaining shared-Redis concurrency question. It changes no production path.
The decision is recorded in [D15](../../decisions/REX-M8-DURABLE-BOTTLENECK-ATTRIBUTION.md).

## Result

Production partition concurrency remains disabled. D15 proposes closing the
shared-Redis line when M8.10 is integrated. Across the five unchanged M8.9 D13 repetitions, sparse Redis CPU
utilization was tightly bounded at 0.596–0.603 for one worker, 0.796–0.804 for
two, and 0.929–0.934 for four. The medians were 0.601, 0.800, and 0.933. This is
consistent with the repeated two-worker p99 failure being dominated by queueing
at the one Redis execution thread rather than an unlocated client regression.
[d13-topology-input.json](d13-topology-input.json) retains every source value
and the revision, seed, environment, and source-summary digest used to compute
these ranges and medians.

The attribution run covered 96.2% and 97.1% of summed sparse sample duration at
one and two workers, and 82.6% and 81.8% for dense work. `Next` increased from 66
to 121 microseconds per event, while the four Lua stages together stayed near
120 microseconds of SLOWLOG time. SLOWLOG and client timers are intrusive:
compared with the matched control, sparse throughput was 1.6% lower at one
worker and 8.6% lower at two. Dense throughput was 9.3% and 6.3% lower, while
dense Redis CPU per event was 14.4% and 14.7% higher. The absolute stage results
are diagnostic only.

Dense work has a separate single-worker cost worth addressing. The single
5,000-event CPU-profile case, run without stage timing or SLOWLOG, allocated
about 452 KB and 4,812 objects per event, 21.5x and 13.6x the sparse fixture.
The dense one-worker commit stage averaged 338 microseconds, including 130
microseconds inside its SLOWLOG-instrumented Lua execution. The retained data
does not isolate allocation as the rest of that latency; its exact allocation
counters justify a falsifiable candidate, whose A/B gates must prove that the
allocation is on the critical path. D15 authorizes that separately gated work
without reopening the concurrency decision.

Dense Redis utilization remains lower at 0.52 for two workers and 0.69 for
four. Its concurrency case closes for a different reason: dense two-worker
speedup repeatedly missed the unchanged 1.50x D13 gate, and improving dense
serial allocation cannot make the overall gate pass while sparse remains
saturated.

The synthetic sparse and dense rules read only input facts supplied by each
event. Their steady-state path uses `Next`, `Begin`, `ApplyInput`, `Commit`, and
`Complete`; snapshot, acknowledgement, and dead-letter counters are retained as
explicit zeros. Programs that require prior facts will also pay snapshot cost,
which this diagnostic does not characterize.

## Reproduction

The stage and matched-control cases use 500 events because dense SLOWLOG output
records about 150 nested and top-level entries per event:

```sh
GOMAXPROCS=4 python3 scripts/durable-concurrency/run.py \
  --redis-server /path/to/redis-server \
  --output /tmp/rex-m810-stage --events 500 --runs 1 \
  --workers 1,2 --scenarios sparse-balanced,dense-balanced --stage-profile

GOMAXPROCS=4 python3 scripts/durable-concurrency/run.py \
  --redis-server /path/to/redis-server \
  --output /tmp/rex-m810-control --events 500 --runs 1 \
  --workers 1,2 --scenarios sparse-balanced,dense-balanced
```

The CPU run without stage timing or SLOWLOG uses 5,000 events:

```sh
GOMAXPROCS=4 python3 scripts/durable-concurrency/run.py \
  --redis-server /path/to/redis-server \
  --output /tmp/rex-m810-cpu --events 5000 --runs 1 \
  --workers 1,2 --scenarios sparse-balanced,dense-balanced --cpu-profile
```

[attribution.json](attribution.json) contains the reduced findings and decision.
The three metadata/summary pairs retain the stage, control, and CPU runs. The
four `cpu-*-top.txt` files retain the Go profiles' top reports. Raw case files,
Redis logs, compiled test binaries, and pprof binaries were validated locally
and are not retained.

These are loopback diagnostics on one host and one Redis process with
persistence disabled. They do not establish managed-service capacity, an
arrival-latency distribution, multi-Redis scaling, or production supervisor
behavior.
