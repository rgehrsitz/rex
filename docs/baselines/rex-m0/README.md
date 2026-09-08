# REX-M0 baseline report

Recorded 2026-09-07 (America/New_York; raw timestamps use UTC on 2026-09-08).
Status: local baseline complete. Next implementation chunk: **REX-M1**.

## Starting point and evidence

The starting commit is `dddcdbac40af6a0265fb7c38c748e3ac196f2385`, plus the
existing working-tree priority/default/bytecode-v3 changes. This is a verified
source snapshot, not a claim that those changes have been merged or released.
The baseline added measurement tooling and corrected one indentation in the
pending `pkg/runtime/bytecode.go` changes. It did not optimize production logic
or change its behavior.

The exact pending compiler/runtime changes are retained in [source.patch](source.patch).
The [initial source manifest](source-manifest.sha256) and
[integration source manifest](source-manifest-integration.sha256) identify the
measured files. All Go source and fixture hashes agree between runs. The
fingerprints differ only because the Python orchestration/summary tooling was
updated; generated rules and bytecode also have per-sample digests.

| Evidence | Location |
| --- | --- |
| All measured results | [Summary table](summary.md), [ranges and investigation budgets](summary.json) |
| Memory processing, five runs per fixture | [Raw samples](memory.jsonl), [environment](environment-memory.json) |
| Real Redis processing, three runs per fixture | [Raw samples](redis.jsonl), [environment](environment-integration.json) |
| Logging overhead, three runs | [Raw samples](logging-info.jsonl) |
| State growth, three runs per cardinality | [1 key](churn-1.jsonl), [1,000 keys](churn-1000.jsonl), [10,000 keys](churn-10000.jsonl) |
| Commands, exit codes, elapsed times | [Initial checks/memory invocation](commands-initial.json), [accepted integration invocation](commands-integration.json) |
| Reusable harness and methods | [Instructions](../../../scripts/baseline/README.md), [fixture matrix](../../../scripts/baseline/fixtures.json) |

There are 108 accepted runs across 28 groups. Memory runs use 1,000 measured
batches each; Redis runs use 100; logging runs use 1,000; churn runs use 10,000
individual updates. Each run starts with a fresh engine and 100 warmup batches.
Short Redis runs keep the dense fixture practical; their p99 is exploratory.

An initial Redis measurement attempt was intentionally stopped after discovering
that server logs and benchmark output shared a filename in the new runner.
**None of those Redis samples is included.** Its command record retains the
failed attempt for transparency. Previously completed checks and memory samples
remain valid. The corrected runner separated the log files and completed the
accepted Redis/logging/churn invocation. [runner-initial.py](runner-initial.py)
preserves the original orchestration source identified by the initial manifest;
use the maintained runner in `scripts/baseline` for new runs.

## Environment and measurement scope

- MacBook Air `Mac16,13`, Apple M4, 10 CPU cores (4 performance, 6 efficiency),
  24 GB RAM; macOS 26.6.2, Darwin 25.6.0, `arm64`.
- Go 1.26.6; benchmark processes set `GOMAXPROCS=2`. Local Go build cache and
  rebuilt `govulncheck` v1.1.4 live in temporary directories.
- Redis 7.4.2, compiled locally with libc allocation and TLS disabled. One
  disposable process on TCP IPv4 loopback, persistence disabled, no subscribers,
  no other clients changing data. No production service was used.
- Scripts disabled; equal priority 10; affected rules placed last in source
  order; deterministic scalar facts and unique action targets. No feedback rules.
- Default logging disabled. Logging comparison enables structured `info`
  serialization to `io.Discard`; the standard-library logger also writes to
  `io.Discard`. This excludes terminal, filesystem, and log-collector costs.
- Builds and checks run before measurements; measurement groups execute serially.
  This is a developer laptop baseline, without CPU pinning or thermal isolation.

