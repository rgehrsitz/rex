# Ruleset reload and rollback

Ruleset reload is disabled unless `engine.reload.interval` is positive. A
minimal configuration is:

```json
{
  "bytecode_file": "/etc/rex/current.bytecode",
  "engine": {
    "reload": {
      "interval": "2s",
      "history_dir": "/var/lib/rex/programs",
      "history_max_files": 32
    }
  }
}
```

The history directory must be durable across process replacement. Size
`history_max_files` for every deployment retained during at least
`redis.durable.journal_ttl`; REX refuses a new activation when the directory is
full rather than deleting recovery evidence.

## Deploy a ruleset

Compile and validate a supported v4 or v5 batch artifact with the existing M5 tooling. Publish it
with an atomic rename in the same filesystem as `bytecode_file`:

```sh
cp next.bytecode /etc/rex/.current.bytecode.next
mv /etc/rex/.current.bytecode.next /etc/rex/current.bytecode
```

The daemon loads the complete candidate, applies configured limits, archives
its exact bytes, and activates it between events. In Streams mode, activation
waits until the consumer group has no pending deliveries. Undelivered lag does
not block activation; those events have not acquired a program yet.
Consequently, a nonzero lag can span program versions across consecutive events;
each individual event still uses exactly one program.

Watch the log for `Ruleset reload completed` and the new `program_id`. Monitor:

- `rex_ruleset_reloads_total`
- `rex_ruleset_reload_failures_total`
- `rex_ruleset_reload_deferred_total`
- `rex_event_queue_pending` in Streams mode

An invalid, unreadable, unsupported, or unarchivable candidate increments the failure
counter and leaves readiness and the current program unchanged. A deferral also
leaves the current program active and retries on a later poll.

## Roll back

Find the required digest in the deployment record or reload log, then restore
that exact history artifact through the same atomic publication path:

```sh
cp /var/lib/rex/programs/PROGRAM_ID.bytecode /etc/rex/.current.bytecode.rollback
mv /etc/rex/.current.bytecode.rollback /etc/rex/current.bytecode
```

Confirm a successful reload log with the expected program ID. This restores
rule behavior for later events. It does not undo facts committed by events
already processed under the newer program.

Before removing an archived artifact, verify that `XPENDING` contains no event
for that deployment and retain the artifact for the full
`redis.durable.journal_ttl` after the last event that could have used it. Zero
pending work by itself is insufficient because a journal entry or deliberate
redrive can remain recoverable after acknowledgement. REX logs the program IDs
released from memory after their history files are removed and a later reload
succeeds.

## Recovery failures

At startup REX validates every `*.bytecode` history entry and verifies that its
name matches its digest. A corrupt or misnamed entry prevents readiness so the
operator can restore trustworthy history.

If a durable retry requires a program absent from history, processing returns a
program mismatch and stops ready service without acknowledging the event.
Restore `<required-program-id>.bytecode` to the history directory and restart
the daemon. Do not rename another artifact to that digest; startup verifies the
contents.
