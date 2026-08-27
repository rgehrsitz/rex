#!/bin/sh
set -eu

event="$(cat /demo/events/temperature-high.json)"

for attempt in $(seq 1 30); do
  if redis-cli -h redis ping | grep -qx PONG; then
    break
  fi
  sleep 1
done

redis-cli -h redis SET temperature 35 >/dev/null

for attempt in $(seq 1 30); do
  redis-cli -h redis PUBLISH rex_updates "$event" >/dev/null
  if [ "$(redis-cli --raw -h redis GET demo:status)" = '"hot"' ]; then
    echo "Rex demo smoke test passed: demo:status is hot"
    exit 0
  fi
  sleep 1
done

echo "Rex demo smoke test failed: demo:status was not set to hot" >&2
exit 1