Memory measurements cover dependency selection, interpretation, and simulated
actions without network I/O. Redis measurements use the production store and
include its actual reads, writes, and publications. Both time the synchronous
runtime API; multi-fact batches are repeated calls in sorted order, matching
current daemon dispatch semantics. Producer writes, ingress, JSON decoding,
queueing, subscribers, and derived-event consumption are excluded. These
numbers are **not daemon end-to-end throughput or production SLOs**.

The fixture matrix varies 100/1,000/10,000 total rules, 10/100/1,000 affected
rules, 4/32 dependencies, shared/unique dependencies, half-missing dependencies,
1/8 input facts per batch, 0/50/100% matching, and 1/8 actions per rule.
The harness checks expected action counts; the summarizer additionally verifies
Redis's observed command counters against instrumented store calls.

## Findings that should guide M1 and M4

Values below are medians of each run's p50; see the full table for p95,
throughput, allocations, and ranges.

| Observation | Measured baseline | Implication |
| --- | --- | --- |
| Unrelated rules increase CPU work | Memory p50: 10.83 µs at 100 rules, 38.92 µs at 1,000, 332.75 µs at 10,000; always 10 affected rules | Direct dependency and execution indexes are a high-value M1 change. |
| Missing-data filtering is especially expensive | 403.21 µs for 100 affected rules with 400 unique dependencies; 7,917.88 µs when half the rules have a missing dependency, despite executing fewer actions | Replace nested removal scans; preserve today's missing-data semantics until M4. |
| Diagnostic reads materially increase command volume | The 10-action fixture issues 1 MGET + 10 SET + 10 PUBLISH + 10 GET per batch, confirmed by Redis itself | M1 should remove 10 GETs while preserving 10 actions. This is a command-count target, not a predicted latency speedup. |
| Redis dominates action-heavy latency here | Sparse 1,000-rule fixture: 38.92 µs in memory versus 2,327.96 µs with Redis | Distinguish CPU-index gains from network/store gains when evaluating M1. |
| Multi-fact batches repeat work | Eight inputs produce 8 MGETs and 80 actions for 10 affected rules; memory p50 365.54 µs | M4's deduplicated snapshot evaluation is a behavior change with measurable potential. |
| Detailed traces have measurable CPU cost | Shared-dependency fixture: 377.62 µs disabled versus 475.62 µs at `info` to discard, about 26% higher | Make tracing selectable; disk/console cost would be additional and was not measured. |
| Fact retention follows unbounded input cardinality | After 10,000 updates: 16 retained facts for one cycling key, 1,015 for 1,000 keys, 10,015 for 10,000 keys | M4 needs explicit state bounds. |

The 10,000-key churn case retains roughly 1.02 MB additional Go heap after GC;
the fixed-key control reclaims about 33 KB. Heap deltas include runtime noise,
so fact counts provide the clearer growth evidence. Churn timing is dominated
by tiny operations and harness overhead; it is not a capacity estimate.

## Provisional regression budgets

Every group in [summary.json](summary.json) records its measured median, minimum,
maximum, and an investigation limit for p50/p95 latency, allocations, and bytes
per batch. The limit is **maximum + (maximum - minimum)** across the baseline
runs. Compare it with the median of the same number of candidate runs on the
same machine and fixture. These limits reflect observed variation; they are
heuristics, not statistical confidence intervals or portable CI gates.

Examples for memory mode with logging disabled:

| Fixture | p50 baseline | Investigate above | Allocations/batch baseline |
| --- | ---: | ---: | ---: |
| sparse-100 | 10.83 µs | 26.00 µs | 107 |
| sparse-1000 | 38.92 µs | 39.13 µs | 107 |
| sparse-10000 | 332.75 µs | 335.75 µs | 107 |
| missing-half | 7,917.88 µs | 8,145.12 µs | 535 |

