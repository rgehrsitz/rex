# D15 — Stop shared-Redis concurrency; optimize dense allocation separately

Accepted for REX-M8.10 in PR #51, 2026-09-14.

The production-concurrency line stops at one serial processor per REX daemon
when partitions share one Redis process. The unchanged five-run D13 diagnostic
puts sparse Redis CPU utilization at a 0.601 median with one worker, 0.800 with
two, and 0.933 with four. Two workers already fail the service-p99 gate, and
four workers approach the single Redis execution thread's capacity. Together
with flat per-script execution time and rising client-stage latency, this is
consistent with queueing at that shared execution thread. Faster client work
would feed the shared server sooner; it would not remove that queueing.

Dense Redis utilization retains headroom at a 0.520 median with two workers and
0.689 with four. Dense closes on the repeated D13 speedup failure rather than
saturation: its two-worker median remained below 1.50x, and dense-only work
cannot satisfy the overall gate while sparse fails structurally. Dense serial
efficiency remains an independent target below.

This decision does not reject partitioning or parallelism on a different
topology. A future proposal may measure one Redis execution domain per
partition, Redis Cluster with an explicit key-slot design, or another adapter.
It must define routing, supervision, reload, migration, and temporal ownership
before changing the daemon. The existing single-processor production contract
remains unchanged.

The M8.10 diagnostic harness times `Next`, `Begin`, input application, snapshot
access, commit, completion, acknowledgement, and dead-letter operations only
when explicitly enabled. It also uses Redis SLOWLOG to retain the top-level Lua
durations and nested command entries. SLOWLOG includes nested command time in
the parent `EVALSHA` duration, so those entries overlap. Instrumented results
are never eligible for D13 gates. A matched 500-event uninstrumented control
quantifies the observer overhead. A separate 5,000-event run without stage
timing or SLOWLOG provides Go CPU and exact allocation counters; CPU profiling
still makes that run diagnostic rather than D13-comparable.

The diagnostic identifies one independent optimization worth pursuing: dense
commit construction allocates about 453 KB and 4,812 objects per event, versus
21 KB and 353 objects for sparse work. That work may be reduced without merging,
reordering, or removing any durable stage and without changing persisted bytes.
It is a single-worker efficiency improvement, not a route back to production
concurrency.

Before implementation, a dense-allocation candidate must predeclare a paired
five-run A/B procedure using the same 5,000-event one-worker sparse and dense
fixtures. Every comparison uses the median of the five runs. The control must
have throughput CV at or below 20%, and its p95 maximum divided by its p95
minimum must not exceed 1.20. An unstable control makes the result inconclusive.
The candidate must meet all of these gates:

- dense allocation at or below 150,000 bytes and 1,500 objects per event;
- dense median throughput at least 1.10x control and median p50 and p95 at most
  0.90x control;
- sparse throughput at least 0.98x control and sparse allocation no more than
  1.02x control;
- Redis CPU per event within five percent in both fixtures; and
- byte-identical durable formats plus the existing ordering, fencing,
  recovery, lost-reply, deduplication, and race checks.

If allocation gates pass but throughput or latency gates fail, allocation is
not on the critical path and the candidate is reverted. D13 is not an
acceptance gate for this work and is not rerun. Dedicated connections and
durable-stage fusion remain out of scope.
