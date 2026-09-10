# REX-M8.4 acceptance evidence

Date: 2026-09-09
Starting revision: `cdab686` (merged REX-M8.3, PR #42)
Branch: `codex/rex-m8-change-only`

## Delivered contract

- Rule-level `emit: "on_change"` selects execution contract v7 and suppresses an exact scalar write already present in the persisted target snapshot.
- V7 payloads carry canonical compiler-owned capabilities so change-only behavior composes with typed declarations and temporal conditions. V4, v5, and v6 payloads do not carry the field.
- Suppression occurs after conflict detection and identical-write coalescing. Mixed default/change-only writers retain the write. Repeated emission remains the default.
- Suppressed actions remain visible in traces, count against existing budgets, report the skipped observer outcome, and create no action identity, public write, notification, or derived event.
- Persisted target facts are the only activation state. Restart preserves suppression and external drift causes a corrective emission.

The final contract and rationale are in [the D11 decision](../../decisions/REX-M8-CHANGE-ONLY.md). The runnable source and replay fixtures are [change-only-rules.json](../../../examples/m8/change-only-rules.json) and [change-only-scenario.json](../../../examples/m8/change-only-scenario.json).

## Compatibility evidence

The existing frozen v4 SHA-256 fixture remains unchanged. New frozen v5 and v6 fixture hashes match artifacts compiled from the merged `cdab686` worktree byte for byte:

| Contract | SHA-256 |
| --- | --- |
| v5 example | `c6f81a7815fab30a29ca143a2732b7ed301fc4d82498c2476bd42e18231e6b8e` |
| v6 example | `62e72984a8511cdb9b8cd7851e68d7bd619c431ef8fb485050c327f620310c93` |

Compiler tests also freeze canonical minimal v5/v6 artifacts and reject missing, forged, or mismatched v7 capabilities, change-only payloads under earlier headers, and v7 headers without change-only behavior.

## Semantics and performance evidence

`internal/semantics` compares 256 deterministic generated combinations of present, missing, null, and invalid persisted facts across boolean, string, and number proposals against an independent exact-scalar oracle. Runtime tests cover event-overlay isolation, equal/different values, mixed writers, conflicts, coalescing, default repeated emission, restart, external drift, skipped outcomes, zero-write termination, durable completion, and v7 temporal composition.

Apple M4, Go 1.26.6, three 500 ms samples:

| Path | Time | Allocations |
| --- | ---: | ---: |
| V4 default | 1.598–1.643 µs/op | 1,024 B/op, 17 allocs/op |
| V7 write | 2.221–2.352 µs/op | 1,472 B/op, 24 allocs/op |
| V7 suppress | 2.209–2.393 µs/op | 1,472 B/op, 24 allocs/op |

This isolated evaluator benchmark confirms that v4-v6 avoid the new persisted-target allocation and that suppression adds no allocation cost over the v7 write path. It does not include adapter latency; live change-only rounds add target snapshot reads and avoid equal write/publication work.

`rex_actions_skipped_total` and the corresponding labeled action-outcome series
now count change-only suppression. The current aggregate observer contract has
no rule or target dimension; use structured traces for attribution.

## Validation

Completed after consultant review and fixes:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`
- `./scripts/test-m5-cli.sh`
- `govulncheck ./...`: no reachable vulnerabilities; one imported-package and one required-module finding are unreachable
- six release archive cross-builds with generated SHA-256 checksums
- v7 validate, bundle, and simulate example smoke test
- ten-second v7-seeded batch decoder fuzz run: 92,930 executions, one new
  coverage-increasing input, no failure

Claude reviewed the complete uncommitted diff twice. The first pass found no core
correctness defect and identified replay ambiguity, validation ordering, legacy
allocation cost, suppression complexity, and coverage/documentation gaps. The
implementation now rejects ambiguous replay events, reuses bounded shape parsing,
avoids all v4-v6 suppression bookkeeping, marks suppression in linear time, and
adds missing-target, Redis, durable retry, temporal composition, and compatibility
coverage. The second pass confirmed those fixes and found two low-severity items:
legacy contracts still performed CPU-only suppression bookkeeping and signed zero
was unstated. Both were fixed before the final test and race runs.
