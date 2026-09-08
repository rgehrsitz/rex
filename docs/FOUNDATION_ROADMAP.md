# REX foundation roadmap

Created: 2026-09-07. Last planning review: 2026-09-07.

Status: implementation underway; REX-M0 and REX-M1 complete locally.

## Purpose and authority

Make REX faster, more reliable, and easier to embed and extend while preserving
JSON rulesets, compiled bytecode, and a shared event-driven runtime. This is the
implementation roadmap agreed after the cleanup and engine-correctness work.
Use its milestone IDs in tasks, issues, and pull requests.

This document owns the implementation sequence, acceptance criteria, and progress
for the next stage. The [revival plan](REVIVAL_PLAN.md) retains the earlier
milestones; the [engine audit](ENGINE_AUDIT.md) owns verified defect evidence and
remediation status; the [evolution reference](EVOLUTION_REFERENCE.md) retains
design rationale and earlier proposals. Where their suggested future sequences
differ, follow this roadmap. An accepted direction is not a completed feature.

## Agreed direction

- Preserve the JSON -> bytecode -> runtime architecture and keep Redis as the
  first adapter.
- Evaluate an event batch against one snapshot, stage outputs, and explicitly
  define commit, conflict, failure, and derived-event behavior.
- Separate evaluation from state persistence, event consumption, and delivery.
- Make evaluation work proportional to affected rules and required facts;
  measure improvements before claiming gains.
- Provide explanation, scenario execution, simulation, and replay through the
  production evaluation core; verify it against an independent reference interpreter.
- Add a durable processing mode with explicit at-least-once delivery and
  idempotent commits before introducing consequential external actions.
- Bound state and execution, address script isolation, and improve deployment
  configuration and operational visibility.
- Build richer features only after their correctness and recovery prerequisites.

The decisions below settle implementation details within this direction. Record
decisions and supporting evidence as work proceeds; they are not a requirement
to seek renewed approval for routine implementation choices.

## Baseline and progress dashboard

The planning review inspected the working tree based on `dddcdba`, including
uncommitted priority/default/bytecode-v3 changes. `go test ./...` passed during
that review, with cached results for most packages. The review did not rerun
race checks, produce a performance baseline, or establish production capacity.
Do not mistake this working-tree assessment for verification of a released tag.

Status vocabulary: `Planned`, `In progress`, `Blocked`, `Complete`. A milestone is
complete only when its acceptance criteria and linked evidence are present.

| ID | Major chunk | Depends on | Status | Evidence / next action |
| --- | --- | --- | --- | --- |
| REX-M0 | Record an implementation baseline | None | Complete | [Local baseline report](baselines/rex-m0/README.md): checks, 108 measured runs, source fingerprints, and review budgets. Next: M1. |
| REX-M1 | Improve lookup and Redis efficiency | M0 | Complete | [Implementation and performance report](baselines/rex-m1/README.md): equivalent outputs, no budget flags, and documented map-memory tradeoff. Next: M2. |
| REX-M2 | Establish an independent semantics safety net | M0 | Complete | [Current-v3 safety net](../internal/semantics/README.md): memory store, 12 scenarios, 128 seeds, truth tables, mutation checks, and disassembly goldens. Local validation complete; review pending. Next: M4. |
| REX-M3 | Harden deployment and operational visibility | M0 | Planned | Fix startup errors, TLS/secrets, readiness, and latency visibility. |
| REX-M4 | Introduce deterministic batch evaluation and adapter boundaries | M1, M2 | Planned | Record state/commit semantics before changing execution behavior. |
| REX-M5 | Deliver explanation, simulation, and rule-development tools | M4 | Planned | Expose the scenario runner through stable CLI commands. |
| REX-M6 | Constrain script execution | M0; integrate with M4 | Planned | Retain disabled default; design isolated worker lifecycle and limits. |
| REX-M7 | Deliver durable event processing | M3, M4, M5 | Planned | Design journal, commit, acknowledgement, and crash recovery together. |
| REX-M8 | Extend the proven foundation | Capability-specific gates below | Planned | Start with ruleset reloads; split each capability into its own proposal. |