The first sparse fixture had a wider observed range than later fixtures. Use
paired baseline/candidate reruns when evaluating M1; do not fail CI over a
single small threshold crossing. Retain exact expected action counts. Changes
to bytecode meaning or batch semantics require a separate M4 baseline, rather
than presenting less semantic work as a behavior-preserving optimization.

## Correctness, build, and security checks

| Check | Result / evidence |
| --- | --- |
| `gofmt -l .` | Clean after the one indentation correction; [log](format.log). |
| `go mod tidy -diff` | Passed, no module changes; [log](tidy.log). |
| `go vet ./...` | Passed; [log](vet.log). |
| `go test -count=1 ./...` | Passed, uncached; [log](test.log). Includes fixture action/dependency counts and determinism checks. |
| `go test -race -count=1 ./...` | Passed, uncached; [log](race.log). |
| `go build ./...` | Passed; [log](build.log). |
| Six release archives and SHA-256 verification | Passed for darwin/linux/windows on amd64/arm64; [checksums](archive-checksums.txt), [verification](archive-verification.txt). Archives were built in a temporary directory. |
| `govulncheck -show verbose ./...` | No reachable vulnerabilities on darwin/arm64; [log](vulnerability.log). |
| `GOOS=linux GOARCH=amd64 govulncheck -show verbose ./...` | No reachable vulnerabilities for the CI target; [log](vulnerability-linux.log). |
| Hosted CodeQL | Latest recorded run passed for base commit `dddcdbac40af`; [record](codeql-base.json). This does not cover uncommitted changes. Hosted CI/CodeQL must run when this work is integrated. |

The scan reports `GO-2026-5970` in imported `golang.org/x/text` and
`GO-2026-5024` in required `golang.org/x/sys`, but reports that REX does not call
the vulnerable symbols for the scanned targets. Record these for the next
dependency review; M0 did not change dependencies. The temporary scanner fixes
the local toolchain mismatch for this run; the user's previously installed
scanner was not replaced. No Windows-target vulnerability scan is claimed.

## Open audit work

| Finding | Still open / next milestone |
| --- | --- |
| REX-006 | Script cancellation/isolation; M6, earlier for deployments enabling scripts. |
| REX-009 | Post-action verification GET and publication logging; M1. |
| REX-010 | Constructor fatal exit/startup cancellation; M3. |
| REX-011 | Redis TLS and environment credentials; M3. |
| REX-012 | Routing and unbounded mutable facts; M3/M4. |
| REX-013 | Permanent tool/CI housekeeping remains partial; M0 records a correctly rebuilt temporary scanner and current hosted CodeQL evidence. |

Batch state visibility, action-failure/commit behavior, and durable delivery
remain M4/M7 contract work. A passing baseline does not resolve these defects
or change current reliability guarantees.

## Resume instructions

1. Start with REX-M1 in the [foundation roadmap](../../FOUNDATION_ROADMAP.md).
2. Verify the source snapshot and preserve unrelated changes. To reconstruct the
   original runtime, start at the base SHA, apply `source.patch`, and use the
   M0 Go harness/fixtures identified by the manifests. Resolve the eventual
   integration revision from the commit introducing this report when available.
3. Reproduce with the [maintained runner](../../../scripts/baseline/README.md).
   For matching sample counts, run `--measurements memory --runs 5
   --memory-events 1000`, then separately `--measurements redis logging churn
   --runs 3 --redis-events 100 --memory-events 1000`. The second invocation may
   use `--skip-checks` only when the first provides passing checks for the same
   Go sources. Keep both output directories and summarize their combined JSONL
   samples in a separate directory.
4. Optimize direct indexes/filtering and diagnostic reads in separate changes;
   compare expected actions, command counts, allocations, and repeated latency
   results. Update the roadmap and audit with the resulting integrated revisions.

M0 is complete as a local measurement/checkpoint milestone. Integration and
hosted CI remain the normal PR gate; this report does not certify a release.
