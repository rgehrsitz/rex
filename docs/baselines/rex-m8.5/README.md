# REX-M8.5 local acceptance evidence

Starting revision: `f5a45d7` (merged M8.4 PR #43). Implementation is on
`codex/rex-m8-partition-readiness`; PR review/integration is pending.

## Scope and decision

Delivered a deterministic offline ownership-group analyzer and reproducible
benchmarks of the current batch evaluator/coordinator. Concurrent execution is
**no-go at this stage**: no representative durable scaling evidence or enforced
cross-namespace fact ownership exists. This milestone establishes prerequisites;
it does not close the roadmap's partitioned concurrency gate.

Claude CLI was consulted on the architecture. It independently recommended
ownership enforcement and durable profiling before concurrency, identifying
shared public fact keys, program-scoped timer keys, and retained-artifact
cleanup as hazards. Its broader proposal is split into this analysis/baseline
chunk and a subsequent ownership/durable-load chunk. We have not adopted its
optional cross-owner snapshot-read policy: the analyzer conservatively joins
shared reads until a separate consistency contract is accepted.

## Measurements

Environment: Apple M4, darwin/arm64, macOS 26.6.2, Go 1.26.6, default
GOMAXPROCS=10. Redis is not involved; no application logging is enabled by the
fixture. Three 500 ms measurements per case; fixture compilation/setup excluded.
The desktop and Claude consultation were active, so these are local development
measurements rather than isolated capacity certification.

Each rule has a trigger and a persisted Boolean gate, then writes one distinct
Boolean output. Sparse events select one rule; dense events select all rules.
Both sizes use default runtime limits except ActionsPerRound=1024 and
EventFacts=1024, so the 1,000-output event and derived round fit the explicit
budget. Default deployments would reject that dense event at 256 actions.
The evaluator checks output count. The coordinator checks two rounds and drains
and checks publications after every event; drain/copy cost is included, and
retained publication memory is bounded. Each coordinator/store is reused across
iterations: after the first event, outputs already exist. This measures steady-state
reprocessing, not cold-start event cost; v4 still emits every matching write.
Chain IDs are fixed because this is non-durable memory processing, with no
identity generation/deduplication cost.

Median ns/op from [raw measurements](benchmarks.txt):

| Rules | Affected rules | Pure evaluate | Coordinator + memory + publication drain |
| --- | --- | --- | --- |
| 10 | 1 | 1,294 | 3,978 |
| 10 | 10 | 7,809 | 23,212 |
| 1,000 | 1 | 1,188 | 4,235 |
| 1,000 | 1,000 | 932,563 | 2,698,528 |

Sparse candidate selection does not scale with total rules in this fixture.
Dense work costs much more, but its shared trigger also makes it one ownership
group under the conservative contract. These are sequential baseline values,
not a before/after comparison or evidence of parallel speedup. V4 is used to
isolate base batch costs; typed validation, timers and change-only behavior need
additional representative workload cases before a concurrency go decision.

A separate 5-second dense coordinator [profile run](profile-run.txt) has
[full top](profile-top.txt) and [application-focused cumulative](profile-focused.txt)
tables. Darwin runtime/system samples dominate the full profile; application
stacks include evaluation, candidate sorting, JSON size accounting and allocation.
Cumulative entries overlap and must not be added. The profile includes process
setup even though benchmark timing excludes it. It cannot diagnose Redis wait
cost or establish that coordinator serialization is the production bottleneck.

## Reproduce

```sh
go test ./pkg/runtime -run '^$' -bench BenchmarkBatchPartitionBaseline -benchmem -benchtime=500ms -count=3
go test ./pkg/runtime -run '^$' -bench 'BenchmarkBatchPartitionBaseline/rules=1000/dense=true/coordinator-memory$' -benchtime=5s -cpuprofile=/tmp/rex-m85.cpu.pprof -o /tmp/rex-m85-profile.test
go tool pprof -top /tmp/rex-m85-profile.test /tmp/rex-m85.cpu.pprof
go tool pprof -top -cum -focus='rgehrsitz/rex' /tmp/rex-m85-profile.test /tmp/rex-m85.cpu.pprof
```

Run profiling separately from normal measurements. Raw binary profiles are
local temporary artifacts; the source fixture, exact commands and text tables
are retained here for reproduction.

## Validation

The analyzer tests cover independent components, shared readers/writers,
derived chains and transitive bridges, nested temporal and change-only rules,
unused typed declarations, deterministic output, source/artifact CLI parity,
and invalid input with clean stdout. Runtime benchmark fixtures assert expected
writes, rounds and publications rather than timing unchecked work.

Completed locally:

- `go test -count=1 ./...` and `go test -race -count=1 ./...` passed.
- `go vet ./...`, `go build ./...`, and `git diff --check` passed.
- `bash scripts/test-m5-cli.sh` passed; the new `go run ./cmd/rexc
  partition-plan -rules examples/m8-partitions/rules.json` smoke check reports
  the expected two groups.
- Explicit v4, v5, v6 and v7 analyzer tests passed, including the combined v7
  typed/temporal/change-only case. A focused race run covered the final tests.
- Claude's implementation review found no correctness defects. Its two
  documentation notes (pending validation text and paragraph wrapping) were
  addressed. Claude reviewed connectivity, ordering, capability validation,
  CLI errors and benchmark medians.

No runtime/compiler production behavior, artifact format or dependencies changed;
real-Redis fault/scaling tests belong to the following ownership milestone.
Hosted CI and PR review remain pending.

Benchmark fixture SHA-256:
`f22bfb9530e02b344049961bbdebce6c415581edd201370ff266b086faedfbba`.
