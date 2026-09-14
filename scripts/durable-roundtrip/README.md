# REX-M8.9 durable round-trip measurements

This opt-in runner performs shuffled paired `watch` and `script` measurements
using the M8.7 serial processor harness. Each case owns a fresh loopback Redis
process with persistence disabled. It records source hashes, summary metrics,
and the predeclared D14 gates. Pass `--retain-raw` to keep per-case files.

```sh
python3 scripts/durable-roundtrip/run.py \
  --redis-server /path/to/redis-server \
  --redis-cli /path/to/redis-cli \
  --output /tmp/rex-m8.9 \
  --runs 5 --events 1000
```

The four partitions are drained by one serial round-robin driver. This isolates
transaction-path cost; it is not a production concurrency or capacity test.
Redis command counts include commands invoked inside Lua and therefore are
diagnostic, not a proxy for client exchanges. The store regression test measures
client exchanges directly.
