# REX-M1 implementation and performance report

Verified 2026-09-07, America/New_York. Status: complete in the working tree;
integration and hosted CI remain the normal PR gate. Next milestone: **REX-M2**.

## Delivered behavior

- Load rule execution locations/priorities and dependencies into direct maps.
  Event processing no longer scans every rule's metadata for each candidate.
- Filter candidates once using a missing-fact set and each rule's dependencies.
  The persistent candidate slice is never modified. Priority, source-order ties,
  missing-data behavior, and action order are preserved.
- Preserve combined dependencies when an accepted artifact contains repeated
  dependency records; the new map does not silently keep only the last record.
- Remove the post-action diagnostic `GET`. Writes and publications still execute
  through the same store operation and retain their existing failure semantics.
- Send successful publication diagnostics through structured `debug` logging,
  without including the event payload or bypassing configured logging.
- Add `logging.trace_conditions` (default `true`) and
  `Engine.SetConditionTracing(bool)`. Setting it to `false` omits per-condition
  records while retaining candidate/action/rule summaries, warnings, and failures.
  Configure the engine before use; concurrent configuration is not introduced.

M1 changes neither compiler output nor bytecode meaning. The preceding pending
v3/priority work remains the baseline, not a change attributed to this milestone.
The [M1-only patch](m1-only.patch) separates this work from that earlier delta;
the [full source patch](source.patch) and [manifest](source-manifest.sha256)
identify the checked working-tree snapshot based on `dddcdbac40af`.

## Results

The unchanged M0 Go harness and fixture matrix produced 99 candidate runs across
25 groups: five memory runs per fixture, three Redis runs per fixture, and three
`info` logging runs. Sampling counts, Go/OS/CPU settings, compiled artifact
digests, retained fact counts, and action/dependency work match M0. Redis's own
command counters agree with the instrumented store calls.

The [full comparison](comparison.md) and [machine-readable ranges/budgets](comparison.json)
show **zero provisional investigation flags** for p50/p95 latency, allocations,
or bytes per batch. The separate [same-session comparison](paired/after/comparison.md)
adds 58 before/after runs across seven representative groups, also with zero
flags. The before snapshot's Go and fixture hashes were checked against M0.

| Representative result | M0 p50 | M1 p50 | Scope |
| --- | ---: | ---: | --- |
| 10 affected rules among 10,000 | 332.75 µs | 5.96 µs | Memory processing; about 56× faster in this fixture. |
| Half of 100 candidates missing a dependency | 7,917.88 µs | 66.67 µs | Memory processing; about 119× faster. |
| 1,000 affected rules | 3,165.92 µs | 604.92 µs | Memory processing; about 5.2× faster. |
| 100 actions using unique dependencies | 22,916.62 µs | 15,130.67 µs | Redis processing; about 1.5× faster. |
| Missing-dependency fixture with Redis | 19,175.46 µs | 7,718.12 µs | About 2.5× faster. |

With only 10 affected rules, the same-session M1 memory p50 measurements are
7.50 µs, 5.92 µs, and 6.04 µs at 100, 1,000, and 10,000 total rules. The previous
full-index scaling cost is gone; small timing differences remain machine noise.

For ten matching actions, Redis commands drop from **31 to 21 per batch**:
one MGET, ten SETs, ten PUBLISHes, and now zero verification GETs. No actions or
publications were suppressed to achieve that reduction. Eight-fact input batches
still execute the current repeated per-fact evaluations; M4 owns that semantic
change.

### Memory tradeoff and limits

Direct maps retain more loaded-program memory. The 10,000-rule fixture's warmed
Go heap rises from roughly 5.59 MB to 6.39 MB, about **0.8 MB / 14%** in this run.
This is a process-heap observation, not an exact map-size accounting. Per-batch
allocations and bytes pass the M0 budgets; for the sparse fixtures allocations
fall from 107 to 105 per batch. The unrelated-fact retention issue remains M4
work and is not claimed fixed.

The test machine is the M0 Apple M4 MacBook Air, 24 GB RAM, macOS 26.6.2,
Go 1.26.6, `GOMAXPROCS=2`. Redis is the same locally built 7.4.2 binary on TCP
loopback, with persistence disabled and no subscribers. Setup, compilation,
fact seeding, and 100 warmup batches are outside timed API execution.

These are synchronous API measurements, **not daemon end-to-end capacity or
production SLOs**. Ingress, decoding, queueing, and derived-event consumption
remain excluded. The M0 range-based limits are local investigation heuristics.
The same-session comparison runs each fixture's before repetitions followed
by its after repetitions; it is not a randomized statistical experiment.

## Profiling and trace selection

[CPU profile](cpu.pprof), [full top table](profile-top.txt), and
[application-focused table](profile-focused.txt) record a sustained 1,000-rule
in-memory benchmark. The profile includes substantial Darwin runtime/system
frames; application samples emphasize the evaluator and logging-level checks.
Named-map lookups do not provide a compelling reason for numeric IDs or a
predecoded instruction representation in this milestone. Those larger changes
are deferred until later workload evidence and the M2 safety net justify them.

