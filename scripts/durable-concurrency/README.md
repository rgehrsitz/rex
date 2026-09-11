# Durable partition concurrency experiment

This opt-in M8.8 harness compares one, two, and four independently owned
partitions against one disposable Redis process. It creates a distinct Redis
store, batch engine, durable queue, stream set, fact claim, and serial processing
goroutine for each partition. It does not alter `rexd` or enable a production
worker pool.

```sh
python3 scripts/durable-concurrency/run.py \
  --redis-server /path/to/redis-server \
  --output /tmp/rex-m88-results \
  --events 1000 --runs 5
```

Pass `--redis-cli` when it is not beside `redis-server`. `--workers` accepts a
unique comma-separated subset of `1,2,4`; `--scenarios` accepts a subset of
`sparse-balanced,dense-balanced,sparse-skew,completion-retry,owner-loss`.
Event counts must be multiples of 100.

Each case owns a fresh Redis process, warms 100 events per partition, produces a finite
backlog, and then starts one serial goroutine per partition at a shared gate.
The runner shuffles case order from a saved deterministic seed to reduce
systematic ordering bias. It records aggregate and per-partition throughput,
service latency, finite-backlog completion time, speedup, parallel efficiency,
Redis CPU time, command counts, overlapping processing windows, process
allocations, host load, and source/environment provenance. Speedup and command
gates exclude fault scenarios; race and CPU-profile runs are marked
non-comparable.

Every case verifies per-partition input/output order, exact output and write
counts, final facts, empty pending and dead-letter queues, and ownership release.
Fault cases additionally prove retry deduplication or real lease expiry,
stale-owner fencing, and successor recovery while sibling partitions drain.
Use `--race` for correctness only; race timings are not comparable. Use
`--cpu-profile` with a narrow worker/scenario selection to retain Go profiles.

The runner computes the D13 gate. One-worker throughput must first have a
coefficient of variation at or below 20%; otherwise the decision is
inconclusive. A stable run passes only when two-worker median paired throughput
is at least 1.5x for both balanced workloads, service p99 stays below the
one-worker maximum, the skewed hot partition does not regress beyond that
maximum, and non-fault commands per event remain exact. The 90/10 workload has a
theoretical two-worker throughput ceiling near 1.11x because its hot partition
remains serial.

This is a finite-backlog processor-loop experiment on one host and one Redis server. It does
not cover daemon supervision, routing, reload, rollback, dynamic rebalancing,
temporal artifacts, open-loop arrivals, multiple Redis servers, or production
capacity. It omits production lease-renewal traffic. A stable result below the
documented speedup gate keeps production concurrency disabled; an unstable
control is recorded as inconclusive.
