# REX-M8.2 typed fact contract

Status: implemented locally; review pending (2026-09-09)

## Decision

A ruleset may declare an optional top-level `facts` map. Its presence opts the
ruleset into execution contract v5 and makes the fact namespace closed. Each
condition fact, input fact, initial-state fact, and action target must have one
declaration. Supported scalar types are `number`, `string`, and `boolean`.
Declarations may set `nullable` to true; it defaults to false.

Rulesets without `facts` retain the v4 contract. Their canonical payload and
artifact bytes are unchanged. A v4 artifact containing declarations and a v5
artifact without declarations are invalid, so the artifact header unambiguously
identifies the contract.

## Fact states and numbers

A declared fact may be missing. Missing remains a first-class state, comparisons
produce unknown, and a rule fires only when its complete condition is true.
Explicit JSON null is accepted only for a nullable fact and also compares as
unknown. Null action values remain unsupported.

Inputs with an undeclared name, wrong scalar type, or disallowed null are
rejected before state persistence or rule evaluation. Existing stored state may
predate a declaration. When a snapshot contains a wrong type or disallowed
null, the evaluator marks that fact invalid and its comparisons produce unknown;
it does not silently coerce or overwrite state.

`number` uses the existing JSON and Go `float64` representation. Inputs and
constants must be finite JSON numbers. V5 does not add decimal, integer, or
arbitrary-precision semantics.

## Compiler and runtime boundaries

The compiler validates declaration shape, all condition constants, and every
action value. The runtime repeats validation for external input and replay
initial state. Derived events contain compiler-validated action values and are
also checked before each subsequent round.

The v3 compatibility compiler rejects typed declarations, including validate-only
CLI use. Loaders, reload history, explanation, simulation, and durable processing
accept both v4 and v5. Durable processing validates decoded input before the
queue's `ApplyInput` persistence boundary. A typed validation failure is a
poison event: it consumes the configured bounded durable attempts, remains out
of fact state, and then moves to the dead-letter stream for operator review.

## Compatibility and rollout

Adding `facts` is an intentional contract migration because producers can no
longer send arbitrary names or scalar types. Operators can deploy v5 with the
M8.1 atomic reload flow. Old artifacts remain usable and historical v4/v5
artifacts remain selectable for M7 retries.