The [trace-control benchmark](trace-bench.log) exercises 1,000 rules with two
conditions each, a counted memory store, and logs sent to `io.Discard`. Across
five runs, median mean batch time is 756 µs with condition records and 572 µs
with summaries only, about 24% lower. This is a different fixture from the M0
matrix, and benchmark means must not be confused with the matrix's p50 values.
The disabled-logging sub-benchmark showed substantial run-to-run variation and
is retained for transparency, not used for a cross-setting speed claim.

Reproduce profiling after the normal measurements, not concurrently with them:

```sh
GOMAXPROCS=2 go test ./pkg/runtime -run '^$' \
  -bench '^BenchmarkM1DenseEvaluation$' -benchtime=1s -count=5
GOMAXPROCS=2 go test ./pkg/runtime -run '^$' \
  -bench '^BenchmarkM1DenseEvaluation/disabled$' -benchtime=3s \
  -cpuprofile=/tmp/rex-m1.cpu.pprof -o /tmp/rex-m1-profile.test
go tool pprof -top /tmp/rex-m1-profile.test /tmp/rex-m1.cpu.pprof
```

The benchmark timer excludes fixture setup; the CPU profile covers the test
process, including setup. Use the full fixture suite for before/after claims.

## Verification and evidence

| Check | Evidence |
| --- | --- |
| Normal and race-enabled suites, uncached | Passed: [normal](test.log), [race](race.log). |
| Formatting, module metadata, vet, builds | Passed: [commands](commands-memory.json), [format](format.log), [tidy](tidy.log), [vet](vet.log), [build](build.log). No dependency changes. |
| All six release archives and checksums | Passed: [verification](archive-verification.txt), [checksums](archive-checksums.txt). |
| Vulnerability scans | No reachable vulnerabilities for [darwin/arm64](vulnerability.log) or [linux/amd64](vulnerability-linux.log); previously recorded non-reachable advisories remain. |
| Regression sensitivity | The diagnostic-read and publication-logging assertions [fail on the saved M0 source](before-regression-failures.log) and pass with M1. |
| Runtime compatibility | Tests cover priority ties, missing dependencies across disappearance/recovery, immutable candidates, repeated dependency records, action order, cancellation, and failures. |
| Trace controls | Default-on and explicit-off parsing are tested; runtime tests preserve correlated summaries and failures when conditions are disabled and restore records when enabled. |
| Benchmark equivalence | The comparator rejects changed fixtures/artifacts, sampling, semantic work, or inconsistent Redis command counts. All 25 full and seven paired groups pass. |
| Hosted CI/CodeQL | Must run on integration; M0's recorded hosted result covers only the base commit, not this working tree. |

Raw candidate evidence: [memory](memory.jsonl), [Redis](redis.jsonl),
[logging](logging-info.jsonl), [memory environment/identity](environment-memory.json),
[integration environment/identity](environment-integration.json), and
[integration commands](commands-integration.json).
Paired runs retain [binary/source identity](paired-identity.json),
[exact local command sequence](paired-command.sh), and separate
[before](paired/before) and [after](paired/after) samples.
The paired command file records temporary paths from this session; rebuild the
two binaries from the identified source snapshots when reproducing elsewhere.

## Resume and integration

Use the [baseline runner instructions](../../../scripts/baseline/README.md) with
the M0 sample counts: five memory runs of 1,000 batches, then three Redis runs
of 100 batches and three logging runs of 1,000 batches. The second invocation
may use `--skip-checks` when the first has validated the same Go sources. Copy
the accepted JSONL files into a separate comparison directory, then run:

```sh
python3 scripts/baseline/compare.py docs/baselines/rex-m0 /path/to/candidate-results
```

Keep direct-index/filter changes, read removal, and trace controls separately
reviewable when integrating; `m1-only.patch` identifies the total M1 delta.
Replace the working-tree source reference with merged PR/revision links when
available. Do not attribute the earlier v3 change to M1 or widen this work into
batch semantics, scripts, delivery guarantees, or concurrency.

Continue with REX-M2 in the [foundation roadmap](../../FOUNDATION_ROADMAP.md).

### PR review follow-up

PR #33 adds contextual errors for unmatched comparison groups, preserves churn
in comparison output, and captures tracked and untracked changes across the
entire fingerprinted source set. Historical measurement files remain unchanged.
CodeQL also identified an unchecked compiler allocation-size sum; compilation
now rejects integer overflow before allocation. Boundary tests and the full Go
normal/race suites and vet pass, along with the Python tooling regressions
(`python3 -m unittest discover -s scripts/baseline -p 'test_*.py'`). These review
fixes are subsequent to the measured snapshot; no new timing claim is made.
