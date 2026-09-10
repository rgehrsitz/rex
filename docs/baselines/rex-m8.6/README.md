# REX-M8.6 acceptance evidence

Base: `97042ec` (merged M8.5 PR #44), branch
`codex/rex-m8-partition-ownership`. Implementation is local; review and hosted
CI are pending. No artifact version or dependency changes.

## Delivered behavior

Optional exact-name Streams ownership, complete active/reload/history coverage,
`rexc partition-check`, immutable persistent public/protocol claims, a bounded
legacy-mode marker, and lease-guarded managed input/output/terminal operations,
snapshot recording and timer cleanup. Public footprint computation is cached
in the immutable Program. Successful program coverage checks are cached by
artifact digest per adapter (bounded at 1,024 entries); claim JSON is cached.

Claims are immutable across restart; offline domain-wide migration is required
for transfer, shrink, or conversion from legacy. Initial enable rejects pending
work in the configured group and a live owner. Claims do not fence older
binaries, Pub/Sub or direct Redis writers; deployments exclude those writers.
M8.7 owns representative durable profiling; no speedup or capacity claim here.

## Verification

Environment: Go 1.26.6, macOS 26.6.2, Apple M4. Built disposable Redis 7.4.2 from
its official release tarball; bound to 127.0.0.1:16386 with persistence disabled.
Real managed tests use DB 15, separate from existing legacy fixtures in DB 0.
The server contains only this task's disposable test data and is stopped after
validation. The race detector covers claim races and normal repository tests. CI now
provides Redis 7.4.2 and enables these real-Redis tests in the normal race job.

Passed locally:

```sh
go test -count=1 ./...
go test -race -count=1 ./...
REX_REDIS_TEST_ADDR=127.0.0.1:16386 go test -race -count=1 ./...
go vet ./...
go build ./...
bash scripts/test-m5-cli.sh
go run ./cmd/rexc partition-check -rules examples/m8-partitions/rules.json -ownership examples/m8-partitions/ownership.json
REX_REDIS_TEST_ADDR=127.0.0.1:16386 go test -race -count=1 ./pkg/store ./pkg/runtime -run TestRealRedis
git diff --check
```

Focused race tests were repeated after the final claim/recovery regressions.
The ownership CLI example emits schema 1 with an empty diagnostics array.
Full repository tests retain frozen legacy/batch semantics and artifact tests.

Covered failure/compatibility boundaries:

- Invalid/duplicate/empty/reserved ownership names, literal commas in config,
  disabled legacy policy, public footprint including unused typed declarations,
  nested temporal predicates and change-only targets.
- Deterministic offline coverage, exit codes and malformed specifications;
  startup configuration, reload and historical artifact rejection.
- Concurrent conflicting managed opens yield one winner; disjoint managed
  owners both work; legacy/managed races yield one accepted mode.
- Three hundred legacy namespace reservations still occupy one marker.
  Unchanged managed opens do not invalidate a registry WATCH.
- Overlapping fact and protocol keys fail before stream creation; wrong stream
  types fail before reservation; immutable claim changes fail; canonical
  reordered facts permit successor restart.
- First enable rejects pending legacy history and a live old owner.
- Unowned mixed input leaves no facts before dead-lettering; an incompatible
  pinned program through a RedisDurable queue remains pending without a DLQ.
- After successor takeover, stale Begin, completion, acknowledgement,
  dead-letter, snapshot, commit and private cleanup are rejected.
- Unchanged managed/legacy acquisition succeeds despite producer XADDs during
  the transaction. Renewal retries a benign registry rewrite; terminal
  completion retries its own lease refresh, with real Redis verification.
- Real Redis concurrent claims yield one winner. A client hook deletes the
  owner lease immediately before EXEC: no fact/output escapes. A successor
  retries; an injected lost EXEC reply is recovered using the commit marker,
  yielding one output and a terminally acknowledged input.
- Existing real Redis durable restart/poison/lost-reply tests also pass.

The first broad real-Redis rerun encountered per-namespace legacy records left
by an earlier uncommitted prototype. The final single-marker format correctly
rejected them. Restarting the disposable, nonpersistent server and rerunning
passed; this was not a bypass of a final-format recovery failure.

## Claude consultation

Claude recommended splitting enforced ownership (M8.6) from profiling (M8.7).
Its implementation review led to a single legacy marker instead of accumulating
legacy claims; no-op HSET avoidance; key-type preflight; clearer missing-lease
errors; cached program/claim data; and additional takeover, initial-enable and
real-queue tests. Follow-up review identified startup contention from watching
streams during unchanged acquisition and missing optimistic-conflict retries
around lease renewal/terminal operations. Those are fixed and covered by
producer-write and real Redis registry/lease-refresh hooks. The final scoped review confirmed both remaining findings were fixed and
reported no concrete remaining defect.

Material design differences are recorded in D12: no online shrink/release,
whole-domain managed/legacy exclusion, and disjoint protocol streams. Global
registry WATCH remains intentional so claim removal/change invalidates managed
effects; only new registrations mutate it, not idempotent restarts. A new
namespace can transiently abort another transaction, which retries or remains
pending under the existing bounded recovery protocol.
