# REX-M8.3 temporal rules contract

Status: implemented locally; review pending (2026-09-09)

## Decision

A condition leaf may declare `for` with a positive Go duration up to 365 days.
Its predicate must remain continuously true for that processing-time duration.
Any `for` selects execution contract v6; v6 may also carry typed fact declarations.
Source without temporal conditions retains its existing v4 or v5 artifact bytes.

The first true observation starts a private timer and the condition is unknown.
A later event affecting the rule observes the timer: before the deadline the
condition remains unknown, and at or after the exact deadline it is true. False,
missing, null, or invalid resets the timer. A later true observation starts a new
interval. REX does not schedule a wake-up at the deadline.

## Clock and ordering

V6 uses processing time supplied by an injected runtime clock. The coordinator
samples it once per chain, so all derived rounds share one instant. Offline
scenario events require an RFC 3339 `at` value in nondecreasing order. Production
uses the system clock unless the embedding application calls `Engine.SetClock`.
If the clock moves behind a persisted start, REX restarts that interval at the
new sample and keeps the condition unknown. Malformed private state remains an
evaluation error because it indicates storage corruption.

Producer event timestamps have no temporal meaning in v6. Event-time windows,
watermarks, allowed lateness, and late-event correction are unsupported. This
keeps replay and recovery unambiguous until an event-time contract is designed as
a separate versioned capability.

## State and failure boundaries

Each temporal leaf has one stable private key derived from the exact artifact,
rule index, and condition path. Its RFC 3339 start time is persisted through the
existing memory, Redis, or durable Redis commit domain. Reloading identical bytes
reconstructs the same keys; changing the artifact starts independent temporal
state for the new program. The `__rex_temporal_` prefix is reserved across every
source and external input path so producers cannot forge or shadow trackers.

Private writes are excluded from public snapshots, publications, durable output
streams, derived events, applied-output lists, and action identities. A commit
containing timer and action writes applies them together where the adapter already
offers atomicity. Non-durable Redis retains its documented sequential partial or
unknown commit behavior.

Durable processing stores the first clock sample in the event journal before
evaluation. Retries and restart reuse that instant along with the pinned program
and snapshot, so elapsed wall time during recovery cannot change the result.

Reset deletes its tracker instead of retaining a null key. Active timers have no
TTL because expiration would make a valid, delayed qualifying event restart an
interval instead of observing that its deadline passed. Artifact history retains
old program timers for rollback and durable recovery; when an artifact leaves
that retained set, the engine manager deletes its private keys through the store
adapter.

V6 evaluates every leaf of an affected condition tree, even after the Boolean
result is known when that leaf or subtree contains temporal state, so a hidden
temporal predicate cannot retain a stale timer. Unrelated sibling subtrees retain
normal short-circuit behavior. The final `all`/`any` truth value and action firing
rules are unchanged. This maintenance can evaluate more nodes than the equivalent
v4/v5 tree, and those nodes count against the normal chain-work budget.

The contract remains level-triggered after the deadline: every later qualifying
event fires the rule until the predicate resets. Consumers that need one external
notification per sustained interval must apply idempotency or wait for the
separate change-only emission capability.

## Bounds and compatibility

A program may contain at most 10,000 temporal leaves. Timers count toward snapshot,
staged-write, and existing program dependency budgets. V4 and v5 decoders reject
temporal payloads, and v6 rejects payloads without temporal conditions. The v3
compatibility compiler rejects `for`.

The durable processing-time guarantee lasts for the configured journal retention
window, like the pinned program and historical snapshot guarantees it extends.
`JournalTTL` must cover the maximum retry and recovery interval.
