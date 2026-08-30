# Rex evolution reference

This is the durable reference for product, architecture, and engineering ideas
identified while reviving Rex. It complements the [revival plan](REVIVAL_PLAN.md):
that document tracks committed milestones, while this one records the reasoning,
open decisions, and deliberately deferred possibilities behind future work.

Last reviewed: 2026-08-29.

For a verified, prioritized list of engine findings discovered immediately
after this review, see [ENGINE_AUDIT_2026-08-30.md](ENGINE_AUDIT_2026-08-30.md).

## Current position

Rex is a compact, event-driven rules engine: JSON rulesets compile into
versioned bytecode, and `rexd` evaluates incoming Redis fact events and writes
resulting facts. The project should evolve incrementally; the compiler ->
bytecode -> shared-runtime model is worth preserving.

The maintenance and delivery baseline is in place:

- Go `1.26.6`, current direct dependencies, Dependabot, CI, CodeQL, and tagged
  release archives are configured.
- Bytecode version 2 is deterministic, CRC-checked, structurally validated,
  fuzz-tested, and documented.
- `rexd` owns subscription lifecycle, supports graceful shutdown, traces,
  health and Prometheus-style metrics, bounded action execution, and bounded
  derived-event hops.
- Scripts are disabled by default.
- The repository has a Docker Compose smoke demo and a successful `v0.1.0-alpha`
  release.

The 2026-08-29 local baseline passed `go test ./...`, `go test -race ./...`,
`go vet ./...`, `go build ./...`, and `go mod tidy -diff`; aggregate statement
coverage was 83.2%. The locally installed `govulncheck` must be rebuilt with Go
1.26 before it can be used locally; CI remains the project scan of record.

## Product boundaries

### Preserve

- JSON ruleset -> bytecode -> one shared `rexd` runtime.
- Redis as the first transport and fact-store adapter.
- An event-driven daemon rather than a fixed periodic scheduler.
- Small, independently releasable changes with thorough regression tests.

### Do not claim

- Redis Pub/Sub is durable, queued, or exactly-once. It is a best-effort
  transport and does not expose queue lag.
- The current Otto integration is a sandbox or safe for untrusted scripts.
- A malformed but syntactically valid ruleset has fully defined business
  semantics until the language-validation work below is complete.

### Current decisions

- Scripts are for trusted rulesets only and remain disabled by default.
- NATS and other transports wait until the subscription/transport boundary is
  independent of the Redis implementation.
- A generated, per-ruleset application is out of scope; Rex deploys bytecode
  to a shared runtime.

## Correctness work to do before broadening the feature set

### Rule language and action contract

The compiler must reject shapes whose meaning it cannot faithfully execute.

- Require every condition group to contain exactly one of `all` or `any`.
- Forbid a node from being both a leaf condition and a nested group.
- Reject duplicate rule names.
- Align validation with runtime support. In particular, either implement an
  action such as `sendMessage` or reject it during compilation; do not emit a
  bytecode artifact that can only fail at runtime.
- Validate referenced scripts and their syntax/identifiers before deployment.

Add table-driven compile-and-run semantic tests for nested boolean groups,
invalid group shapes, duplicate names, unsupported actions, and scripts.

### Runtime state and event semantics

- Candidate rule slices are copied before filtering missing dependencies, so
  evaluation no longer mutates the compiler-generated fact-to-rule index.
- Precompute rule dependency lookup rather than scanning all dependencies for
  every candidate rule.
- Make Redis construction return errors rather than terminating the process
  through a logger fatal call.

The main product decision is the meaning of a fact-object event. The
recommended direction is **atomic event batches**: first apply all input facts
to a snapshot, evaluate the union of affected rules against that snapshot, then
stage and commit output writes. This prevents evaluations against transient
partial state and makes an event with several facts one coherent state
transition.

This is deliberately not a periodic scheduler. It adapts the useful
snapshot/staged-write property to Rex's event-driven model.

## High-value product improvements

### 1. Compiler linting

Add `rexc lint` after structural validation and before bytecode generation.
Use stable diagnostic IDs, source locations, and a `--fail-on-warnings` flag.

Initial errors:

- duplicate rule names;
- ambiguous condition groups;
- unsupported action types;
- undefined script references;
- static write cycles when they can be proven.

Initial warnings:

- multiple rules writing the same fact;
- a rule writes a fact it (directly or indirectly) reads;
- actions that repeatedly publish an unchanged value;
- rules or outputs that are never referenced.

Lint should explain a concrete risk and suggested fix. It is more valuable now
than a large schema/catalog system because it improves existing JSON rulesets
without changing their authoring model.

### 2. Simulation and scenario testing

Add a declarative test suite and an in-process simulator:

```text
rexc simulate -rules rules.json -events history.jsonl --trace
rexc test -rules rules.json -suite scenarios.json
```

