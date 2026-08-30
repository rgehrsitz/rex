# Engine semantics audit — 2026-08-30

This is the master, evidence-backed backlog for Rex. It consolidates the
revival reviews, the PulsarSuite comparison, and a line-by-line verification of
the Claude review artifact dated 2026-08-29.

Use [EVOLUTION_REFERENCE.md](EVOLUTION_REFERENCE.md) for long-lived product
reasoning and [REVIVAL_PLAN.md](REVIVAL_PLAN.md) for a short, committed
milestone checklist. This audit is the source of truth for findings that must
be triaged before feature work.

## Verification method

The following direct probe was run on `main` at `91654b6` with Go `1.26.6`:

- compile deliberately malformed and valid rulesets with `compiler.Parse` and
  `rexc`'s bytecode writer;
- load them through `runtime.NewEngineFromFile`;
- execute them against a temporary in-memory `ContextStore`;
- observe emitted actions and returned errors.

The relevant baseline checks previously passed: `go test ./...`,
`go test -race ./...`, `go vet ./...`, `go build ./...`, and `go mod tidy
-diff`. The audit did not change production code.

## Finding status

| ID | Finding | Status | Priority |
| --- | --- | --- | --- |
| REX-001 | Missing dependencies mutate the fact-to-rule index | Fixed and verified 2026-08-30 | P0 |
| REX-002 | Hybrid leaf/group conditions are accepted and partially discarded | Confirmed by execution | P0 |
| REX-003 | A group containing both `all` and `any` silently ignores `any` | Confirmed by execution | P0 |
| REX-004 | `sendMessage` compiles but fails at runtime and aborts the event | Confirmed by execution | P0 |
| REX-005 | Duplicate rule names compile but bytecode load rejects them | Confirmed by execution | P0 |
| REX-006 | Script timeout does not stop JavaScript execution | Confirmed by code inspection | P1, if scripts enabled |
| REX-007 | Priority has no execution-order effect; documentation disagrees | Confirmed by execution and inspection | P1 |
| REX-008 | Bytecode jump targets are not semantically validated | Confirmed by execution | P1 |
| REX-009 | Actions perform an unnecessary post-write Redis `GET` | Confirmed by execution and inspection | P2 performance |
| REX-010 | Redis startup exits through the logger instead of returning an error | Confirmed by inspection | P1 |
| REX-011 | TLS and environment-based Redis credentials are unsupported | Confirmed by inspection | P1 for managed Redis |
| REX-012 | Local facts are unbounded and channel routing is convention-only | Confirmed by inspection | P2 / design decision |
| REX-013 | CodeQL and Dependabot housekeeping are incomplete | Confirmed | P2 |

### Important dialect clarification

REX-002 and REX-003 are **not valid according to the checked-in JSON Schema**:
it requires exactly one of `all` or `any`, and each child must be exactly a
leaf or exactly a nested group. They are nevertheless accepted by the actual
`compiler.Parse` path used by `rexc`. Therefore these are validation defects,
not supported-but-ambiguous language features. The correct small fix is to
reject them; do not invent meanings for invalid input.

## P0 — correctness before new features

### REX-001: shared candidate-index mutation

**Root cause:** `ProcessFactUpdateContext` takes a slice directly from
`factRuleIndex` and removes missing-dependency candidates with in-place
`append`. The slice shares the index's backing array.

**Reproduction:** three rules listen to `a`; `r1` also needs absent `c`.
The first update correctly runs `r2` and `r3`. After `c` is present, the next
update runs `r2`, `r3`, `r3`; `r1` never runs. The long-lived index has become
`[r2, r3, r3]`.

**Resolution (2026-08-30):** `ProcessFactUpdateContext` now copies the indexed
candidate slice before applying transient missing-dependency filtering. The
regression test runs both rounds, asserts that the index remains unchanged,
and verifies that each eligible rule fires exactly once after the dependency
appears. The complete normal and race-enabled test suites pass.

