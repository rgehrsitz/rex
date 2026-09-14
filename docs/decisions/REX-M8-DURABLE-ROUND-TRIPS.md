# D14 — Script steady-state durable transactions

Accepted for REX-M8.9, 2026-09-13.

## Decision

Use Redis Lua scripts for the five steady-state durable stages: `Begin`,
`ApplyInput`, historical snapshot read/store, round commit, and `Complete`.
Each script performs its ownership checks, reconciliation checks, and effects in
one server execution. The existing `WATCH`/`MULTI` implementation remains
available through `redis.durable.transaction_mode: "watch"` for explicit
rollback. The daemon default is `script`; an empty library option remains
`watch` for source-compatible Redis 6.2 embedding.

WATCH mode continues to support one standalone Redis 6.2-or-newer commit domain;
script mode requires Redis 7.0+ so it can preflight all writes with
`redis.acl_check_cmd` before its first mutation. Redis
Cluster remains unsupported because durable commits can span arbitrary fact,
journal, ownership, and stream keys. Scripted deployments also require Redis ACL
permission for `EVALSHA`, `EVAL`, and `SCRIPT LOAD` plus every command and key
invoked by the scripts. Script and ACL-preflight capability are checked when the durable adapter opens; REX
does not silently change transaction modes.

## Protocol

Every key is supplied through `KEYS`. Go supplies the exact existing encoded
fact values, journal markers, output payload, stable IDs, and retention values.
Scripts do not construct persistent formats, so `rex-m7-v1` journals and stream
entries remain readable in both modes.

Managed partitions compare the live owner ID and the complete immutable claim
before any effect. Scripts return data statuses rather than Redis error replies:
success, already complete, terminal, ownership loss, reconciliation mismatch,
program mismatch, or unsafe key type. A malformed reply or transport failure
after dispatch remains an infrastructure error. Commit reports an unknown
outcome unless the script returned a definitive preflight failure; retry reads
the marker inside the same atomic script and resolves a lost reply without a
duplicate output.

Snapshot encoding stays in Go. One script checks the fence and existing
historical value, Go reads and encodes a missing snapshot, and a second script
rechecks the fence and stores or returns the winner. This reduces exchanges
without changing snapshot bytes or moving type semantics into Lua.

## Scope and gates

Cold paths remain on `WATCH`: dead-lettering, the post-completion acknowledge
fallback, lease acquire/renew/release, ownership reservation, and temporal
cleanup. Event acquisition, pinned program/time operations, daemon supervision,
partition routing, Redis Functions, Cluster hash tags, and dense-allocation work
are outside M8.9.

Acceptance requires:

1. full normal/race regression suites plus targeted real-Redis script-mode
   recovery, fencing, lost-reply, cache-loss, and ACL tests;
2. format and recovery compatibility across `script` and `watch`;
3. automatic recovery after `SCRIPT FLUSH`;
4. at least a 2x reduction in measured client exchanges across the five stages;
5. paired single-partition sparse throughput of at least 1.5x and dense
   throughput of at least 1.15x, with script p99 no worse than the maximum paired
   WATCH p99 and coefficient of variation no greater than 20%; and
6. a diagnostic rerun of D13 after the preceding gates pass.

The D13 rerun does not decide M8.9. It supplies evidence for the next production
concurrency decision because the earlier failure may include host CPU and
single-threaded Redis contention beyond transaction round trips.

## Rollback

Set `redis.durable.transaction_mode` to `watch` and restart the partition owner.
No data migration or backlog drain is required. The ownership lease prevents two
implementations from processing one partition concurrently, and both modes use
the same markers to recover in-flight work.

## Consultation

Claude recommended measuring client exchanges rather than treating Redis command
counts as round trips, using scripts instead of a smaller WATCH refactor, keeping
the storage format unchanged, retaining an explicit rollback mode, and judging
this milestone independently of D13. Those recommendations are adopted. The
snapshot read deliberately remains in Go between two fenced scripts so it avoids
Lua array limits and preserves the existing decoder.
