# REX-M7 durable processing protocol

Status: accepted for implementation on 2026-09-08. This resolves D7 in the
foundation roadmap.

## Topology and ordering

The first durable mode supports Redis 6.2 or newer on one standalone primary
commit domain. Redis Cluster, Active-Active, and concurrent REX workers for one
partition are outside this contract because an event can read and write facts
with unrelated keys. One configured input stream and consumer group form one
ordered partition, and one `rexd` process owns it at a time. Pub/Sub remains a
separate, explicitly best-effort mode; a deployment must not feed the same
logical inputs to both modes.

The owner is enforced by an expiring, compare-and-renew Redis lease scoped to
the configured durable namespace. Losing the lease fails readiness and stops
the worker. Each partition therefore needs a unique namespace.

Producers append one `payload` field containing the normal REX fact-event JSON.
The Redis stream ID is the stable input identity. Redis acceptance is the
successful `XADD` response, subject to the configured Redis persistence and
replication policy. Consumer-group delivery is at least once: `XACK` happens
only after REX records terminal success or atomically moves a poison event to
the dead-letter stream.

On startup, the owner consumes its own pending entries first, then uses
`XAUTOCLAIM` to recover sufficiently idle entries from a prior owner, and reads
new entries only while the group has no older pending work. The worker processes
one event at a time. This preserves partition order instead of treating a pool
of consumers as state-safe concurrency.

## Journal and historical snapshots

Each input ID has a journal hash containing its original payload, pinned
program digest, attempt count, per-round dependency snapshots, per-round commit
records, and terminal state. The first read of a round stores the complete
dependency snapshot with `HSETNX`; a retry uses that stored snapshot instead of
fetching newer fact values. The input payload remains in the stream and is also
checked against the journal so identity cannot be reused with different data.

A retry requires the same program digest. If that artifact is unavailable, the
event remains pending and readiness fails until the operator restores the
artifact or deliberately redrives the event under a new identity. Silent replay
under different rule semantics is forbidden.

After decoding, the worker atomically writes the event's input facts and an
input commit marker before evaluation. Retrying the input reuses that marker.
This makes the stream the ordered ingress for authoritative external changes;
writers must not mutate REX-owned fact keys outside this protocol while durable
mode is active.

Input, output, and dead-letter stream keys plus the `rex:durable:` keyspace are
reserved. An input fact or rule action that targets one of those keys is
rejected as poison before it can change protocol state.

## Atomic state, output, and deduplication

Each evaluation round supplies stable action identities derived from input ID,
program digest, round, rule identity, and source action index. Stable write
identities additionally include the target fact. The Redis adapter validates
all values before starting a transaction, then atomically performs:

1. every authoritative fact `SET` for the round;
2. one `XADD` to the configured durable output stream containing the committed
   fact envelope and stable action/write IDs; and
3. the round commit marker in the event journal.

The adapter watches the journal marker and retries only transactions aborted by
optimistic contention. A lost `EXEC` reply is an unknown outcome, so the input
is left pending. Before any later retry, the adapter reads the marker: if it is
present, the round is reported committed without repeating facts or output; if
absent after Redis is reachable, the transaction can be attempted safely.
Pipelining alone is never treated as a commit protocol.

After a no-output terminal round, terminal journal state and `XACK` are written
in one transaction. Repeating that operation is safe. Journal expiry is the
deduplication retention boundary and must exceed the maximum input replay and
stream-retention interval. Trimming input history before its journal expires is
allowed; expiring the journal while an input can still be replayed is not.

## Retries, poison events, and dead letters

Every delivery increments the journal attempt counter. Processing failures leave
the event pending. Once attempts reach the configured maximum, a transaction
adds one dead-letter entry containing the original stream ID, payload, program
digest, error, attempts, and a stable dead-letter ID; records terminal
dead-letter state; and acknowledges the input. The marker makes a lost reply
recoverable without creating a second dead-letter record. Dead-lettering lets
the ordered partition continue. Redrive requires an explicit operator command
that appends a new input event and records the original ID; it never deletes or
rewrites history.

Redis connectivity, transaction uncertainty, unsafe key types, and other
storage failures are infrastructure failures rather than poison input. They fail
readiness and remain pending without dead-lettering, even after the configured
attempt count. This prevents an outage or misconfiguration from discarding
accepted work.

## Persistence, retention, and failure boundary

For process and network failures, recovery relies on stream PEL state and
journal markers. Redis host-loss durability is only as strong as Redis itself.
Production deployments should enable AOF and choose `appendfsync everysec` or
`always` according to their loss budget, maintain backups, and use replication
where required. `WAIT` can reduce replication-loss risk but does not make Redis
strongly consistent during failover. REX does not claim survival beyond the
configured Redis persistence/replication boundary.

Input, output, dead-letter, and journal retention are operator settings. The
worker bounds read count to one, blocks for a finite interval, caps attempts,
uses an idle-claim threshold, and exports pending count, lag, retries,
dead-letter count, and processing failures. Readiness requires Redis access,
partition ownership, and the ability to make progress; liveness remains
process-only.

The protocol follows the Redis guarantees documented for
[Streams and consumer groups](https://redis.io/docs/latest/develop/data-types/streams/),
[transactions](https://redis.io/docs/latest/develop/using-commands/transactions/),
and [persistence](https://redis.io/docs/latest/operate/oss_and_stack/management/persistence/).
