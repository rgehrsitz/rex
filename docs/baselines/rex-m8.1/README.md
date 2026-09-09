# REX-M8.1 local acceptance evidence

Date: 2026-09-09

Starting revision: `f1298b1`

Branch: `codex/rex-m8-ruleset-reload`

## Delivered behavior

- Validated v4 rulesets activate atomically between complete events.
- Failed candidates retain the active program and unchanged invalid bytes are
  not repeatedly validated.
- Exact artifacts are archived by SHA-256 program ID before activation.
- Startup validates and registers bounded history for M7 durable recovery.
- Durable retries select their journal-pinned engine, while reload waits for
  existing pending deliveries to drain.
- Operators can roll back by atomically restoring an archived artifact to the
  watched bytecode path.
- Metrics count successful, failed, and deferred reload attempts without
  unbounded labels.

## Acceptance checks

The following completed successfully on the local macOS arm64 workspace:

```text
git diff --check
test -z "$(gofmt -l .)"
go mod tidy -diff
go vet ./...
go test ./...
go test -race ./...
./scripts/test-m5-cli.sh
go build ./...
VERSION=v0.0.0-ci DIST_DIR=<temporary-directory> ./scripts/release/build-archives.sh
govulncheck ./...
```

The archive check cross-built `rexc`, `rexd`, and the release tools for amd64
and arm64 on Darwin, Linux, and Windows. `govulncheck` found no called
vulnerabilities. The existing real-Redis M7 fault suite remains unchanged;
M8.1 adds a journal lookup covered by the Redis store test and exercises reload
backlog coordination through a deterministic stats adapter.

## Focused fixtures

- `TestEngineManagerSwapsOnlyAtEventBoundary`
- `TestEngineManagerUsesPinnedHistoricalProgram`
- `TestEngineManagerReleasesPrunedHistoricalPrograms`
- `TestRulesetReloadRejectsInvalidCandidateAndKeepsActive`
- `TestRulesetReloadArchivesThenAtomicallyActivatesCandidate`
- `TestRulesetReloadWaitsForPendingDurableWork`
- `TestRulesetReloadRetriesAfterHistoryCapacityIsFreed`
- `TestRulesetReloadRejectsMisnamedHistory`
- `TestRulesetReloadPreservesProgramForEventEnteringAfterPendingCheck`
- `TestRulesetReloadCleansAbandonedTempFiles`
- `TestRedisDurableCommitRecoveryAndSnapshotReplay`

Hosted CI and review remain integration gates. Replace the branch reference
with the merged revision and PR link when M8.1 is integrated.
