# Reproducible REX baseline

This harness supports [REX-M0](../../docs/FOUNDATION_ROADMAP.md) and comparisons
for later milestones. The recorded starting measurements are in
[the M0 report](../../docs/baselines/rex-m0/README.md).

## Run

Run from the repository root. Requirements: the Go toolchain in `go.mod`, Python
3, Git, the release script's archive tools, a Redis server binary, and a
`govulncheck` binary built with the same Go toolchain. No Python packages are
required. Results must go into a new directory.

For the original Redis/tool versions, an isolated setup is:

```sh
REX_M0_WORK=$(mktemp -d)
export GOCACHE="$REX_M0_WORK/go-cache"
GOBIN="$REX_M0_WORK" go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
curl -fL https://download.redis.io/releases/redis-7.4.2.tar.gz \
  -o "$REX_M0_WORK/redis.tar.gz"
```

Verify the archive's SHA-256 is
`4ddebbf09061cbb589011786febdb34f29767dd7f89dbe712d2b68e808af6a1f`
before extracting. On macOS use `shasum -a 256`; on Linux use `sha256sum`.
These versions reproduce the baseline and are not a recommendation to deploy
an old Redis release or to freeze future security tooling.

```sh
tar -xzf "$REX_M0_WORK/redis.tar.gz" -C "$REX_M0_WORK"
make -C "$REX_M0_WORK/redis-7.4.2" -j4 BUILD_TLS=no
python3 scripts/baseline/run.py \
  --output "$REX_M0_WORK/results" \
  --redis-server "$REX_M0_WORK/redis-7.4.2/src/redis-server" \
  --govulncheck "$REX_M0_WORK/govulncheck"
python3 scripts/baseline/summarize.py "$REX_M0_WORK/results"
```

The runner executes formatting, module metadata, vet, uncached normal/race
tests, builds, vulnerability scanning, and all six release archive builds and
checksum verification. It then runs measurements sequentially with
`GOMAXPROCS=2`, against its own loopback Redis process with persistence disabled.
It verifies server ownership before clearing fixture keys, terminates Redis on
exit, and retains logs and source fingerprints. It does not run hosted CodeQL;
record the applicable hosted result separately and state which revision it covers.

Defaults are five runs, 1,000 memory batches/run, 200 Redis batches/run, and 100
warmup batches/run. Dense Redis fixtures can take several minutes. Use
`--runs`, `--memory-events`, and `--redis-events` to choose a sampling budget;
event counts must be multiples of 100 to preserve exact match-rate expectations.
The M0 report records the counts actually used for each measurement group.

`--measurements memory redis logging churn` selects groups.
`--skip-checks` supports measurement-only follow-ups when separate passing check
evidence for the same Go sources is retained. It is never evidence that those
checks passed. To compare M1 with M0, retain the fixture matrix and measured
Go harness, use the same group counts and environment, and rerun baseline and
candidate close together to control machine drift.

## What is measured

`tools/rex_baseline` generates deterministic rules, parses JSON, writes bytecode,
and loads the validated artifact. That setup, fact seeding, and warmup are
excluded from timed execution. Every run gets a new engine and store state.
The harness rejects a result if the number of actions differs from the fixture's
expected work. Scripts are disabled and priorities are equal.

The fixture matrix puts affected rules at the end of each ruleset to expose
linear execution-index lookup. It varies total/affected rules, shared versus
unique dependencies, missing dependencies, batch size, matching rate, and actions.
Batch inputs are delivered in sorted order using repeated
`ProcessFactUpdateContext` calls, matching the current daemon's dispatch model.

- **Memory:** CPU/allocation cost of the synchronous runtime processing path,
  with a minimal counted in-memory store. This includes dependency selection,
  bytecode interpretation, and simulated writes; it is not an opcode-only test
  or a promised M2 store API.
- **Redis:** the same runtime API backed by the production Redis store. Counts
  from Redis `INFO commandstats` must agree with the instrumented store calls.
  This includes dependency reads and action writes/publications/verification
  reads. There are no subscribers and outputs do not recursively trigger rules.
- **Logging:** compares disabled versus `info` serialization to `io.Discard` on
  the shared-dependency fixture. It measures logging CPU/allocation overhead,
  not terminal formatting, disk writes, or a log collector.
- **Churn:** 10,000 unrelated fact updates cycling over 1, 1,000, or 10,000 keys.
  It records retained fact count and heap change after GC with the engine alive.

Latencies are measured per API batch; throughput is completed batches divided
by elapsed time, without external load or concurrency. The harness excludes
producer writes, Pub/Sub ingress, JSON decoding, scheduling/queue delay, and
derived-event consumption. It is not a daemon end-to-end capacity test, an
open-loop saturation test, or a production latency SLO.

Allocations and bytes use process-wide Go memory counters around the measured
loop. Retained heap uses explicit GC before/after measurement, with latency
samples allocated before the first GC. Small negative deltas are possible from
runtime/GC noise; use fixed-cardinality repeats and retained fact counts to
interpret growth. Compilation memory and retained memory in Redis are excluded.

## Review thresholds

`summary.json` contains per-group medians, observed ranges, and provisional
investigation limits for p50/p95 latency, allocations, and bytes per batch.
Each limit is the maximum baseline run plus its observed range. Compare it
with a candidate median across the same number of runs, on the same workload
and machine. An exceedance calls for investigation and paired reruns; it is not
an automatic portable CI failure. Do not turn a small-sample p99 into a target.

Preserve semantic work and action counts when optimizing. M1 should remove
diagnostic reads and full-index scans; M4 intentionally changes batch behavior
and therefore needs both migration scenarios and a newly versioned baseline.

For behavior-preserving comparisons, collect candidate JSONL files in a separate
directory and run:

```sh
python3 scripts/baseline/compare.py docs/baselines/rex-m0 /path/to/candidate-results
```

The comparator checks fixture/artifact identity, sampling and environment fields,
semantic work, retained fact counts, and actual Redis command counts. It writes
`comparison.md` and `comparison.json` with latency/allocation review flags and
loaded-heap observations. Additional baseline groups may be omitted from a
candidate comparison, but every included group must have matching repetitions.
See [the M1 report](../../docs/baselines/rex-m1/README.md) for the first result.
