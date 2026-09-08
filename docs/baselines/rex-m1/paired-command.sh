#!/bin/bash
set -euo pipefail
export GOMAXPROCS=2
redis_server=/private/tmp/rex-m0/redis-7.4.2/src/redis-server
redis_cli=/private/tmp/rex-m0/redis-7.4.2/src/redis-cli
"$redis_server" --bind 127.0.0.1 --port 16381 --save '' --appendonly no --daemonize no --dir /private/tmp/rex-m1 > /private/tmp/rex-m1/paired/redis-server.log 2>&1 &
redis_pid=$!
trap 'kill "$redis_pid" 2>/dev/null || true; wait "$redis_pid" 2>/dev/null || true' EXIT
for attempt in {1..100}; do
  kill -0 "$redis_pid"
  if "$redis_cli" -p 16381 INFO server 2>/dev/null | tr -d '\r' | grep -qx "process_id:$redis_pid"; then break; fi
  sleep 0.1
done
"$redis_cli" -p 16381 INFO server | tr -d '\r' | grep -qx "process_id:$redis_pid"
for fixture in sparse-100 sparse-1000 sparse-10000 missing-half; do
  for version in before after; do
    /private/tmp/rex-m1/"$version".bin -source-id "M1-paired-$version" -fixture "$fixture" -events 1000 -runs 5 -warmup 100 > /private/tmp/rex-m1/paired/"$version"/memory-"$fixture".jsonl
  done
done
for fixture in sparse-1000 unique-100 actions-8; do
  for version in before after; do
    /private/tmp/rex-m1/"$version".bin -source-id "M1-paired-$version" -mode redis -redis 127.0.0.1:16381 -fixture "$fixture" -events 100 -runs 3 -warmup 100 > /private/tmp/rex-m1/paired/"$version"/redis-"$fixture".jsonl
  done
done
