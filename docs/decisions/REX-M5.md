# REX-M5 — Offline authoring and replay contract

Status: complete, merged in PR #36 at
`039f5c649448fc2261239bade97bde250cb497f3`. Depends on M4, merged in PR #35
at `f6e036c765d57c6c352617831dfb5052c86cae2e`.

Tool output and replay formats have schema version 1, independently of execution
contract 4. Only v4 is executable by these offline tools. V3 remains available
through the legacy compiler/runtime; unsupported tooling contracts fail explicitly.
V4 contains structured condition IR, not jump instructions. Explain reports that
representation together with priority/source order, dependencies and actions.

A replay bundle embeds exact source bytes, source/artifact SHA-256 digests,
explicit limits, a complete initial JSON state and ordered named inputs. Absent
keys in that complete state mean missing; null and invalid JSON-shaped values
retain their distinct snapshot meaning. Input values must be v4 scalars. Inputs
are persisted into private simulation memory before each chain, matching the
M4 producer contract. All derived commits stay in that private memory. No tool
constructs a Redis adapter. There are no clocks/functions in v4; bundles reject
unsupported execution versions instead of inventing historical results.

Reports include ordered chains, condition/rule results, actions, round errors,
and final state. Chain IDs come from unique input IDs rather than randomness.
Simulation stops at its first evaluation failure; earlier virtual commits remain
visible. Comparison rebuilds only the candidate program and runs the same state,
inputs and limits independently. It reports action/fact/result differences and
condition traces as causal evidence, without claiming complete counterfactual
proof. Recorded digests are integrity/provenance checks, not signatures.

CLI exit codes: 0 success (including lint warnings), 1 global invalid input,
unsupported capability or I/O failure, and 2 per-scenario validation/mismatch,
replay evaluation error, lint error, or comparison difference. A test suite
collects scenario-local failures rather than aborting the report. Machine-readable
stdout contains one JSON document; diagnostics go to stderr. Output is
deterministic and excludes wall time, random IDs, absolute paths and timestamps.

`rexd --dry-run --bundle ...` is deliberately offline. It intercepts execution
before configuration, store construction, subscriptions or metrics startup. This
is simulated execution against supplied historical state, not a live Redis peek.

Lint uses stable diagnostic IDs. Potential conflicts/cycles are warnings unless
proven for a supported simple case. Undefined script references are errors, and
scripts are unavailable regardless. Routing checks cover results-channel
subscriptions; arbitrary fact keys remain legal in v4. Schema/parser agreement
is checked against a shared corpus in CI, with explicitly documented semantic
checks beyond standard JSON Schema (UTF-8 byte limits and unique names).
