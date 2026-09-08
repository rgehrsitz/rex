# REX-M4: versioned batch evaluation and commit contract

Status: merged in [PR #35](https://github.com/rgehrsitz/rex/pull/35), revision
`f6e036c`, 2026-09-08. This decision implements D1–D5 from the foundation
roadmap; M7 will add durable recovery, not redefine v4 condition or conflict
semantics.

## D1 — Snapshot and ownership

An immutable loaded v4 program owns only rules and indexes. Each round asks a
SnapshotReader once for the union of relevant dependencies not present in that
round's input, then overlays all input facts. Rules never read the adapter or
mutate that snapshot. The memory adapter snapshots under a lock; Redis uses one
MGET on a single standalone Redis server. This is a point-in-time read, not a
transaction across external producers or a replayable historical snapshot.
Producers own input persistence. Rex commits derived outputs only. External
writers must publish input events; M7 owns ordered history and recovery.

The engine retains no event facts in v4. Inspection uses returned round results;
the deprecated v3 compatibility engine exposes copies instead of Engine.Facts.

## D2 — Conflicts and ordering

Candidates are deduplicated and evaluated by ascending priority, with source
order for ties. Actions are staged in rule/action order. Two different values
for a target reject the round before commit. Identical scalar JSON values
coalesce to one write while preserving both action records. Earlier staged
writes never influence later conditions. Numeric JSON forms normalize through
float64, preserving the existing numeric precision contract.

## D3 — Three-valued conditions

Facts distinguish missing, explicit null, invalid stored JSON/non-scalar values,
and present scalar values. Comparisons with missing/null/invalid/wrong-type
values return Unknown, including NEQ. Event values must be finite JSON scalars
or null; invalid events fail before snapshot or commit. Only True fires.

| A | B | all(A,B) | any(A,B) |
| --- | --- | --- | --- |
| True | True | True | True |
| True | False | False | True |
| True | Unknown | Unknown | True |
| False | False | False | False |
| False | Unknown | False | Unknown |
| Unknown | Unknown | Unknown | Unknown |

The operations are commutative. A true any branch can fire despite a missing
other branch; v3 instead filtered the whole rule. The source schema retains
its existing scalar condition constants; no null-comparison operator is added.

## D4 — Commit and failure scope

Evaluation/cancellation/conflict/budget errors discard the current round's
staged writes. Each round is a distinct commit: earlier committed rounds remain
committed after a later error. Adapter results distinguish committed, not
committed, partial, and unknown. No automatic retries or compensating rollback
are allowed. Partial/unknown outcomes latch the coordinator as halted; operator
reconciliation and a new engine instance are required to resume. This latch is
in-memory, not crash recovery or deduplication.

Memory commits atomically under its lock. The standalone Redis adapter validates
and encodes before issuing sequential SETs without client retries. A command
error after dispatch is conservatively Unknown, with acknowledged prior writes
reported. Output notification failure after successful SETs is Partial. There
is no claim of a Redis transaction, durable publication, or cluster support.

## D5 — Derived rounds, transport and limits

Coalesced committed outputs feed the next local round of the same chain. Cycles
are allowed but bounded by rounds, rule evaluations, actions, unique writes,
and bytes; fan-out consumes the shared chain action/work budget. Defaults are
finite and configurable within hard ceilings. Every round validates its limits
before commit. A chain ID accompanies commit reports; the coordinator owns its
budgets for the complete synchronous call. Budgets do not survive restart.

Redis publishes completed round outputs as `_rex.kind = "committed_output"`
notifications on `rex_results`. v4 consumers ignore these notifications even if
subscribed to that channel: local derived processing already owns the chain.
They are observations, not new input events. Producers publish unmarked input
fact objects to the configured input channels. v3 consumers must not share this
output channel during migration. Pub/Sub remains best-effort and non-durable.

V4 artifacts contain a bounded, CRC-checked structured rule IR (JSON encoding)
with a new version/header; tree grouping is retained to represent Unknown
correctly. The old binary jump program remains v3 and never acquires v4 meaning.
The default compiler/daemon path is v4; v3 requires explicit CLI/config opt-in.
Scripts are rejected in v4. M6 subsequently removed them from every contract.
