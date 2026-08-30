# Compatibility matrix

This matrix states what a Rex release is built for and what is verified by this
repository's automated checks. “Supported” means a release contract; “tested”
means the current CI suite exercises the combination. It deliberately does not
turn an upstream dependency's broad compatibility claim into a Rex guarantee.

| Area | Supported / built | Automated coverage | Notes |
| --- | --- | --- | --- |
| Release binaries | Linux, macOS, and Windows on `amd64` and `arm64` | Every tagged release cross-builds every listed archive | Download the archive matching the target OS and CPU. |
| Source build toolchain | Go `1.26.6` | CI uses the toolchain pinned by `go.mod` | The `go` directive is `1.26.0`; the toolchain directive selects `1.26.6`. |
| Redis transport | Redis Pub/Sub through `github.com/redis/go-redis/v9` | Unit and integration-style tests use `miniredis` | Validate a production Redis version and deployment topology in staging before treating it as supported for your environment. |
| Rules source | JSON rulesets accepted by the current `rexc` | Parser, compiler, and fuzz tests | Preserve the source ruleset with every deployed bytecode artifact. |
| Bytecode | Format version 2 only | Compiler and runtime validation tests | `rexd` rejects format-version 1 and all unknown versions. See [bytecode compatibility](BYTECODE_COMPATIBILITY.md). |
| Scripts | Otto JavaScript, disabled by default | Runtime tests cover enabled and disabled behavior | Enable only for rulesets from trusted authors; scripts are not sandboxed. |

The upstream `go-redis` project publishes its own supported Redis versions.
When its compatibility policy changes, reassess the pinned dependency and
update this matrix rather than silently extending Rex's support statement.

Ruleset parsing is strict: unknown fields are rejected. The 2026-08-30
compiler-truthfulness milestone deliberately narrowed source compatibility by
rejecting hybrid or dual-mode condition groups, duplicate rule names, and
actions other than `updateStore`. Bytecode format 2 did not change, but
recompiling source that relied on those previously accepted-invalid shapes now
returns a compile error instead of producing an unexecutable artifact.