Default sequence: M0 -> M1 -> M2 -> M4 -> M5 -> M7 -> M8. M3 can run after
M0, independently of the core refactor. M6 can start after M0 and must move
earlier wherever scripts are enabled; final integration must satisfy M4's
evaluation contract. A scripts-disabled release need not wait for M6. These
are dependency lanes, not a promise of simultaneous staffing or delivery dates.

## REX-M0 — Record an implementation baseline

**Outcome:** subsequent changes have a reproducible starting point.

- [x] Identify the revision containing the accepted preceding fixes; preserve
  unrelated local changes and record any remaining audit defects.
- [x] Record Go, Redis, OS/architecture, logging settings, commands, fixture
  sizes, and revision with every baseline result.
- [x] Run the existing normal/race suites, vet, builds, and configured CI checks.
  Local equivalents passed; hosted CodeQL covers the base commit only and
  remains an integration gate, as detailed in the report.
- [x] Define benchmark fixtures varying total rules, affected rules, dependency
  overlap, missing facts, event batch size, matching rate, and action count.
- [x] Separate evaluation-only benchmarks from Redis integration/load tests.
  Include a real Redis run with stated network conditions before making an
  end-to-end performance claim.
- [x] Capture throughput, latency distribution, allocations, retained memory,
  Redis command counts, and logging overhead. Use repeated runs and set
  workload-specific regression budgets from those results, not invented targets.

**Exit evidence:** a linked baseline report and repeatable commands. M1 and M2
may add fixtures to the baseline, but comparisons must use the same fixture and
environment on both revisions.

**Completed 2026-09-07:** [report and evidence](baselines/rex-m0/README.md),
[reproduction instructions](../scripts/baseline/README.md), and
[measured ranges/budgets](baselines/rex-m0/summary.json). The baseline identifies
`dddcdbac40af` plus a retained source patch/fingerprints, including pending v3
changes; it is not an integrated release claim. Memory measurements isolate
the synchronous processing path from I/O; real-Redis measurements cover that
same API and do not claim daemon end-to-end capacity.

**Suggested change boundary:** baseline harness and report only.

## REX-M1 — Improve lookup and Redis efficiency

**Outcome:** each update avoids scans and round trips unrelated to its work.

- [x] Build direct rule-to-dependencies and rule-to-execution-location indexes
  at load time. Preserve source order and ascending priority.
- [x] Replace repeated dependency scans and nested removal loops with a
  non-mutating candidate filter using direct lookups.
- [x] Remove the post-action verification `GET` (REX-009).
- [x] Replace unconditional standard-library publish logs with configured
  structured diagnostics; make detailed evaluation traces selectable or sampled.
  Keep failures and useful event summaries observable.
- [x] Profile after these changes. Consider numeric rule/fact IDs or predecoded
  instructions only if profiles justify their added complexity.

**Acceptance criteria:**

- Existing action order, missing-dependency behavior, and valid artifact meaning
  remain unchanged; fixtures produce identical semantic results.
- Holding affected rules and dependencies fixed no longer adds a full-ruleset
  scan to event processing as total rules grow.
- Store instrumentation proves that an action no longer causes a diagnostic
  read; before/after results report both improvement and any regressions.
- Normal/race tests and benchmark budgets pass. Update REX-009 in the audit with
  the fixing revision and evidence.

**Suggested PRs:** direct indexes/filter; redundant read removal; trace controls.
Do not claim that command pipelining establishes reliable delivery.

**Completed locally 2026-09-07:** [M1 report](baselines/rex-m1/README.md),
[M0 comparison](baselines/rex-m1/comparison.md), and
[same-session comparison](baselines/rex-m1/paired/after/comparison.md).
The unchanged M0 harness verifies equivalent artifacts/actions and zero
diagnostic GETs. All 25 full and seven paired groups pass the provisional
review budgets. Normal/race suites and the local CI-equivalent checks pass.
Loaded-program maps add roughly 0.8 MB of heap in the 10,000-rule fixture;
per-event allocations decrease. Condition tracing remains enabled by default
and can be disabled explicitly. Profiling did not motivate a larger instruction
representation change. Source patches/fingerprints are retained until merged
revision links can be recorded; hosted CI remains an integration gate.

