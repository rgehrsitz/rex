# REX-M3 operations contract

REX-M3 makes dependency failures observable and recoverable without changing
the Redis Pub/Sub delivery guarantee. This document is the deployment contract
for `rexd`.

## Configuration and secrets

Configuration precedence is environment variable, JSON configuration file,
then built-in default. Environment names are formed from the setting path:
uppercase it, replace dots with underscores, and prefix `REX_`.

| JSON setting | Environment variable | Default |
| --- | --- | --- |
| `redis.address` | `REX_REDIS_ADDRESS` | `localhost:6379` |
| `redis.username` | `REX_REDIS_USERNAME` | empty |
| `redis.password` | `REX_REDIS_PASSWORD` | empty |
| `redis.database` | `REX_REDIS_DATABASE` | `0` |
| `redis.channels` | `REX_REDIS_CHANNELS` | `rex_updates` |
| `redis.connect_timeout` | `REX_REDIS_CONNECT_TIMEOUT` | `5s` |
| `redis.health_check_interval` | `REX_REDIS_HEALTH_CHECK_INTERVAL` | `1s` |
| `redis.health_check_timeout` | `REX_REDIS_HEALTH_CHECK_TIMEOUT` | `500ms` |
| `redis.tls.enabled` | `REX_REDIS_TLS_ENABLED` | `false` |
| `redis.tls.server_name` | `REX_REDIS_TLS_SERVER_NAME` | empty |
| `redis.tls.ca_file` | `REX_REDIS_TLS_CA_FILE` | system roots |

`REX_REDIS_CHANNELS` is a comma-separated list. Durations use Go duration
syntax such as `250ms`, `5s`, or `1m`.

TLS verifies the server certificate and requires TLS 1.2 or newer. When
`server_name` is empty, REX derives it from the host in `redis.address`. Set
`server_name` when that host is not the certificate name. Set `ca_file`
to a PEM bundle for a private authority. REX has no setting to skip certificate
verification. Logs include the Redis address, database, and whether TLS is in
use; they omit usernames, passwords, fact values, and event payloads from Redis
connection and publication diagnostics.

## Startup, health, and shutdown

Redis construction performs a `PING` under `redis.connect_timeout` and returns
an error to the daemon on refusal, authentication failure, invalid TLS, or
cancellation. It does not exit the process from the store package. Resources
opened before a later startup failure are closed.

`/healthz` reports whether the HTTP process is serving. `/readyz` starts at
`503`, changes to `200` after Redis is connected and the subscription is active,
changes back to `503` when either the subscription reports an error or a health
check fails, and returns to `200` after connectivity recovers. Shutdown sets
readiness to false and closes the subscription socket before waiting for its
reader.

Disconnect and reconnect notifications are edge-triggered: one disconnect and
one event-source error are recorded for a continuous outage, followed by one
reconnect when the subscription is restored. Repeated receive failures during
the same outage do not inflate the counters. The metrics endpoint reports these
dependency transition counters. `rex_event_processing_duration_seconds` is a
histogram with fixed buckets from 1 ms through 1 second plus `+Inf`.
`rex_rule_outcomes_total` and
`rex_action_outcomes_total` use fixed outcome values, so rule names, fact names,
action targets, channel names, and error messages cannot create unbounded metric
series.

## Routing contract

`redis.channels` lists inbound channels consumed by this daemon. It is not an
output allowlist.

Legacy v3 `updateStore` actions store their target fact and publish the update
to the target prefix before the first colon. `weather:status` therefore publishes
to `weather`; a target without a colon publishes to its full target name. An
empty prefix such as `:status` is rejected by the compiler and store before any
write. Whether an external subscriber exists cannot be determined statically.
Deployments must provision consumers for every required output prefix. Add an
output prefix to `redis.channels` only when the same daemon should consume its
derived update, and retain `engine.max_event_hops` to bound feedback.

V4 stages writes and publishes one committed-output envelope to `rex_results`
after a successful commit. V4 derived rounds happen inside the coordinator, so
the daemon ignores its own committed-output envelope. External consumers that
need committed results subscribe to `rex_results`.

## Payload and overload behavior

Inbound events are limited to 1 MiB at the transport boundary. V4 also enforces
the configured event-fact, snapshot, action, round, work, and staged-byte limits
described in the [M4 migration guide](M4_MIGRATION.md). Rejected payloads are
reported as event failures and do not enter evaluation.

The event source reads directly from the Pub/Sub socket and hands each event to
the serial consumer through an unbuffered channel. Slow evaluation therefore
applies TCP backpressure instead of filling an unbounded application queue or
silently timing out a buffered send. Redis Pub/Sub remains best effort: it does
not retain events while a subscriber is disconnected and exposes neither queue
lag nor a broker-side drop count. `rex_event_queue_lag_seconds` and
`rex_event_queue_drops` are consequently `NaN`. Use the disconnect and event
source error counters to detect known risk windows. REX-M7 introduces the
durable mode needed for replay and acknowledgement.
