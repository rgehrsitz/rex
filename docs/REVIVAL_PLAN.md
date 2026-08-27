# Rex revival plan

## Purpose

Rex has a sound compiler/runtime foundation and a healthy test suite. This plan keeps it maintainable through small, reversible changes rather than a rewrite. Update this document as milestones are completed or priorities change.

## Baseline assessed on 2026-08-25

- `go test ./...`, `go test -race ./...`, `go vet ./...`, and all command builds pass.
- Statement coverage is 76.8% overall; compiler, runtime, scripting, Redis-store, end-to-end, and benchmark tests already exist.
- The repository had legacy GitHub Actions, but no maintained CI baseline, release workflow, container setup, or project task runner.
- The direct dependency set is dated. In particular, `github.com/redis/go-redis/v9` v9.5.3 has a reachable vulnerability fixed in v9.6.3.
- A vulnerability scan also found reachable standard-library issues in the Go 1.26.0 toolchain used for the assessment; Go 1.26.6 or newer fixes those findings.

## Guiding decisions

- Preserve the existing JSON ruleset -> bytecode -> runtime model.
- Prefer a sequence of independently releasable changes; do not combine dependency updates with behavioural refactors.
- Treat scripts as trusted-only until their execution model is redesigned. Do not accept untrusted rules containing scripts.
- Keep Redis as the first-class adapter. Defer NATS until the storage/event boundary is clean.

## Now: safety and maintenance baseline

- [x] Adopt a supported, patched Go toolchain and record the supported Go policy in `go.mod`, CI, and the README.
- [x] Upgrade `go-redis` to at least v9.6.3; prefer the current v9 release after running the full suite.
- [x] Update the remaining direct dependencies in a dedicated compatibility change.
- [x] Add CI that runs formatting checks, `go test ./...`, `go test -race ./...`, `go vet ./...`, `govulncheck ./...`, and command builds.
- [x] Add scheduled dependency-update automation.
- [x] Replace the placeholder content in `SECURITY.md` with real supported-version and contact information.
- [x] Correct stale README claims and examples: Go version, nonexistent example path, placeholder Codecov token, and the NATS claim.

**Completion criteria:** a clean clone has a documented toolchain, reproducible validation commands, automated checks on every change, and no known reachable dependency vulnerability.

## Next: make the runtime reliable

- [x] Make `rexd` the only owner of the Redis subscription. Remove the engine's hidden, hard-coded subscription loop.
- [x] Make `rexd` subscriptions honor its root context and close Redis clients and engines on shutdown.
- [x] Pass caller contexts through runtime fact/action operations and Redis fact-store calls.
- [x] Keep Redis transport types out of the engine-facing store interface.
- [x] Define a canonical JSON fact-event format and decode JSON values consistently, including strings and booleans.
- [x] Make comparison failures return `false`; never panic from an unchecked type assertion.
- [x] Add cancellation, signal-handling, duplicate-delivery, malformed-event, and shutdown integration tests.

**Completion criteria:** every running resource has an owner and shutdown path; an event is consumed once by the configured path; malformed data cannot crash `rexd`.

## Then: make compiled artifacts trustworthy

- [x] Create a bytecode decoder that validates header version, offsets, lengths, instruction boundaries, and declared counts before execution.
- [x] Replace the placeholder checksum with an actual CRC-32 integrity check.
- [x] Sort map-derived data before serialization so compiling the same ruleset produces identical bytecode.
- [x] Add fuzz tests for ruleset parsing and bytecode decoding, plus corrupt/truncated-artifact regression tests.
- [x] Document bytecode compatibility and an artifact versioning policy in [the bytecode compatibility guide](BYTECODE_COMPATIBILITY.md).

**Completion criteria:** corrupt or incompatible bytecode returns a clear error, never a panic; identical input produces identical output.

## Script decision gate

The present Otto timeout only returns from the caller; it does not stop an infinite JavaScript execution. The VM is also shared mutable state. Before expanding scripts, choose one path:

1. **Trusted scripts only (selected for the next release):** scripts are disabled by default and may be enabled with `engine.scripts_enabled` only for controlled deployments with fully trusted rulesets.
2. **Isolated execution:** run scripts in a separate constrained process with a hard timeout and memory/CPU limits. This is necessary for untrusted rule authors.
3. **Remove scripts:** retain a smaller, safer declarative rules engine.

Do not present the current in-process execution as a security boundary.

## Later: developer and operator experience

- [x] Add `rexc validate`, `--output`, and useful JSON source-location diagnostics.
- [x] Provide a one-command Redis demo via Docker Compose, with a smoke test and sample event.
- [ ] Add structured traces explaining a rule evaluation and action outcome.
- [ ] Add health checks and metrics for event throughput, evaluation latency, rule fires, action failures, and queue lag.
- [ ] Add cycle protection, action limits, and idempotency guidance for chained rules.
- [ ] Publish versioned binaries, checksums, release notes, and a compatibility matrix.

## Possible product directions

- A replay/simulation CLI for testing historical fact events against a ruleset.
- Explicit fact schemas and typed values rather than loosely typed JSON values.
- A rule playground with JSON Schema validation and evaluation explanations.
- A NATS adapter after the store/event boundary is refactored.
- Additional action adapters, such as webhooks or queues, with retry and dead-letter behaviour.

## Change discipline

Each implementation pull request should state the milestone it advances, retain or improve test coverage, and keep unrelated modernization out of scope. A dependency bump, runtime refactor, bytecode format change, and feature addition should be separate changes whenever practical.