## REX-M2 — Establish an independent semantics safety net

**Outcome:** runtime changes can be checked against behavior defined outside the
bytecode interpreter.

- [x] Add a first-class in-memory store and declarative scenario fixtures with
  initial facts, ordered input batches, expected facts/actions, and expected errors.
- [x] Implement a small AST reference interpreter that does not reuse compiler
  traversal, emitted jumps, or bytecode execution to decide conditions.
- [x] Compare reference and bytecode execution over a deterministic corpus and
  generated valid rules. Preserve reproducible seeds and minimized failures.
- [x] Cover nested `all`/`any`, type mismatches, missing/null inputs, priority
  ties, overlapping dependencies, multiple actions, cycles, and store failures.
- [x] Use an injected clock and controllable action results in the harness.
  Keep arbitrary JavaScript out of the deterministic oracle until M6 supplies
  a suitable contract; test its integration separately.
- [x] Add deterministic disassembly fixtures and retain parser/loader fuzzing.

**Acceptance criteria:** scenarios run without Redis; differential tests detect
deliberately introduced semantic faults; generated failures can be replayed.
Tests identify which execution contract they target. Initially characterize
current behavior, then add a separate expected corpus for M4's new semantics;
do not silently rewrite old expectations to make a refactor pass.

**Suggested PRs:** in-memory scenarios; reference interpreter and differential
tests; deterministic disassembly fixtures. Public CLI polish belongs to M5.

## REX-M3 — Harden deployment and operational visibility

**Outcome:** startup and dependency failures are recoverable and visible, and
operators can identify slow or stalled processing.

- [ ] Make Redis construction return `(*RedisStore, error)`, pass startup
  cancellation/deadlines, update factories, and close resources on failure
  (REX-010).
- [ ] Add verified TLS configuration and documented environment-variable mapping
  for credentials and connection settings; define precedence and redact secrets
  (REX-011).
- [ ] Make readiness track ongoing subscription/connectivity and processing
  readiness, including disconnect/reconnect transitions. Keep liveness separate.
- [ ] Add latency histograms and bounded-cardinality rule/action outcome metrics.
  Make detailed traces configurable without hiding failures.
- [ ] Document and enforce routing from output facts to configured channels;
  report unreachable outputs before deployment where statically detectable.
- [ ] Document payload limits and overload behavior for Pub/Sub. Expose available
  drop/disconnect signals; retain unavailable queue lag as unavailable.
- [ ] Resolve or explicitly disposition remaining REX-013 tool/CI housekeeping
  using the current repository state rather than repeating historical findings.

**Acceptance criteria:** connection refusal, invalid TLS, canceled startup,
disconnect/reconnect, and shutdown tests exercise actual lifecycle behavior;
no library constructor terminates the process. Configuration tests demonstrate
precedence without logging credentials. Histograms reveal slow-event tails, and
metric labels remain bounded. Update audit findings individually with evidence.

**Suggested PRs:** constructor/context; TLS and environment configuration;
readiness; metrics; routing/limits. Keep dependency upgrades separate.

## REX-M4 — Introduce deterministic batch evaluation and adapter boundaries

**Outcome:** evaluation computes proposed results from explicit inputs; a
coordinator owns state transitions and delivery.

Conceptual boundary, not a frozen Go API:

```text
immutable program + snapshot + event batch + explicit evaluation context
    -> proposed actions/fact changes + structured trace + evaluation error

event source -> coordinator -> evaluator -> commit adapter -> derived event
```

The evaluator performs no direct Redis I/O and does not publish outputs. Clock
and script/function inputs must be explicit when used. Transport interfaces
must not require Redis message types. Keep interfaces small and driven by the
in-memory and Redis implementations.

