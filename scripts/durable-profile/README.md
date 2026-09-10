# REX durable profiling

This opt-in harness measures the existing single-process durable Redis path for
REX-M8.7. It starts and owns a fresh loopback Redis process for every scenario. It
does not enable concurrent workers or measure daemon ingress.

Build Redis 7.4.2 as described in `scripts/baseline/README.md`, then run:

```sh
python3 scripts/durable-profile/run.py \
  --redis-server /path/to/redis-7.4.2/src/redis-server \
  --output /new/output/directory \
  --events 1000 \
  --runs 3
```

The output directory must not already exist. Event counts must be multiples of
100. Each run exercises four exact-ownership partitions and five isolated scenarios:
sparse balanced, dense balanced, sparse 90/10 skew, a failure after commit but
before completion, and real lease expiry followed by successor recovery.

Each scenario first processes 100 successful warmup events, which are excluded
from every measurement but included in correctness checks. The harness then
records service and finite-backlog end-to-end p50/p95/p99 latency, aggregate
drain throughput, per-partition active-service rate and serial-drain share,
Redis command counts, process allocations during the measured drain, and fault-
path time. It also verifies event order, exact output
counts, write identities, final facts, empty pending/dead-letter queues, two
attempts for recovered work, and stale processing after successor takeover.

Use `--race` as a correctness run only. Use `--cpu-profile` to retain a Go CPU
profile; its instrumented timings are not comparable to normal runs. The runner
records tool versions, source hashes, git status, topology, and Redis INFO data.
It disables Redis persistence and detailed condition tracing. Results are local
loopback measurements, not production capacity or a claim of parallel speedup.
