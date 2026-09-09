# REX-M7 durable processing runbook

REX durable mode uses one Redis Stream consumer group as an ordered partition.
It preserves accepted inputs across a `rexd` restart, records input facts,
derived fact writes, durable output, and commit markers before acknowledging the
input, and bounds poison retries. Pub/Sub remains available as the default
best-effort mode.

## Supported deployment

Use Redis 6.2 or newer with a standalone primary commit domain. Enable AOF and
choose `appendfsync everysec` or `always` for the required host-loss budget.
Replication and `WAIT` can reduce data loss during failover, but this release
does not coordinate Redis Cluster, Active-Active Redis, or concurrent workers
for a shared fact partition.

One `rexd` instance obtains an expiring lease for each durable namespace. A
second instance fails startup while the lease is held. The consumer renews the
lease every third of `lock_ttl` and fails readiness and exits if renewal fails.
Choose `lock_ttl` longer than expected transient Redis latency.

Give every partition a unique namespace and three distinct input, output, and
dead-letter stream keys. Those streams and every key beginning with
`rex:durable:` are reserved and cannot be used as fact names or rule action
targets. Namespaces accept only 1-64 ASCII letters, digits, underscores, and
hyphens. A `RedisStore` instance owns one durable partition and rejects a second
open.

While durable mode is active, route every external fact mutation through the
input stream. Direct `SET` writes to REX-owned fact keys bypass ordering and
snapshot history and are outside the recovery contract.

Journal retention is the deduplication window. Set `journal_ttl` longer than the
maximum input retention and any replay window. Do not trim an input stream past
unread or pending entries. Apply producer backpressure from
`rex_event_queue_lag`; use Redis memory policy and monitoring that reject writes
before evicting stream or journal keys. Output and dead-letter streams use the
configured approximate maximum lengths.

## Configuration and cutover

Compile and validate a v4 or v5 batch artifact first. Durable mode rejects v3 artifacts.
Configure one producer and one consumer together:

```json
{
  "redis": {
    "event_mode": "streams",
    "durable": {
      "stream": "rex_events",
      "group": "rex",
      "consumer": "rexd-1",
      "output_stream": "rex_results_stream",
      "dead_letter_stream": "rex_dead_letter",
      "namespace": "production",
      "claim_idle": "30s",
      "block": "1s",
      "journal_ttl": "168h",
      "max_attempts": 5,
      "output_max_len": 100000,
      "dead_letter_max_len": 10000,
      "lock_ttl": "30s",
      "retry_backoff": "250ms"
    }
  }
}
```

Run a dry-run comparison against representative events before cutover. Record
the artifact digest, input stream tail ID, output stream tail ID, and current
fact snapshot. Use a fresh input stream because a newly created group begins at
its oldest entry. Stop the Pub/Sub producer, start the Streams consumer, then make
the producer append only to the input stream. Do not dual-publish the same
logical event to Pub/Sub and Streams.

An accepted event is a successful producer `XADD` response:

```sh
redis-cli XADD rex_events '*' payload '{"weather:temperature":30.5}'
```

Watch `/readyz` and these metrics during the drain:

- `rex_event_queue_lag`: entries not yet delivered to the group
- `rex_event_queue_pending`: delivered entries awaiting acknowledgement
- `rex_event_retries_total`: failed attempts left pending
- `rex_event_recoveries_total`: pending deliveries recovered by a worker
- `rex_dead_letters_total`: poison events removed from the ordered path
- `rex_event_failures_total` and `rex_event_processing_duration_seconds`

The worker samples consumer-group statistics at the Redis health-check interval
(one query per second by default), rather than once per event.

## Recovery checks

After an unclean stop, restart with the same artifact, stream, group, and
namespace. The consumer reads its own pending entry or claims the prior
consumer's entry after `claim_idle`. It reuses stored dependency snapshots and
commit markers. A round whose Redis transaction committed before the connection
failed returns its saved result without repeating its output.

Inspect state without changing it:

```sh
redis-cli XINFO GROUPS rex_events
redis-cli XPENDING rex_events rex
redis-cli XRANGE rex_results_stream - + COUNT 20
redis-cli XRANGE rex_dead_letter - + COUNT 20
```

If startup reports a program mismatch, restore the exact artifact named by the
journal error. Do not edit journal hashes. If the old artifact cannot be
restored, treat the pending event as an explicit migration and redrive it under
a new input identity after review.

## Poison repair and redrive

A failed event remains at the head of the ordered partition until
`max_attempts`. The final attempt atomically writes the original input ID,
payload, program digest, attempt count, error, and stable dead-letter ID, then
acknowledges the input.

Infrastructure failures such as Redis disconnects, uncertain transactions, or
an output key with the wrong Redis type are never classified as poison. They
fail readiness and keep the input pending until an operator restores the
dependency.

Repair the producer or rule data before redrive. Append a new event and carry
the old ID as provenance; never delete or rewrite the old journal. M7 uses this
audited manual procedure after validating the repaired payload with the normal
simulation tooling; a first-class redrive command is follow-up work:

```sh
redis-cli XADD rex_events '*' payload '{"weather:temperature":30.5}' original_input_id 'OLD-ID'
```

Record the returned new ID with the incident. External consumers of
`rex_results_stream` must deduplicate with the stable IDs in each output payload;
their side effects remain at least once unless the destination implements an
idempotency key.

## Drain and rollback

For a planned drain, stop the Streams producer, wait until group lag and pending
are both zero, record the final IDs and fact snapshot, then stop `rexd`. A
rollback to Pub/Sub starts a Pub/Sub consumer and producer only after that drain.
If pending work cannot be drained, leave the Streams deployment stopped with its
stream, group, journals, and artifact intact; repair and resume it before any
mode switch. This prevents forgotten pending work from later applying beside a
new processing path.
