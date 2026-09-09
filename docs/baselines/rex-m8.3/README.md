# REX-M8.3 local acceptance evidence

Date: 2026-09-09

Starting revision: `da2b03a`

Branch: `codex/rex-m8-temporal-rules`

## Delivered behavior

- A bounded leaf-level `for` duration selects execution contract v6.
- Processing time is injected and sampled once per chain; offline scenarios use
  explicit nondecreasing RFC 3339 times.
- Private timer starts persist across restart and exact-artifact reload without
  becoming public facts, publications, outputs, derived events, or actions.
- False, missing, null, and invalid observations reset the timer, including past
  otherwise decisive Boolean siblings. The exact deadline is inclusive.
- V4/v5 artifacts and source behavior remain compatible; event-time semantics
  remain explicitly unsupported.

## Focused fixtures

- `TestTemporalArtifactContractAndValidation`
- `TestTemporalConditionCountIsBounded`
- `TestTemporalConditionBoundaryResetAndRestart`
- `TestTemporalStateMaintainedPastBooleanShortCircuit`
- `TestTemporalConditionRequiresClockAndRestartsAfterRegression`
- `TestTemporalAndActionWritesShareStagedByteBudget`
- `TestTemporalMaintenanceSkipsUnrelatedShortCircuitedSiblings`
- `TestMemoryInternalWritesArePrivate`
- `TestRedisDurableInternalCommitIsAtomicAndSilent`
- `TestProcessNextDurablePinsTemporalTimeAcrossRetry`
- `TestTemporalDurableQueueWithoutTimeJournalIsInfrastructureFailure`
- `TestEngineManagerCleansTemporalStateWhenHistoryIsReleased`
- `TestEngineManagerCleansTemporalStateOutsideManagerLock`
- `TestEngineManagerContinuesRetirementAfterCleanupFailure`
- `TestEngineManagerDropsPermanentlyUnsupportedCleanup`
- `TestTemporalReplayUsesExplicitProcessingTime`
- `TestSchemaParserAgreement`

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

The v6 example also passed `rexc validate`, `rexc bundle`, and `rexc simulate`;
the report selected execution contract 6 and fired exactly once at the boundary.
The archive check cross-built release tools for amd64 and arm64 on Darwin,
Linux, and Windows. `govulncheck` found no called vulnerabilities; its existing
unreachable package/module findings remain.

Hosted CI and review remain integration gates.
