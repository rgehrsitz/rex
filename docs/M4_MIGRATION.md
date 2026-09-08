# REX-M4 migration and validation

Status: merged in [PR #35](https://github.com/rgehrsitz/rex/pull/35), revision
`f6e036c`. Execution contract v4 is the default for `rexc` and `rexd`.
[D1–D5](decisions/REX-M4.md) define the semantics, truth tables, adapter failure
scope, and budget ownership.

## Before/after examples

These are behavior comparisons, not performance measurements. The independent
corpora live in `internal/semantics/testdata/current-v3.json` and `batch-v4.json`.

| Trigger / scenario | V3 | V4 |
| --- | --- | --- |
| Both `a` and `b` change; one rule depends on both | Rule fires twice, one per fact | Fires once in the round using both new values |
| First rule writes `out=true`; later rule tests `out` | Later rule sees earlier write immediately | Later rule sees the pre-round snapshot; new `out` is a subsequent round |
| `any(a > 0, missing == true)` with positive `a` | Missing dependency filters the rule | True branch wins over Unknown; rule fires |
| Missing/null/wrong type in `NEQ` | False or prefiltered | Unknown; only a final True fires |
| Two matching actions write different values to `out` | Sequential writes; final action wins | Entire current round rejected before persistence |
| Two actions write the same value | Two writes/publications | One coalesced write, two action records |
| Action write fails | Local `Engine.Facts` could still contain the failed write | No retained fact cache; commit outcome reported explicitly |
| Reply lost after an applied write | Caller sees an error with no reconciliation latch | Unknown; stop and reconcile before constructing a new coordinator |
| Unrelated fact-name churn | V3 cache grows | V4 retains no event facts |

Identical values coalesce only within a round. This is not change-only emission:
a rule that writes its own triggering fact can cycle until a chain budget fails.
Earlier successful rounds stay committed if a later round fails. Input facts
are producer-owned; rejecting derived evaluation does not undo producer writes.

## Deployment cutover

1. Preserve v3 source, artifacts, and configuration. Run current-v3 and batch-v4
   scenarios for representative rules; explicitly review duplicate actions,
   visibility changes, and conflicting writers.
2. Compile script-free source with `rexc -rules rules.json -output rules.v4`.
   `rexc validate` validates the v4 source/capability bounds. Use
   `rexc -legacy-v3` only when intentionally retaining the old contract.
3. Configure `bytecode_file` for the v4 file. Keep
   `engine.allow_legacy_v3=false` and `engine.scripts_enabled=false`.
4. Producers persist their input facts and publish a single JSON object per
   event to the configured input channel. Rex performs one MGET for the union
   of dependencies absent from that round's event, overlays every event fact,
   and evaluates against that fixed view. With multiple writers this is a
   latest-value point-in-time read, not a durable historical snapshot.
5. Output fact writes use the commit adapter. Redis publishes one observation
   per successful round on `rex_results`, marked `_rex.kind=committed_output`.
   V4 does not consume that observation as another input; derived rounds already
   run locally. Do not route it into a v3 consumer or republish it as fresh input.
   Existing consumers of per-key-prefix output channels must migrate explicitly.
6. On partial/unknown commit, the daemon exits its consume loop with an error.
   Reconcile authoritative facts and downstream notifications before restart.
   Restart clears the in-memory latch; it does not recover or deduplicate work.
   M7 supplies that durable protocol. Never blindly replay the failed event.

Rollback means deploying the retained v3 artifact with explicit
`engine.allow_legacy_v3=true`, routing outputs according to the old convention,
and accepting old semantics. Coordinate producer/consumer cutover; do not run
both evaluators on the same input stream and duplicate effects.

## Go API migration

- Public mutable `Engine.Facts` is removed. `Snapshot()` returns a copy for
  legacy v3 inspection; v4 returns an empty map because it retains no fact state.
- Use `Engine.EvaluateBatch(ctx, facts)` for `ChainResult`, staged action traces,
  and explicit per-round commit outcomes. `ProcessBatchContext` returns just the
  error. Maps use decoded JSON types (`float64`, string, bool, or nil).
- `LoadProgram` owns an immutable validated IR. `Program.Evaluate` receives an
  explicit snapshot/event/limits/budget and performs no I/O. Its condition trace
  distinguishes missing, null, invalid, and present values. JavaScript and
  temporal/random functions are unavailable, so no implicit clock is read.
- `NewCoordinator` accepts separate `SnapshotReader` and `Committer` adapters.
  Calls serialize; configuration setters are initialization-only. V3 embedded
  single-fact APIs remain for migration and the frozen safety net.
- `EventSource`/`EventSubscriber` carry transport-neutral events and lifecycle.
  Redis subscription types remain inside the adapter and legacy test wrappers.
- Memory-store atomic commit does not generalize to Redis. The Redis writer
  uses a separate client with command retries disabled, validates before writing,
  and conservatively reports Unknown after any dispatched SET error. A publish
  error after acknowledged writes reports Partial. Neither adapter retries.

## Bounds

All limits are positive. `engine.max_actions_per_evaluation` supplies the
per-rule v4 limit. Remaining `engine.batch` defaults are:

| Setting | Default |
| --- | ---: |
| event_bytes | 1,048,576 |
| event_facts | 256 |
| snapshot_bytes | 4,194,304 |
| actions_per_round | 256 |
| chain_actions | 1,024 |
| chain_work | 100,000 |
| rounds | 16 |
| staged_bytes | 1,048,576 |

`chain_work` counts rule evaluations and visited condition nodes across all
rounds; chain actions count attempts, including identical coalesced writes.
Each round validates staged outputs and the resulting next-event size before
commit. An exceeded later-round budget does not roll back earlier commits.
Limits have finite hard ceilings enforced by `Limits.Validate`. `max_event_hops`
continues to validate ingress metadata; local chain limits additionally bound
branching fan-out. No unbounded engine-global fact map exists in v4.

Transport payloads are rejected before JSON decoding when over 1 MiB. Snapshot
values are checked before JSON decoding when their aggregate exceeds 4 MiB.
These application bounds do not cap memory already allocated by the Redis
protocol client to receive an oversized server response; trusted deployment
input/storage limits are still required. Pub/Sub can lose messages and exposes
no durable backlog or replay history. M3/M7 own stronger operational guarantees.

## Reproduction and evidence

```sh
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go test ./internal/semantics -run 'TestIndependentV4|TestAuthoredBatchV4'
go test ./pkg/runtime -run 'TestBatch|TestProgram'
REX_REDIS_TEST_ADDR=127.0.0.1:16379 go test ./pkg/store -run TestRealRedisBatchOutcomes -count=1 -v
```

The opt-in Redis test requires a disposable standalone server and uses unique
keys cleaned on completion. It checks a server-applied SET whose acknowledgement
is replaced by an injected error, a publication error after persistence, and
connection refusal. Fault injection occurs at the client hook boundary, not a
process-crash or packet-loss simulation. Adapter tests also verify cancellation,
missing/null/invalid representation, JSON parity, command-retry configuration,
and event-source shutdown. Coordinator tests verify no retries and the latch.
These checks are M4 evidence, not M7 durability or recovery evidence.

### Review clarifications

Run one active v4 consumer per input channel/commit domain. Coordinator locking
serializes only one process; Pub/Sub broadcasts to every subscriber. Multiple
daemons duplicate evaluation and notifications and may observe different
snapshots. Running two consumers is not an HA configuration; M7 owns that design.

The transport rejects raw input above 1 MiB. `engine.batch.event_bytes` may lower
the evaluator's fact payload budget; that check happens after decoding. These
are separate bounds on raw envelopes and decoded facts. Redis also measures the
complete serialized output envelope against 1 MiB before performing any writes.
Metadata overhead can therefore reject an otherwise valid staged payload.

Embedded callers must carry decoded event metadata with `eventcontext.WithMetadata`.
The v4 engine ignores `committed_output` and rejects other non-empty kinds.
`RoundResult.commit_error` retains adapter errors per round; earlier successful
rounds remain successful even if a later round fails. `SetMaxActionsPerEvaluation`
now returns an error for invalid v4 limits without changing either limit field.
Legacy v3 keeps its non-positive/unbounded behavior. Nil v4 fact state is
intentional: Go defines iteration over a nil map, and v4 never retains facts.