Relevant code: [engine.go](../pkg/runtime/engine.go) (`ProcessFactUpdateContext`).

### REX-002: hybrid leaf/group node is silently compiled as a leaf

**Root cause:** parser validation permits a condition object containing
`fact`/`operator`/`value` and `all` or `any`. Conversion branches solely on
`item.Fact != ""` and drops children.

**Reproduction:** a leaf `a > 10` also containing nested `any: [b < 3]`
fires for `a=20, b=99`, even though the nested group is false.

**Required work:** enforce a discriminated condition shape: either a leaf with
only `fact`, `operator`, and `value`, or a group with exactly one of `all` and
`any`. Reject unknown/mixed fields. Add parser and compiler/runtime tests.

Relevant code: [parser.go](../pkg/compiler/parser.go),
[traverse.go](../pkg/compiler/traverse.go).

### REX-003: top-level `all` plus `any` drops `any`

**Root cause:** the IR can hold both, but `traverse` is `if All ... else if
Any`, so only `all` is emitted.

**Reproduction:** `{all: [a > 10], any: [b < 3]}` fires for `a=20, b=99`.
The `any` condition is false but has no effect.

**Required work:** reject the shape with the same discriminator validation as
REX-002. This matches the published JSON Schema and avoids a backward-
incompatible new conjunction meaning.

### REX-004: `sendMessage` is an accepted but unimplemented action

**Root cause:** compiler validation accepts `sendMessage`; runtime action
dispatch supports only `updateStore`.

**Reproduction:** a matching `sendMessage` rule returns `Unknown action type
encountered`; the outer processing loop returns immediately, so a later valid
rule for the same fact update does not run.

**Required work:** for the next release, reject `sendMessage` at compilation.
Do not add a messaging adapter merely to make the validator honest. Separately
make and document a deliberate action-failure policy: fail-fast for the event,
or record-and-continue per rule/action. Add a test covering the chosen policy.

### REX-005: duplicate names fail at deployment rather than compilation

**Root cause:** the parser does not track names. The runtime bytecode decoder
correctly rejects duplicate rule-execution-index entries.

**Reproduction:** `rexc` produces a bytecode file for two rules named `same`;
`rexd` refuses to load it.

**Required work:** reject duplicate names during parsing with a source-located
diagnostic. Add a compiler/runtime consistency test so known invalid artifacts
cannot be generated by the compiler.

## P1 — settle the runtime contract

### REX-006: scripts are trusted-only, not time-limited or isolated

`SafeVM.RunScript` creates an Otto interrupt channel but never sends an
interrupt. Its timeout returns to the caller while an infinite script continues
running. The VM and script map are shared mutable state, and the timed-out
goroutine can remain blocked on an unbuffered result channel.

**Required work:** retain the existing default of scripts disabled. Before
scripts can be enabled for anything beyond controlled trusted input, choose
one: isolated process execution with hard CPU/memory/time limits, or remove
scripting. Do not describe the current implementation as a sandbox.

### REX-007: priority is a nonfunctional ordering feature

Candidate rules run in source/index order. A direct probe placed priority 10
before priority 1 and observed that order of actions. `priority_threshold`
only controls a high-priority log message; it does not select or order rules.
The README and JSON Schema claim priority defaults to 10 and affect ordering,
while an omitted Go `int` priority is zero.

**Required work:** either sort candidates by priority with a stable tie-breaker
and test it, or remove priority from the accepted language. Correct the README,
schema, examples, and config documentation either way.

### REX-008: structurally valid bytecode can have invalid jump semantics

The bytecode decoder checks instruction framing but not that jump targets land
on instruction boundaries. A probe changed a valid artifact's `JUMP_IF_FALSE`
offset to zero, recomputed its CRC-32, and the runtime loaded it and executed
an action despite the false condition.

**Required work:** while validating bytecode, record every instruction boundary
and verify every jump target is in range and targets a valid instruction
boundary. Treat CRC-32 only as accidental-corruption detection, as already
documented; it is not authenticity protection.

