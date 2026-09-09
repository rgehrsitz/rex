# REX-M8.2 local acceptance evidence

Date: 2026-09-09

Starting revision: `a001349`

Branch: `codex/rex-m8-typed-facts`

## Delivered behavior

- Optional declarations select v5 and define a closed scalar fact namespace.
- Existing undeclared source retains deterministic v4 artifacts and behavior.
- Compiler checks condition constants and action values against declarations.
- Runtime, replay, Pub/Sub, and Streams reject bad external input before REX
  persists state; durable processing validates before its input journal write.
- Missing remains valid and unknown, nullable facts accept explicit null, and
  incompatible stored values become invalid/unknown without coercion.
- Explain output exposes declarations and the actual execution contract. Reload
  and historical durable selection accept supported v4 and v5 artifacts.

## Focused fixtures

- `TestTypedFactArtifactContract`
- `TestTypedFactCompilerValidation`
- `TestLegacyCompilerRejectsTypedFacts`
- `TestTypedFactRuntimeContract`
- `TestProcessNextDurableValidatesTypedInputBeforePersistence`
- `TestTypedFactToolingContract`
- `TestSchemaAndParserAgreement`

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

The v5 example also passed `rexc validate`, `rexc bundle`, and `rexc simulate`.
The archive check cross-built release tools for amd64 and arm64 on Darwin,
Linux, and Windows. `govulncheck` found no called vulnerabilities; its existing
unreachable package/module findings remain.

Hosted CI and review remain integration gates.
