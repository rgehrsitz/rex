# REX-M5 authoring tools

All commands below run locally without Redis. Build `rexc` and `rexd` from this
revision; the tools support batch execution contracts v4, v5, v6, and v7. The legacy `-legacy-v3`
compiler remains available but v3 replay/jump disassembly is not supported by
these tools. Batch explain describes structured IR: v4/v5/v6/v7 have no bytecode jumps.

## Explain and compile

```sh
go build -o /tmp/rexc ./cmd/rexc
go build -o /tmp/rexd ./cmd/rexd
/tmp/rexc -rules examples/m5/rules.json -output /tmp/m5.bytecode
/tmp/rexc explain -artifact /tmp/m5.bytecode
/tmp/rexc explain -rules examples/m5/rules.json
```

Compilation writes `/tmp/m5.bytecode.manifest.json` with schema/execution
versions, source SHA-256, artifact SHA-256 and artifact length. Keep source,
artifact and sidecar together. The sidecar is provenance, not a signature or
runtime authorization mechanism. Failed sidecar writes fail compilation but
may leave the already written artifact; do not deploy partial output sets.
Validation-only and explicit legacy compilation do not emit sidecars.

Explain lists rules in priority/source order with source indices, dependencies,
condition trees and action metadata. Replay reports add `rule_results` for each
affected rule: `true`, `false`, or `unknown`; only true fires. A rule absent from
a round's results was not affected by that round's input. Condition traces show
missing, null, invalid, and present values' states without embedding the values.
Short-circuited conditions are omitted in v4/v5. After an otherwise decisive
v6 result, evaluation continues only through sibling leaves or subtrees that
contain temporal state, while preserving the same truth table. Failed evaluations
discard staged output but preserve condition/rule diagnostics
and an `evaluation_error` on the failed round. Earlier committed rounds remain
visible. Use the static explanation alongside that error to inspect its source.

## Named scenarios, replay and comparison

```sh
/tmp/rexc test -rules examples/m5/rules.json -scenario examples/m5/suite.json
/tmp/rexc bundle -rules examples/m5/rules.json -scenario examples/m5/scenario.json > /tmp/m5-bundle.json
/tmp/rexc simulate -bundle /tmp/m5-bundle.json > /tmp/m5-report.json
/tmp/rexc compare -bundle /tmp/m5-bundle.json -rules examples/m5/candidate.json
/tmp/rexd --dry-run --bundle /tmp/m5-bundle.json
```

`--dry-run` must be the first daemon argument. It accepts only `--bundle` and
runs the same offline replay path before any daemon configuration, Redis client,
subscription, observability listener or external committer is created. Proposed
writes are applied only to private simulation memory so subsequent rounds can
be evaluated. These are memory-adapter outcomes; Redis partial commits, transport
failures, and output envelope rejection cannot be predicted by an offline replay.
This is not a live dry-run against today's Redis state.

Scenario schema 1 requires `name`, an explicit `initial_state` object, and ordered
`events` with unique nonempty `id` and scalar `facts` objects. An absent key in
that complete initial state means missing; null and non-scalar initial JSON values
remain null/invalid snapshot diagnostics. Producers' inputs are persisted in
simulation memory before each chain, matching M4. `limits` may be omitted when
creating a bundle; default v4 limits are then materialized. A replay bundle must
include all limits, the exact source text, and matching program/source digests.
No timestamps or random IDs are generated. Each v6 event requires an RFC 3339
`at` processing time, in nondecreasing order; replay injects it into the runtime.
V4/v5 reject `at`, and unsupported fields/contracts fail rather than substituting
nondeterministic data.

A suite contains `schema_version: 1` and 1–100 uniquely named `scenarios`. Each
scenario needs an `expect` object with exact `final_state` and ordered `actions`
(rule/target/value) and optionally `error_contains`. Actions are proposals retained
in successful round evaluations, including identical coalesced writes. Evaluation
errors discard that round's proposals. Replay stops at the first failing chain;
earlier virtual commits and persisted inputs remain visible.

Comparison runs both sources independently against the same initial state,
ordered inputs and limits. It reports changes in actions, final state or per-event
traces, and includes both full traces as causal evidence. Unaffected source changes
may produce no behavior difference. A matching replay is evidence for that bundle,
not proof that arbitrary future inputs are equivalent.

Limits: documents/output 32 MiB, initial/retained simulation state 4 MiB and
65,536 keys, at most 1,000 events per scenario, plus the existing v4 program and
per-chain bounds. Reports accumulate only within those limits. JSON is indented
and deterministically ordered, with a schema version and no machine-local paths
or wall-clock fields. Evaluation errors are human diagnostics, not stable IDs;
use lint IDs and structured result fields for machine decisions.

## Lint

```sh
/tmp/rexc lint -rules examples/m5/rules.json -channels rex_updates
```

| ID | Meaning | Severity |
| --- | --- | --- |
| REX-L001 | Invalid source or removed capability | Error |
| REX-L002 | Different writers to one target; simultaneous matches could conflict | Warning |
| REX-L003 | A sole EQ condition is reproduced by its self-write, proving recurrence if reached without conflict | Warning |
| REX-L004 | Undefined script reference | Error |
| REX-L005 | Results channel used as input, or an empty input channel | Warning / error |

Lint is conservative: it does not prove arbitrary condition overlap or discover
all multi-rule cycles. Budgets remain the runtime safeguard. Arbitrary fact keys
are valid in v4, so lint does not require the legacy `group:key` convention.
Scripts are removed even when their references are defined. REX-L004 remains a
specific migration diagnostic for an undefined brace-form reference.

Exit status is 0 for successful commands and lint warnings, 1 for global input,
unsupported-contract or I/O failures, and 2 for per-scenario validation errors,
scenario mismatches, replay execution errors, lint errors, or comparison
differences. `rexc test` continues after a malformed scenario and includes its
validation error in the JSON report. Tool commands emit one JSON document on
stdout and diagnostics on stderr. Redirect stdout to save it. Each command's
help lists only its accepted flags; unknown flags are rejected.

Schema-1 result fields use snake_case. M5 adds JSON tags to the M4 `Budget`
type, so serialized chain results now use `budget.actions` and `budget.work`
instead of Go's default `Actions` and `Work`. This corrects the new tooling
contract before integration; Go field names and evaluation semantics are
unchanged.

## Verification

`./scripts/test-m5-cli.sh` builds the binaries and exercises every documented
command without Redis, compares repeated replay and dry-run bytes, and verifies
the comparison exit code. CI runs it alongside normal race/build checks.

`TestV4SchemaParserAgreement` uses the published draft-07 schema and a pinned,
test-only JSON Schema validator against shared positive/negative source shapes.
The parser additionally enforces UTF-8 byte limits, unique rule names, total node/
dependency limits and nesting bounds, which the generic schema does not express.
