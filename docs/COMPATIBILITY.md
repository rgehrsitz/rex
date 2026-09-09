# Compatibility matrix

This matrix states what a Rex release is built for and what is verified by this
repository's automated checks. “Supported” means a release contract; “tested”
means the current CI suite exercises the combination. It deliberately does not
turn an upstream dependency's broad compatibility claim into a Rex guarantee.

| Area | Supported / built | Automated coverage | Notes |
| --- | --- | --- | --- |
| Release binaries | Linux, macOS, and Windows on `amd64` and `arm64` | Every tagged release cross-builds every listed archive | Download the archive matching the target OS and CPU. |
| Source build toolchain | Go `1.26.6` | CI uses the toolchain pinned by `go.mod` | The `go` directive is `1.26.0`; the toolchain directive selects `1.26.6`. |
| Redis transport | Redis Pub/Sub through `github.com/redis/go-redis/v9`, with verified TLS 1.2+ when enabled | Unit and integration-style tests use plain-text and TLS `miniredis`, including disconnect/reconnect | Validate a production Redis version and deployment topology in staging before treating it as supported for your environment; see [M3 operations](M3_OPERATIONS.md). |
| Rules source | JSON rulesets accepted by the current `rexc` | Parser, compiler, and fuzz tests | Preserve the source ruleset with every deployed bytecode artifact. |
| Bytecode | V4 for undeclared rulesets; v5 for typed declarations; v3 explicit compatibility | Compiler and runtime validation tests | `rexd` rejects versions 1, 2, and all unknown versions. Recompile retained JSON rulesets with the current `rexc`; see [bytecode compatibility](BYTECODE_COMPATIBILITY.md). |
| Scripts | Removed on every platform and execution contract | Source, compiler-API, artifact-loader, and daemon-config rejection tests | Migrate calculations to producer facts or declarative rules; see [M6 migration](M6_SCRIPT_REMOVAL.md). |

The upstream `go-redis` project publishes its own supported Redis versions.
When its compatibility policy changes, reassess the pinned dependency and
update this matrix rather than silently extending Rex's support statement.

Ruleset parsing is strict: unknown fields are rejected. The 2026-08-30
compiler-truthfulness milestone deliberately narrowed source compatibility by
rejecting hybrid or dual-mode condition groups, duplicate rule names, and
actions other than `updateStore`. Bytecode format 2 did not change during that
milestone, but recompiling source that relied on those previously
accepted-invalid shapes now returns a compile error instead of producing an
unexecutable artifact.

Bytecode format 3 makes priority ordering deterministic and stores priority in
the rule execution index. Version-2 artifacts are not accepted because they
were compiled under source-order execution semantics.

V4 batch semantics and adapter limits are documented in the
[M4 migration guide](M4_MIGRATION.md). The Redis commit domain is a standalone
server; sequential writes may have partial/unknown outcomes. No automatic
retry, transaction, durable recovery, or exactly-once delivery is claimed.

V5 adds the optional closed typed-fact namespace documented in the
[M8.2 migration guide](M8_TYPED_FACTS.md). Source without declarations remains
v4 and retains its existing artifact bytes.