### REX-010: startup failure must be returned, not fatal-exited

`NewRedisStore` calls `logging.Logger.Fatal` on a failed Ping. This bypasses
the caller's normal error path and defers.

**Required work:** return `(*RedisStore, error)` from construction, update the
factory interface, and add a connection-failure test.

### REX-011: managed Redis configuration is incomplete

The Redis client has no TLS configuration, and daemon configuration does not
enable Viper environment overrides. Passwords therefore require the config
file. This does not prevent all managed deployments, but it prevents services
that require TLS and makes secret injection unnecessarily difficult.

**Required work:** add a documented TLS mode and environment-variable mapping
for Redis credentials; test configuration parsing without exposing secrets in
logs.

### Delivery and event semantics decision

Before adding external side effects, decide whether Redis Pub/Sub remains an
explicit best-effort transport or whether Rex adopts a durable, at-least-once
transport such as Redis Streams. Then define whether a multi-fact JSON event
is an atomic batch. The recommended model is snapshot reads, staged writes,
and one coherent evaluation per event batch.

## P2 — performance, operability, and hygiene

### REX-009: post-action verification read

After every `SetAndPublishFactContext`, the engine performs `GetFactContext`
only to write a debug log. A direct probe counted one `GET` for each action.
The store also emits a standard-library `log.Printf` on every publish,
bypassing configured structured logging.

**Required work:** remove the verification read, route publish diagnostics
through zerolog at debug level, and benchmark before/after. A Redis pipeline
may reduce round trips, but it must not be presented as a delivery guarantee.

### REX-012: memory and routing boundaries

- `Engine.Facts` grows for process lifetime. Document fixed-cardinality facts
  as the current assumption, or design bounded/cacheable state before dynamic
  fact names are supported.
- Derived updates route to `strings.Split(key, ":")[0]`; the parser does not
  enforce the documented `group:key` form and configuration may omit that
  channel. Add validation/linting and an explicit routing contract.

### REX-013: small repository maintenance

- Update CodeQL from `actions/checkout@v4` to the currently used v7.
- Resolve Dependabot PRs #8 and #9 intentionally; do not leave them as stale
  duplicate work.
- Rebuild or pin the local `govulncheck` tool with Go 1.26 so local scans match
  CI.

## Semantics safety net (after P0)

These improvements would have made the P0 issues hard to introduce:

1. Add `rexc explain` to disassemble rules, offsets, resolved jumps, and
   indexes.
2. Keep deterministic disassembly golden files for a corpus of valid rules.
3. Add a slow AST reference interpreter and differential/property tests
   comparing it with bytecode execution.
4. Add a first-class in-memory store and declarative scenario tests, then
   expose `rexc simulate` and `rexc test`.
5. Turn the existing rules JSON Schema into an enforced compiler/CI contract
   with source-located diagnostics.
6. Add `rexd --dry-run` and per-rule metrics after semantics are stable.

## Explicitly deferred

Do not start these before P0 and the P1 contract choices are complete:

- new side-effect actions (webhooks, queues, notifications);
- Redis Streams, NATS, or another transport adapter;
- concurrent event evaluation;
- temporal rules, expressions, schemas/catalogs, or a new YAML dialect;
- enabling scripts beyond trusted controlled deployments.

## Suggested execution order

1. ~~REX-001 as a small isolated bug-fix PR with a failing regression test.~~
   Completed and verified 2026-08-30.
2. REX-002 through REX-005 as the next compiler-truthfulness milestone; keep
   the language restrictive and reject unsupported shapes/features.
3. REX-008 and REX-007 as bytecode/language-contract PRs, each with updated
   documentation.
4. Decide script and delivery semantics; complete REX-006, REX-010, and
   REX-011 according to that decision.
5. Address REX-009, REX-012, and REX-013, then add the semantics safety net.

Every implementation PR must add a regression test that fails on the audited
revision and passes with the change. Do not combine these correctness fixes
with bytecode-format revisions, dependency bumps, or unrelated features.