- [ ] Record decisions D1–D5 below, including failure examples and truth tables.
- [ ] Extract an immutable loaded program and private, bounded evaluation state.
  Replace public mutable `Engine.Facts` with read-only inspection/snapshot APIs
  and document Go API migration.
- [ ] Load the union of required dependencies once, overlay all event facts,
  deduplicate affected rules, and evaluate against the same snapshot.
- [ ] Stage actions in deterministic rule/action order. Resolve write conflicts
  before persistence; a rejected evaluation must not mutate authoritative state.
- [ ] Separate event source, snapshot access, and commit responsibilities. Keep
  Pub/Sub operational during this migration and add adapter contract tests.
- [ ] Treat derived changes as subsequent bounded rounds. Apply per-rule,
  per-event, and chain-wide work limits with explicit failure behavior; a hop
  limit alone does not bound branching fan-out.
- [ ] Bound event bytes/facts, staged outputs, and retained state. Irrelevant
  dynamic fact names must not grow an engine-global map indefinitely.
- [ ] Version changed artifact meaning and update source/schema/CLI/runtime
  compatibility together, following [BYTECODE_COMPATIBILITY.md](BYTECODE_COMPATIBILITY.md).

**Acceptance criteria:**

- A two-fact event affecting one rule evaluates it once in that round, using
  both new values regardless of JSON key order.
- All rules in a round see the same snapshot; earlier staged writes cannot
  alter later conditions. Priority and source-order ties remain deterministic.
- Conflict, missing/null/invalid input, and cancellation cases match the written
  contract and the independent interpreter.
- Failure injection distinguishes evaluation failure, commit failure, and an
  unknown commit outcome. The coordinator never blindly rolls back or retries
  an externally committed result; adapter limitations are explicit.
- Unrelated fact-name churn leaves retained runtime memory bounded. Normal and
  race checks pass without introducing concurrent evaluation.
- A migration report compares representative old/new results and explains
  changed duplicate firing, visibility, and conflict behavior. No existing
  artifact silently acquires new execution meaning.

**Suggested PRs:** decision record and fixtures; behavior-preserving program and
adapter extraction; versioned batch/staged-write semantics; state/work bounds;
migration documentation. Keep format changes focused on the semantics they enable.

## REX-M5 — Deliver explanation, simulation, and rule-development tools

**Outcome:** authors can understand and verify rule changes before deployment.

- [ ] Add `rexc explain` for dependencies, priorities, bytecode/jumps, and action
  metadata; use evaluation traces to explain matches, skips, and errors.
- [ ] Add `rexc test` for named scenarios and `rexc simulate` for ordered event
  replay, with stable machine-readable output and meaningful exit codes.
- [ ] Record a replay bundle containing initial state/checkpoint, ordered inputs,
  program/source digests, execution-contract version, configuration, and explicit
  time/function results where necessary. Never substitute today's Redis state
  for historical state without marking the result as a different simulation.
- [ ] Compare two ruleset versions over the same replay bundle and report
  changed actions/facts and their causes.
- [ ] Add `rexc lint` with stable diagnostic IDs for conflicting writers,
  provable cycles, undefined script references, and routing mistakes. Clearly
  separate errors from warnings and enforce schema/parser agreement in CI.
- [ ] Add an artifact sidecar manifest for provenance and tooling, and a
  `rexd --dry-run` path that emits proposed results without committing them.

**Acceptance criteria:** documented examples run locally and in CI without
Redis; identical deterministic bundles produce identical ordered results;
simulation and dry-run cannot trigger external effects. Unsupported versions
and incomplete replay inputs yield clear errors or explicit limitations.

**Suggested PRs:** explain/manifest; scenario CLI; replay/comparison; lint;
daemon dry-run. Keep the CLI and production runtime on the same evaluator.

## REX-M6 — Constrain script execution

**Outcome:** a script cannot continue indefinitely inside the daemon after its
caller times out, and script state cannot leak across evaluations.

- [ ] Retain scripts disabled by default and reject or explicitly diagnose a
  ruleset requiring unavailable script capabilities at load time.
