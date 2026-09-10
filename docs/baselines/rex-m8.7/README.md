# REX-M8.7 durable profiling evidence

Base: `48cd367` (merged M8.6 PR #45), branch
`codex/rex-m8-durable-profiling`. Implementation and measurements are local;
review and hosted CI are pending. No production behavior, artifact version, or
dependency changes are included.

## Workload and method

The repeatable harness drives the production batch engine, Redis snapshot and
commit adapter, Redis Streams queue, durable journal, exact fact ownership, and
lease fencing. Four partitions each contain 100 rules. Sparse events affect one
rule; dense events affect 100 rules and commit 100 output facts. Balanced cases
send 250 of 1,000 events to each partition. The skew case sends 900 events to
partition 0 and 33–34 to each remaining partition. Equal-length synthetic key
prefixes prevent scenario labels from changing allocation measurements.

A single serial round-robin driver drains a finite backlog after 100 successful
warmup events per scenario. Warmup is excluded from every measurement but its
outputs remain covered by the correctness checks. Service latency
covers `ProcessNextDurable`; end-to-end latency starts before the producer's
`XADD`. Producer time is recorded separately and excluded from drain throughput.
Each scenario runs in a new test process with a fresh Redis server. This models
a warmed finite backlog under the current serial scheduler and exposes its skew
behavior, but it is not an open-loop arrival test, a daemon capacity test, or
evidence of partition isolation or parallel speedup.

The retry scenario fails once after the output commit and before terminal
completion. The owner-loss scenario expires a real lease, proves the stale
owner cannot complete, opens a successor, proves stale processing is fenced
while that successor owns the partition, and recovers the pending event. Every
scenario checks input/output order, exact output and write counts, final facts,
empty pending and dead-letter queues, and single-attempt delivery for ordinary
events. The two fault scenarios additionally verify two attempts and deduplication.

Environment: Go 1.26.6, macOS 26.6.2 arm64, Apple M4, `GOMAXPROCS=2`, Redis
7.4.2 over loopback TCP with persistence disabled, error-level logging, and
condition tracing disabled. Each of three runs used a new Redis process for each
scenario and 1,000 measured events per scenario. Raw summaries are in
[`summary.json`](summary.json); source hashes and setup are in
[`metadata.json`](metadata.json). The separate CPU run's exact source and
instrumentation settings are in [`profile-metadata.json`](profile-metadata.json).

## Results

Ranges below retain all three final runs because host activity caused visible
timing variation. Counts, allocations, and correctness outcomes remained stable.

| Scenario | Drain events/s | Service p50 ms | p95 ms | p99 ms | End-to-end p99 ms | Redis commands/event | Allocated KB/event |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Sparse balanced | 890–2,059 | 0.467–1.151 | 0.623–1.685 | 0.851–2.748 | 481–1,111 | 44 | 50.3 |
| Dense balanced | 510–1,142 | 0.792–1.437 | 1.261–4.559 | 1.964–11.259 | 868–1,908 | 143 | 457.3–458.1 |
| Sparse 90/10 skew | 266–1,872 | 0.482–1.969 | 0.907–13.320 | 1.366–29.128 | 530–3,749 | 44 | 50.3 |
| Completion retry | 353–2,058 | 0.467–1.729 | 0.616–7.542 | 0.847–21.076 | 483–2,814 | 44.017 | 50.3 |
| Owner loss | 412–1,624 | 0.472–1.469 | 0.652–6.724 | 0.983–14.345 | 611–2,414 | 44.055 | 51.3–51.4 |

The completion fault path took 0.89–3.17 ms. Injected lease expiry plus successor
takeover took 115.9–121.3 ms, including the deliberate 110 ms expiry
wait. In the skew case, the hot partition's end-to-end p99 was 530–3,750 ms while
the three cold partitions were 69–423 ms; with only 33–34 cold samples, their
p99 is the sample maximum. Allocation totals include the Redis
client and per-event measurement bookkeeping, so they characterize the harness's
end-to-end process cost rather than evaluator-only allocation.

Dense work increases the protocol count by 99 commands and allocation by about
410 KB per event because it writes and records 100 results. The CPU profile is
dominated by network/syscall activity; [`profile-top.txt`](profile-top.txt)
merges the five isolated scenario profiles. Those profiles include setup,
warmup, production, and drain within each test process; they do not profile the
separate Redis server. The profile and fixed command counts make the
Redis protocol path the first optimization target. They do not establish that
more workers will improve aggregate throughput: this milestone intentionally
has no concurrent driver.

## Decision and limits

The per-partition `service_events_per_second` field is count divided by summed
active service time. `events_per_second_over_full_drain` is only that partition's
share of the serial drain and must not be read as independent capacity.

The evidence supports a separately reviewed concurrent-partition experiment,
not immediate production worker enablement. That experiment must compare the
same fixtures at worker counts 1, 2, and 4; measure aggregate and per-partition
tails under 90/10 skew; keep exact ownership and per-partition order; and repeat
retry and owner-loss correctness checks. It should also break down Redis round
trips rather than treating Redis command count as round-trip count.

The owner-loss fault-path time is deliberately controlled by the harness: it
includes a 110 ms sleep plus successor setup, rather than the configured ten-
minute lease or the production default. The completion retry is immediate work
recovery by the same consumer. Neither number predicts production failover time,
and the one injected pause perturbs owner-loss scenario throughput. Successor
construction, ownership acquisition, and stale-owner fencing also explain the
owner-loss row's extra 0.055 commands and roughly 1.1 KB allocation per event.

The large timing spread includes a heavily disturbed host run and does not
establish a regression budget or capacity decision. Deterministic command and
allocation counts plus the correctness checks remained stable. A production
capacity claim requires a controlled host, production-equivalent Redis network,
persistence and TLS settings, sustained arrival rates, saturation/backpressure,
and longer steady-state samples. Raw event-level samples are intentionally not
checked in; the harness can reproduce them and the retained summary contains
each run and partition percentile.

## Reproduction and verification

```sh
python3 scripts/durable-profile/run.py \
  --redis-server /tmp/rex-m86-redis-build/redis-7.4.2/src/redis-server \
  --output /tmp/rex-m87-results \
  --events 1000 --runs 3

python3 scripts/durable-profile/run.py \
  --redis-server /tmp/rex-m86-redis-build/redis-7.4.2/src/redis-server \
  --output /tmp/rex-m87-race \
  --events 100 --runs 1 --race

python3 scripts/durable-profile/run.py \
  --redis-server /tmp/rex-m86-redis-build/redis-7.4.2/src/redis-server \
  --output /tmp/rex-m87-profile \
  --events 1000 --runs 1 --cpu-profile

go tool pprof -top -nodecount=50 \
  /tmp/rex-m87-profile/runtime.test \
  /tmp/rex-m87-profile/cpu-run-0-*.pprof \
  > docs/baselines/rex-m8.7/profile-top.txt

go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
git diff --check
```

The first command produced the retained results. The race-mode harness, full
normal and race suites, vet, build, Python syntax check, source hash check, and
`git diff --check` pass on the final local sources.

## Claude consultation

Claude was asked to review workload boundaries, measurement validity, and the
retry/owner-loss acceptance checks. Its findings led to final-source reruns,
fresh Redis and test-process isolation per scenario, equal-length keys, explicit
warmup, corrected INFO command accounting, active-service versus serial-drain
partition rates, stronger stale-owner processing checks, ordinary-attempt
assertions, and precise fault-path limitations. The misleading retained-heap
claim was removed. The harness continues to run from a dirty milestone branch,
but now refuses modified tracked Go/module sources or unexpected untracked Go
files and hashes both allowed measurement sources. Documentation changes may be
dirty without changing the measured binary. Claude's follow-up found no
remaining correctness or evidence-integrity defect; its three minor provenance
and explanation notes are also reflected in the final report.

The regenerated metadata records original PR commit `fa19721` and the working
tree containing these review fixes. The follow-up commit is necessarily newer;
a clean reproduction from it will therefore record that newer HEAD and a clean
status. Production Go/module sources remained at `fa19721`, while the exact
profile test and runner are anchored by their recorded content hashes.
