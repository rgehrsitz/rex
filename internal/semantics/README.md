# Current-v3 semantics safety net (REX-M2)

This package is internal test tooling. It characterizes the v3 implementation
before M4 changes batch and commit semantics. It is not the proposed public
simulation CLI (M5), a production evaluator, or a JavaScript oracle.

## Independent decisions

`reference_test.go` reads the authored JSON AST into the shared data structs,
walks conditions itself, discovers dependencies itself, implements typed
comparisons itself, and orders rules independently. It never calls compiler
traversal, code generation, bytecode validation, runtime comparisons, or emitted
jumps to decide whether a rule matches. Only the production side calls the
strict parser, compiler, writer, and runtime loader. Both sides receive fresh
stores and independently decoded ASTs. Sharing the store and dispatch harness
is intentional: the oracle checks evaluation semantics, not transport code.
Authored expected outcomes and separate memory-store tests check the harness.

The contract deliberately preserves these behaviors:

- Producers persist all facts in a batch before dispatch. Input keys are sorted
  and evaluated one at a time; one rule can execute more than once in a batch.
- All dependencies of candidate rules are read before execution, including
  dependencies on an `any` branch that might not be needed logically. A missing
  or null dependency filters the entire rule. A null triggering value is kept
  locally and fails typed comparisons.
- Numeric, string, and boolean comparisons do not coerce mismatched types.
  Even `NEQ` returns false on a mismatched or null value.
- Lower priority runs first; ties retain source order. Actions run in source
  order and immediately affect subsequent rules' local view.
- An action updates local state before calling the store. A failed write leaves
  that local value behind. A failed publication may leave the persisted write
  behind. Earlier successful actions remain committed and processing stops for
  the remaining facts in that batch. Later batches can still run.
- The action limit applies per rule invocation, not per incoming batch.
- Derived publications are explicitly delivered by the harness only when a
  fixture requests it. Cycles stop at the harness delivery budget. The runtime
  itself does not promise cycle detection or durable/exactly-once delivery.

## Corpus and controls

`testdata/current-v3.json` contains 12 authored scenarios with initial facts,
ordered batches, expected authoritative/local facts, ordered action attempts,
and errors. Error injection selects a dependency read, action write, or
post-write publication by one-based operation number. The clock is injected
into the store wrapper: action-attempt timestamps start at the Unix epoch and
advance one second per attempt. No wall-clock timing drives assertions.

Both evaluators are compared after every dispatched fact, including failed
ones, so later writes cannot conceal a transient difference. Final facts,
action attempts/outcomes/timestamps, and errors are also compared. The 12
operator truth tables exercise true, false, null, and wrong-type inputs.
Generated scenarios use fixed seeds 0–127, nested groups up to three levels,
overlapping dependencies/output targets, priority ties, multiple actions,
missing/null/typed values, limits, batches, and scripted store failures.

On a generated mismatch, the test prints the seed and a reduced JSON scenario.
The deterministic greedy reducer removes rules and batches only when the
mismatch persists. It produces a smaller reproducer, not a globally minimal
AST. Save the printed JSON as a new regression fixture when a defect is found;
no semantic failures were found in the initial 128-seed run.

```sh
go test ./internal/semantics ./pkg/store
REX_SEED=42 go test ./internal/semantics -run TestGeneratedCurrentV3 -count=1
REX_SCENARIO=/absolute/path/to/failure.json go test ./internal/semantics -run TestReplayScenario -count=1
```

Three deliberate mutations (numeric comparator, action target, priority)
produce valid, checksum-correct artifacts and are detected by the differential
suite. Mutation tests leave production sources unchanged. Reducer tests verify
that reduction preserves a known failure predicate.

Three checked-in disassembly goldens cover a simple comparison, priority ties
with multiple actions, and nested logic. The bounded decoder lives only in the
test harness and is independent of the AST oracle. Listings show instruction
section offsets, decoded operands, jump destinations, and deterministic indices.
To intentionally update them after reviewing a format/contract change:

```sh
REX_UPDATE_GOLDENS=1 go test ./internal/semantics -run TestDisassemblyGoldens -count=1
```

Parser and loader fuzz targets remain in place. Scripts stay outside the
oracle; existing runtime and end-to-end script tests still run in `go test
./...`. M6 must define an isolated deterministic script contract before adding
JavaScript to these comparisons.

## Memory store

`store.NewMemoryStore(initial)` returns a `ContextStore` for JSON-compatible
facts. JSON copying prevents callers from mutating stored nested values and
normalizes numbers as Redis decoding does. It supports cancellation, close,
thread-safe access, independent snapshots, and ordered publications retrieved
with `DrainPublications`. `Snapshot` and `DrainPublications` return a value and
an error; copy failures are reported, and a failed drain preserves its queue.
The zero-value store is also usable. Missing and null facts both read as nil. It has no
routing, background consumer, automatic cycle execution, TTL, or persistence.
Its write-and-enqueue is atomic; the Redis adapter has a different failure
contract, which the controlled test wrapper explicitly models.

## M4 migration rule

Keep this corpus as `current-v3`. Add a separate, explicitly named M4 corpus
and oracle contract for snapshot/batch/commit semantics. Do not update these
expectations merely to make a refactored runtime pass. Intentional differences
need migration examples and a documented decision. M2 is a safety net, not a
claim that its current behavior is the desired final system.

## Validation (2026-09-08)

Local uncached normal/race suites, vet, and all-package builds pass. The default
CI `go test -race ./...` includes the scenarios, generated comparisons, mutation
checks, goldens, memory-store ownership/concurrency tests, and Redis JSON-parity
test (using miniredis, with no Redis service required). Parser and loader fuzz
smoke runs are recorded with the milestone evidence in the foundation roadmap.
This M2 implementation is local work pending review; no performance improvement
or hosted check result is claimed.

PR review follow-up: harness setup now references the production priority and
action-limit constants, while `TestCurrentV3Defaults` pins their characterized
values (10 and 32). An intentional default change therefore still requires
contract review rather than silently changing both sides of the comparison.
