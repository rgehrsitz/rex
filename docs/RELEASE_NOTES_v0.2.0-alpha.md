# REX v0.2.0-alpha

This release completes the REX foundation roadmap through REX-M8.10. It adds a
deterministic batch runtime, durable Redis processing and recovery, safer
operations, bounded execution, offline rule-development tools, typed facts,
temporal conditions, change-only emission, safe ruleset reload, and exact
partition ownership.

## Supported production contract

- Run one serial durable processor per REX daemon against a standalone Redis
  commit domain.
- Use Redis 7.0 or later with the default scripted durable transaction mode, or
  Redis 6.2 or later with explicit WATCH mode.
- Treat ruleset source as the upgrade source of truth. Current `rexc` emits v4
  through v7 batch artifacts depending on enabled capabilities; explicit v3
  compatibility remains available as documented.
- Configure persistence, TLS, credentials, readiness, and durable recovery using
  the linked operator guides before production rollout.

Redis Cluster and concurrent partition workers sharing one Redis process are
not supported. Measurements in REX-M8.8 through M8.10 did not satisfy the
required latency and scaling gates, so the production daemon remains serial.

## Major additions since v0.1.0-alpha

- Deterministic batch evaluation with bounded state, work, and derived rounds.
- Durable event journaling, atomic commit, retry, reconciliation, fencing, and
  lost-reply recovery.
- Safe ruleset validation, activation, rollback, artifact history, and pinned
  recovery versions.
- Optional typed fact declarations, processing-time temporal conditions, and
  change-only action emission.
- Explanation, lint, simulation, replay, partition planning, and partition
  ownership validation tools.
- Recoverable startup, continuous readiness, TLS and environment-based Redis
  configuration, bounded metrics, and routing validation.
- Script execution removal and explicit failure for retired script actions.
- Versioned cross-platform archives and SHA-256 checksums.

## Upgrade guidance

Read [BYTECODE_COMPATIBILITY.md](BYTECODE_COMPATIBILITY.md) before deploying
existing artifacts. Recompile retained JSON rulesets with this release when an
artifact is outside the documented compatibility set. For durable deployments,
follow [M7_DURABLE_PROCESSING.md](M7_DURABLE_PROCESSING.md) and retain every
ruleset artifact needed by pending journal work. Ruleset reload behavior is
documented in [M8_RULESET_RELOAD.md](M8_RULESET_RELOAD.md), and ownership rollout
is documented in [M8_PARTITION_OWNERSHIP.md](M8_PARTITION_OWNERSHIP.md).

## Deferred performance work

REX-M9 will evaluate a dense commit-path allocation reduction under the D15
paired A/B gates. It is a post-release single-worker optimization and does not
change this release's correctness or topology contract.
