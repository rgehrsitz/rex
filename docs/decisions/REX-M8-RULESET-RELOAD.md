# REX-M8.1 ruleset reload contract

Status: merged in PR #40 (`a001349`, 2026-09-09)

## Decision

`rexd` may poll its configured bytecode path and activate a changed artifact
without restarting. Reload is opt-in, accepts supported batch artifacts, and treats the
SHA-256 digest of the exact artifact bytes as the program identity.

The daemon reads, bounds, validates, loads, and applies all runtime limits and
observers to a candidate before publishing it. A failed candidate never changes
the active engine. The same rejected bytes are not revalidated on every poll;
changing the file creates a new attempt.

An `EngineManager` holds a read lock for one complete Pub/Sub or Streams event.
Activation takes its write lock, so an event finishes on its original engine
before new events can use the candidate. The manager retains engines by program
ID for durable recovery.

## Durable coordination

The M7 journal remains authoritative. Before beginning a recovered delivery,
the manager reads its recorded `program_id` and selects that engine. If the
artifact is unavailable, processing returns `ErrDurableProgramMismatch`; the
daemon becomes unready and leaves the event pending for operator repair.

The daemon also reads consumer-group statistics immediately before activation
and defers a reload while `Pending` is nonzero. This reduces version overlap and
makes normal rollout drain-first. A delivery can begin between this check and
the eventual swap, but the manager lock makes it finish before activation and
the archived engine remains available if it fails.

Undelivered stream lag does not block activation because those events have not
yet acquired a program. A backlog can therefore span a deployment: events
delivered before the swap use the old program and events first delivered after
it use the new one. Reload preserves per-event consistency, not one program for
an entire queued backlog.

## Artifact history

Reload requires a dedicated history directory and a positive maximum file
count. Valid artifacts are written atomically as `<program-id>.bytecode` before
activation. Existing history is validated against both its contents and file
name at startup, then registered for recovery. The active artifact is archived
at startup as well.

REX does not delete history automatically because it cannot prove that an
artifact is outside every Redis journal's recovery window. When the configured
limit is reached, the candidate is rejected and the active program continues.
Operators must retain each artifact for at least `redis.durable.journal_ttl`
after its last possible event and remove it only after checking pending work.
An empty `XPENDING` result alone is insufficient: journal entries and deliberate
redrives can outlive the pending list, so the full journal retention interval
must also have elapsed.
Each historical engine keeps its decoded program in memory, so the file-count
limit also bounds the reload feature's program-memory growth. After successful
activation, decoded engines whose files an operator removed are released.

Rollback uses the same path: restore archived bytes to `bytecode_file`. The
watcher validates and atomically activates that older program at the next safe
event boundary. Fact state is not reverted; a state rollback requires a
separate checkpoint/restore procedure under the M7 contract.

## Compatibility and limits

Reload is disabled by default and changes no v3 or existing daemon behavior.
V3 remains available only through its existing explicit compatibility switch
and cannot enable reload. Its legacy multi-fact dispatch therefore cannot be
swapped between fact updates by the daemon. Activation is process-local; a
multi-instance rollout must coordinate artifact publication and the existing
single-owner durable lease. Polling is portable across supported operating
systems and avoids signal-specific behavior.

## Acceptance evidence

Tests prove that a swap waits for an in-flight event, a pinned retry selects its
historical program, invalid candidates retain the active digest, valid candidates
are archived before activation, and pending durable work defers activation.
Runtime metrics use fixed names without program-ID labels to keep cardinality
bounded.