- [ ] Design a separate worker process protocol with bounded request/result
  sizes, hard wall-time termination, CPU/memory limits, and cancellation.
- [ ] Isolate mutable VM state per invocation or implement a verified reset;
  bound worker count, queue length, and restart rate. Kill and reap timed-out
  workers before reuse.
- [ ] Validate script syntax, references, and parameter contracts before use.
- [ ] Define clock/randomness/host-access behavior and record nondeterministic
  results needed for replay. Complete D6 before claiming deterministic scripts.
- [ ] Publish platform support for enforced limits. Where guarantees cannot be
  enforced, keep that capability unavailable rather than silently weakening it.

**Acceptance criteria:** infinite loops, memory exhaustion, malformed output,
worker crashes, timeouts, and caller cancellation are bounded and do not leave
orphaned workers or corrupt subsequent evaluations. No partial script result is
committed. Test compatibility with M4 and update REX-006 with verified guarantees.
Process separation alone is not a complete security boundary for hostile authors;
any untrusted-author claim also requires restricted host access and a reviewed
threat model.

**Suggested PRs:** protocol/worker lifecycle; enforced platform limits;
determinism and validation; evaluator integration. If JavaScript's value does
not justify this cost, record a deliberate removal decision and migration path.

## REX-M7 — Deliver durable event processing

**Outcome:** accepted events and pending outputs survive the documented failure
model and can be processed again without duplicating committed fact changes.

- [ ] Add a Redis Streams event-source adapter with stable event IDs, consumer
  groups, acknowledgements, pending-work recovery, bounded retries, and a
  dead-letter policy. Retain an explicitly best-effort Pub/Sub mode.
- [ ] Design input journal, authoritative state/checkpoint, event progress, output
  records, and deduplication as one recovery protocol. A historical event must
  not fetch arbitrarily newer dependency values during retry/replay.
- [ ] Persist state changes with a recoverable record of derived outputs before
  acknowledging the input. Define the supported Redis topology and atomic commit
  scope; command pipelining is not a commit protocol.
- [ ] Use stable action/output IDs derived from event identity, program version,
  and action identity. Persist deduplication across restart and define its
  retention relative to stream/replay retention.
- [ ] Resolve unknown commit outcomes before retrying. Define how poison events
  affect ordering and how operators repair/redrive them without losing identity.
- [ ] Establish one ordered state owner per partition initially. Multiple
  consumers alone do not guarantee safe ordering for shared facts.
- [ ] Define stream trimming, Redis persistence/replication assumptions,
  backpressure, maximum pending work, recovery procedures, and failure limits.
- [ ] Add actual backlog/lag, retry, recovery, and dead-letter metrics and make
  readiness reflect the durable processor's ability to accept/process work.

**Acceptance criteria:** a real-Redis fault suite interrupts processing before
commit, after commit/before acknowledgement, and during output dispatch. Under
the documented persistence/failure model, accepted events remain recoverable,
committed updates are not applied twice on retry, derived outputs retain their
identity, and a poisoned event follows the documented policy. Replay from a
checkpoint matches uninterrupted execution. Slow consumers respect resource
limits; recovery is demonstrated by a repeatable runbook.

**Rollout:** use shadow/dry-run comparison first, then one explicit producer and
consumer cutover with recorded cursors/IDs. Prevent uncontrolled dual consumption
from Pub/Sub and Streams. State how to drain or roll back without forgetting
pending work. At-least-once delivery is the transport contract; do not promise
exactly-once effects in external systems without their own idempotency support.

**Suggested PRs:** recovery decision and fault harness; stream consumption;
state/output commit and deduplication; retry/dead-letter recovery; metrics and
cutover documentation. These pieces form a complete release capability; do not
advertise durability after merging only the consumption adapter.

