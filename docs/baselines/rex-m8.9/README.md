# REX-M8.9 durable round-trip evidence

Starting revision: `d01b5ad30af29983d42290506d61157f9d22ad6b` (merged
REX-M8.8, PR #47). Measurements used the uncommitted candidate whose exact
source hashes are in [metadata.json](metadata.json).

## Result

The D14 single-driver A/B gate passes on this host. Five shuffled 1,000-event
runs per mode and workload used a fresh loopback Redis 8.10.1 process per case,
disabled persistence, `GOMAXPROCS=2`, 100 warmup events, and four isolated
partitions drained by one serial round-robin driver.

| Workload | WATCH median events/s | Script median events/s | Ratio | WATCH p99 max | Script p99 max | Result |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| Sparse balanced | 1,998 | 4,021 | 2.01x | 2.337 ms | 0.836 ms | Pass |
| Dense balanced | 1,112 | 1,374 | 1.24x | 2.662 ms | 2.350 ms | Pass |

Throughput coefficients of variation were 2.99–9.46%, within the 20% limit.
Median allocations fell from 50,282 to 20,341 bytes/event sparse and from
457,666 to 429,076 bytes/event dense. Median Redis CPU fell from 188 to 132
microseconds/event sparse and from 246 to 242 microseconds/event dense.

The real-Redis exchange regression observed 7 client exchanges for the five
scripted stages versus 33 in WATCH mode, a 4.7x reduction. Redis command counts
rose from 44 to 51 sparse and 143 to 150 dense because Redis counts commands
invoked inside Lua; those values are diagnostics and are not network exchanges.
See [summary.json](summary.json), [gates.json](gates.json), and
[exchanges.json](exchanges.json).

## Correctness and compatibility

Normal and real-Redis tests cover script cache loss, identical WATCH/script
journal and stream formats, recovery in both mode-switch directions, stale-owner
fencing, lost commit reply followed by ownership loss, restart boundaries, and
an ACL that denies `XADD`. The denied command is detected before any fact,
stream, or marker write. The existing WATCH path remains the empty library
default and the configured daemon rollback mode.

Script mode uses `redis.acl_check_cmd`, so it requires Redis 7.0 or newer. This
run used Redis 8.10.1; hosted CI supplies Redis 7.4.2. Redis 6.2 remains supported
only by WATCH mode and was not separately executed in this local evidence.

## D13 diagnostic

The unchanged five-run 1/2/4-worker D13 matrix again processed 75,000 events.
The runner asserted order, ownership, recovery, deduplication, cleanup, and exact
command behavior during every case. The retained artifacts contain the aggregate
gate and the full validated summary. Its overall performance gate still fails: sparse two-worker
speedup passes at 1.52x but its worst p99 exceeds one worker; dense p99 passes but
speedup is 1.43x, below the 1.50x threshold. Production concurrency remains
disabled. [d13-diagnostic.json](d13-diagnostic.json) retains the gate, environment,
source hashes, and summary digest; [d13-summary.json](d13-summary.json) retains the
complete 75-case summary.

## Limits

These are loopback measurements on one four-performance-core host, not a remote,
TLS, persistence, replica, failover, or managed-service capacity claim. The A/B
latency gate compares maxima across independent fresh Redis cases; it is useful
for rejecting regressions but does not model an arrival distribution. Raw case
samples were validated and reduced to the retained summary. Redis Cluster,
supervision, routing, temporal cleanup, and cold-path scripts remain out of scope.
