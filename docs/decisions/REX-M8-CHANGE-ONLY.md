# REX-M8.4 change-only emission contract

Status: implemented locally and consultant-reviewed; PR review pending (2026-09-09)

## Decision

A rule may set `"emit": "on_change"`. When that rule matches, REX evaluates and traces every authored action normally, then suppresses a target write if its scalar value exactly equals the target's persisted fact at the start of the round. Missing, null, invalid, differently typed, or unequal target facts cause a write. Rules without `emit` keep the existing level-triggered behavior and may deliberately repeat equal writes.

Change-only source compiles as execution contract v7. The canonical v7 payload has a compiler-owned `capabilities` array. It always contains `change_only` and also contains `temporal` and `typed_facts` when those features are present. Source cannot declare this metadata. The decoder requires the array to match the payload's observed features exactly, which prevents version-number feature gates from silently dropping composed behavior. V4, v5, and v6 artifact bytes and meaning remain unchanged.

## Snapshot and conflict semantics

The comparison uses the persisted snapshot, never the incoming event overlay or a private in-memory activation bit. Action targets therefore become snapshot dependencies for change-only rules, including when the event contains the same key. This makes restart and reload behavior derive from authoritative stored facts, and an external change to a target causes the next matching rule to restore its declared value.

REX first applies the existing simultaneous-round conflict and coalescing rules. Different values for one target still reject the round, even if one would compare equal to persisted state. Identical proposals coalesce to one write. That write is suppressed only when every matching writer for the target is change-only; the presence of any default writer preserves it.

Equality is exact over the supported scalar representation: finite JSON numbers compare as `float64`, strings by bytes, and booleans by value. IEEE floating-point equality treats `-0.0` and `0.0` as equal. No type coercion occurs. Typed stored values that violate their declaration normalize to invalid and therefore do not suppress a corrective write.

## Traces, budgets, and durable processing

A suppressed action remains in `Evaluation.Actions` with `suppressed: true`, and its rule remains in the fired-rule list. Runtime observers record it as `ActionSkipped`; it produces no action identity, public write, notification, or derived event. Authored actions consume the existing action, work, and staged-byte budgets before suppression so opt-in emission cannot bypass resource limits.

When a round contains no public or private writes after suppression, the coordinator records the evaluation and ends the chain without calling the committer. Durable processing still completes and acknowledges the input through the journal protocol. If a temporal v7 rule stages private timer maintenance, that internal write still commits even when its public action is suppressed.

Offline replay treats omission from `initial_state` as an explicitly missing persisted fact. The first matching event then emits. Include the target with its current value when testing suppression. Replay persists event inputs before evaluation, as durable processing does, and therefore rejects an event that also contains a change-only target because that event cannot represent the prior target snapshot unambiguously. Within that boundary, replay, comparison, scenario expectations, and dry-run expose the same `suppressed` marker as the production evaluator.

An all-suppressed durable retry needs no separate output commit marker. Suppression
is a pure function of journaled input plus persisted snapshot state, so evaluating
the same unacknowledged input again reaches the same no-write result before the
journal completes it.

## Bounds and compatibility

Change-only action targets count as dependencies and against the existing 65,536 dependency and snapshot-byte limits. Suppressed proposals count against action and staged-byte limits. The legacy v3 compiler rejects `emit` because it cannot represent these semantics.

Rollback uses normal M8.1 artifact activation. Restoring v4, v5, or v6 bytes restores repeated emission for subsequent events; no private change-only state needs migration or cleanup. Adding `emit` to a temporal rule changes its artifact digest and therefore restarts that rule's current `for` window under new private tracker keys; retained-program cleanup retires the prior keys normally.