Redis references: [Pub/Sub delivery](https://redis.io/docs/latest/develop/pubsub/),
[Streams](https://redis.io/docs/latest/develop/data-types/streams/), and
[transaction behavior](https://redis.io/docs/latest/develop/using-commands/transactions/).
Validate commands and guarantees against REX's supported Redis versions when
implementing; transactions do not provide general rollback after command errors.

## REX-M8 — Extend the proven foundation

These are accepted follow-on directions, each requiring a separate scoped
milestone and acceptance tests before implementation. They are not one large
release or a reason to delay foundation work.

| Capability | Earliest gate | Required contract / evidence |
| --- | --- | --- |
| Safe ruleset reload and rollback | M4, M5 | Validate/warm before atomic swap; pin each in-flight event to one program version; retain old version on failure; coordinate version history with M7 recovery. |
| Typed fact declarations | M4, M5 | Define missing/null/invalid and numeric precision; compiler diagnostics and input validation agree; provide migration fixtures. |
| Temporal rules | M4, M5, M7 | Injected clock; persisted bounded timers/state; explicit event-time versus processing-time and late-event behavior; restart and boundary tests. |
| Optional change-only emission | M4, M5, M7 | Explicit opt-in equality/activation semantics, persisted state, and restart/duplicate tests; preserve deliberate repeated actions by default. |
| Partitioned concurrency | M4, M7 plus profiling evidence | Explicit fact/state ownership and cross-partition dependency policy; preserve per-partition order; race, failure, and load tests prove benefit. |
| Additional transports | M4; M7 for durable guarantees | Pass adapter contract tests and document ordering/delivery differences. |
| Webhooks and other external actions | M5, M7 | Outbox dispatch, stable idempotency keys, timeout/retry/dead-letter policy, and destination-specific guarantees. |

## Decisions to record before dependent implementation

Add the final choice, rationale, examples, and deciding PR/document to this table.
The recommendations are starting positions, not already implemented contracts.

| ID | Decision | Recommended starting position | Due / status |
| --- | --- | --- | --- |
| D1 | State authority and snapshot consistency | Specify what a snapshot represents. For durable processing, use coordinator-owned state rebuilt from checkpoints and ordered inputs; do not replay against arbitrary latest Redis values. Define how external writers enter that input history. | M4 design; refine M7. Open. |
| D2 | Conflicting outputs | Reject different values written to the same fact within one round by default; allow identical final fact writes to coalesce while retaining action trace. Do not make priority an undocumented winner policy or suppress future external actions. | Before M4 semantics. Open. |
| D3 | Missing/null/invalid facts | Distinguish them in representation and diagnostics. Prefer explicit indeterminate comparisons: true satisfies `any`, false fails `all`, otherwise unknown propagates; only true fires. Define full truth tables, including `NEQ`, and version the behavior. | Before M4 semantics. Open. |
| D4 | Failure and commit scope | Evaluation errors discard staged outputs. Specify adapter commit scope, observable partial/unknown outcomes, and recovery. Snapshot evaluation alone is not a storage transaction. | Before M4 semantics; complete recovery in M7. Open. |
| D5 | Derived rounds and resource budgets | Outputs feed a subsequent ordered round; enforce action, event, chain/fan-out, payload, and state limits. Define chain IDs, budget ownership, and restart behavior. | M4; persistence in M7. Open. |
| D6 | Script capability and determinism | Isolated bounded workers if retaining general JavaScript; explicit time/randomness inputs or recorded results for replay. Document supported platforms and host-access limits. | Before M6 integration. Open. |
| D7 | Durable topology, ordering, retention | Start with a documented single Redis commit domain and one state owner per partition; choose supported versions, persistence settings, retention, and consumer recovery together. | Before M7 implementation. Open. |

## Review, validation, and completion discipline

At the start of each major chunk:

1. Read this dashboard and linked audit entries; verify the current revision.
2. Set one milestone to `In progress`, link its task/issue, and record the next
   concrete PR. Check dependencies and changes made since the last review.
3. Resolve the decisions needed for that chunk in writing. Record acceptance
   fixtures, performance budgets where relevant, and compatibility implications.

At its completion:

1. Link merged revisions, meaningful tests, benchmark/replay reports, and any
   migration or operator instructions. Use the actual integrated revision.
2. Check off completed work and acceptance criteria. Mark partial work as such;
   successful code generation or a green unit suite alone is not a release claim.
3. Update the engine audit only for findings actually fixed and verified. Keep
   historical defect evidence rather than duplicating it here.
4. Record tradeoffs, remaining risks, and the next milestone. Update the review
   date and dashboard so a later task can resume from these files alone.

Run regression tests appropriate to each change. Core, state, adapter, and
script changes require relevant normal/race tests; retain CI vet/build/security
checks. Compiler/semantic changes also require differential, fuzz-corpus, and
compatibility checks. Delivery changes require real-Redis failure tests.
Performance claims require comparable repeated measurements. Documentation-only
updates need link/content checks, not a redundant full runtime test run.

Keep behavior-preserving refactors, semantic migrations, dependency upgrades,
and new capabilities in separately reviewable changes. Within an artifact
version preserve meaning; version an intentional change and document rollback
constraints, including state and journal compatibility where applicable.

### Milestone review record

Copy this block into the evidence log at each review:

```text
Date / milestone / status:
Starting revision -> integrated revision or PR:
Delivered behavior:
Acceptance evidence (commands, fixtures, reports):
Decisions made and rationale:
Compatibility / rollout / rollback:
Remaining risks or blockers:
Next concrete action:
```

### Evidence log

- 2026-09-07: roadmap created following acceptance of the foundation review.
  No implementation milestone is claimed complete. Begin with REX-M0.
- 2026-09-07 / REX-M0 / Complete (local baseline): added the deterministic
  benchmark matrix, counted memory/Redis harness, repeatable runner, and summary
  budgets. Normal/race tests, vet, formatting, module metadata, builds, release
  archives/checksums, and darwin/linux vulnerability scans passed. Recorded 108
  accepted runs; discarded the initial Redis attempt after correcting a runner
  log-file collision. See the [baseline report](baselines/rex-m0/README.md) for
  source identity, commands, limitations, open audit work, and hosted CI scope.
  No runtime semantics or dependencies changed; one pending-source indentation
  was normalized. Next action: M1 direct indexes and non-mutating filtering,
  followed by the separately reviewed diagnostic GET removal.
- 2026-09-07 / REX-M1 / Complete locally: direct rule/dependency maps and a
  non-mutating filter replace the scans; diagnostic GETs and unconditional
  publication logging are removed; per-condition tracing is configurable with
  its existing default preserved. Regression assertions fail on M0 and pass on
  M1. Recorded 99 full candidate runs, 58 same-session before/after runs, trace
  benchmarks, and a CPU profile. No comparison budget flags; artifact hashes,
  action counts, priority ordering, and missing-dependency behavior agree.
  The [report](baselines/rex-m1/README.md) records memory costs, commands,
  fingerprints, and pending integration/hosted-CI scope. Next: REX-M2.

### 2026-09-08 — REX-M2 local completion

- M0/M1 and priority/v3 work merged in [PR #33](https://github.com/rgehrsitz/rex/pull/33),
  revision `79b64fad4b217525b294c13a0115af4741d286ef`; all PR checks passed.
- Added the [M2 contract and reproduction guide](../internal/semantics/README.md),
  first-class JSON memory store, authored current-v3 scenarios, independent AST
  evaluator, seeded differential tests/reduction/replay, and disassembly goldens.
- Three deliberate semantic mutations are detected. Comparisons include state
  after each dispatched fact and ordered action attempts with an injected clock.
  No semantic failure was found in the initial deterministic generated corpus.
- Normal and race-enabled `go test -count=1 ./...`, `go vet ./...`, and
  `go build ./...` pass locally. Existing script integration tests remain separate
  from the deterministic oracle. Ten-second fuzz smoke runs (`GOMAXPROCS=2`)
  passed: parser 704,919 executions; loader 199,543 executions. These are bounded
  smoke checks, not exhaustive fuzzing.
- Production compiler/runtime behavior and bytecode format remain unchanged.
  M4 must add a separate contract/corpus for intentional semantic changes.
- Next default milestone: **REX-M4**. M3 remains an independent operational lane.
