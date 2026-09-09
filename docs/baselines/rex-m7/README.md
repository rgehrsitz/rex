# REX-M7 durable processing evidence

Date: 2026-09-08
Status: implementation and local acceptance complete; code review pending

## Delivered contract

The implementation follows [the M7 recovery decision](../../decisions/REX-M7.md)
and the [operator runbook](../../M7_DURABLE_PROCESSING.md). Durable mode is an
explicit alternative to Pub/Sub and accepts only v4 artifacts. It provides:

- Redis Streams group delivery with stable input IDs, acknowledgement, pending
  recovery with `XAUTOCLAIM`, strict oldest-pending ordering, bounded poison
  retries, and an idempotent dead-letter transition;
- one active owner lease per namespace and one in-flight input per partition;
- a journal that pins payload and program digest, records input application,
  stores each round's historical dependency snapshot, and retains commit and
  terminal markers;
- atomic Redis transactions for input facts and their marker, and for derived
  facts, durable output, and the round commit marker;
- stable output, action, write, and dead-letter IDs, with retry reconciliation
  after an unknown transaction reply;
- backlog, pending, retry, recovery, dead-letter, processing, connectivity, and
  readiness signals; and
- explicit persistence, retention, cutover, drain, rollback, repair, and redrive
  procedures.

Infrastructure failures remain pending and fail readiness. They do not consume
the poison-event budget. Journal identity or marker corruption requires
reconciliation and stops the worker.

## Acceptance evidence

The normal and race suites passed from the repository root:

```text
go test ./...
go test -race ./...
go vet ./...
go build ./...
./scripts/test-m5-cli.sh
VERSION=v0.0.0-ci ./scripts/release/build-archives.sh
govulncheck ./...
```

All release archive checksums verified. `govulncheck` reported no called
vulnerabilities.

The opt-in fault suite passed against the existing disposable Redis 7.4.2
binary on IPv4 loopback:

```text
REX_REDIS_TEST_ADDR=127.0.0.1:16382 go test ./pkg/store ./pkg/runtime \
  -run 'TestRealRedis(Durable|BatchOutcomes)' -count=1 -v
```

| Boundary | Evidence | Result |
| --- | --- | --- |
| Before derived commit | Close the first adapter after delivery, journaling, and input application; recover the PEL entry under a different consumer | One derived output, one fact result, input acknowledged |
| After commit, before acknowledgement | Close after the fact/output/marker transaction; recover and retry | Commit marker suppresses a second output; completion acknowledges input |
| During output dispatch | Return `io.ErrUnexpectedEOF` after Redis processes `EXEC` | First result is unknown; retry finds the marker; output stream length remains one |
| Historical replay | Change a dependency after its per-round snapshot is journaled | Retry reads the original snapshot |
| Poison ordering | Fail decoding through the configured attempt limit, then append a valid event | One dead-letter record; pending poison acknowledged; later input completes |
| Backpressure/order | Enqueue a second event while the first remains pending | The worker returns the pending event again and does not deliver the newer entry |
| Program drift | Retry a journaled event with a different artifact digest | Processing stops with `ErrDurableProgramMismatch` |
| Unsafe output topology | Replace the output stream with a string key | Preflight rejects the transaction before any fact write |
| Concurrent owner | Attempt a second lease for the same namespace | Second owner is rejected; compare-and-renew and safe release succeed |

The disposable test disables Redis persistence, so it proves process and client
failure recovery, not host-loss survival. Host-loss durability remains bounded
by the production Redis AOF, backup, and replication configuration documented
in the runbook.