A suite should support named scenarios, ordered fact-event batches, optional
delays for future temporal rules, and expected facts/actions. Execute the first
version with an in-memory store, deterministic clock, and structured trace
output. Keep Redis/Compose tests as a separate integration layer; do not begin
with an external Redis polling test runner.

### 3. Artifact provenance manifest

Keep bytecode format 2 unchanged initially. Let `rexc` optionally write a
sidecar manifest recording the source-ruleset digest, compiler version,
bytecode checksum, rule names, fact dependencies, and declared actions. This
makes deployments reviewable and gives simulators and tooling useful metadata.

### 4. Delivery reliability

Choose the contract before adding consequential external actions:

- **Best-effort v0.1:** remain on Redis Pub/Sub, document possible loss and
  duplicate delivery, and require idempotent consumers.
- **Durable processing:** add Redis Streams (or another queued transport) with
  explicit at-least-once acknowledgement and deduplication semantics.

Do not add webhooks, queues, retries, or dead-letter handling until this choice
is made. New actions amplify delivery ambiguity.

## Ideas borrowed from PulsarSuite

PulsarSuite is a related C# rules platform with a richer authoring and testing
surface. Its strongest ideas are semantic and operational, not architectural.

### Adapt now

| PulsarSuite idea | Rex adaptation | Why |
| --- | --- | --- |
| Snapshot reads and staged writes | Atomic event-batch evaluation | Avoid partial-state and read-after-write surprises. |
| Compiler lint with named diagnostics | `rexc lint` with stable IDs | Catch mistakes before bytecode reaches `rexd`. |
| Multi-step test scenarios | `rexc test` / `rexc simulate` with an in-memory engine | Make rule changes safe and repeatable in CI. |
| Rule/dependency manifest | Optional bytecode sidecar manifest | Improves review, deployment, and tooling. |

### Defer until the foundations are proven

- **Temporal conditions** such as a threshold sustained over time. Introduce
  them only with an injected clock, bounded tracker state, restart behavior,
  and exhaustive boundary tests.
- **`on_change` emission** for redundant-update suppression. It is promising
  for idempotency and feedback-loop reduction, but needs clearly persisted
  rule/action activation state.
- **Missing-data policy.** PulsarSuite's explicit indeterminate value is a
  useful model. Rex should first define missing/invalid fact behavior and
  linting; full three-valued logic is a later, backward-incompatible language
  decision.

### Do not copy

- Per-ruleset C# application generation and the resulting runtime/template
  machinery. Rex's bytecode and shared daemon are much smaller to build,
  distribute, patch, and operate.
- A mandatory YAML migration, sensor catalog, or broad expression language.
  Improve the current JSON language first.
- A fixed 100 ms scheduler. Rex should stay event-driven.
- Heuristic automatic expected-output generation. It can assist authors later,
  but explicit scenarios are the trustworthy source of test truth.

PulsarSuite remains a source of design inspiration, particularly its
[runtime semantics](https://github.com/rgehrsitz/PulsarSuite/blob/master/Pulsar/docs/Runtime-Evaluation-Semantics.md),
[linting model](https://github.com/rgehrsitz/PulsarSuite/blob/master/Pulsar/docs/Compiler-Linting-Rules.md),
and [scenario-test approach](https://github.com/rgehrsitz/PulsarSuite/blob/master/BeaconTester/README.md).

## Suggested implementation order

1. **Compiler-truthfulness PR:** strict condition grammar, duplicate names,
   action/runtime alignment, and regression tests. Immutable candidate
   filtering was completed separately as REX-001.
2. **Lint PR:** a small diagnostic model and the initial high-confidence rules.
3. **Event semantics PR:** decide and implement batch snapshot/staged-write
   behavior, with trace and failure-path tests.
4. **Simulation PR:** in-memory scenario runner and `rexc simulate`; add a
   small suite format with fixtures.
5. **Reliability decision:** explicitly retain best-effort Pub/Sub or adopt a
   durable transport before adding external side-effect actions.
6. **Temporal and richer language features:** only after the preceding work
   has a stable test and semantics foundation.

Keep each item separate from dependency bumps, bytecode-format revisions, and
unrelated refactoring. If a feature changes bytecode meaning or layout, follow
the compatibility process in [BYTECODE_COMPATIBILITY.md](BYTECODE_COMPATIBILITY.md).

## How to maintain this document

- Update this reference when a product decision is made, a newly discovered
  risk changes priority, or an idea is explicitly declined.
- Move accepted, near-term items into [REVIVAL_PLAN.md](REVIVAL_PLAN.md) with
  completion criteria before implementation starts.
- Keep completed work summarized here; do not remove its rationale merely
  because its checklist item is complete.
- Keep design exploration separate from release commitments. A documented idea
  is not a promise to support it.
