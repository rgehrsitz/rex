# REX-M6 — Remove in-process scripting

Status: implemented locally; review pending, 2026-09-08. Depends on M4, merged
in PR #35, and follows M5, merged in PR #36 at
`039f5c649448fc2261239bade97bde250cb497f3`.

## Decision

REX removes JavaScript execution instead of replacing Otto with an isolated
worker. This resolves D6 by making the script capability unavailable on every
platform and in every execution contract.

The old in-process runner could return on timeout while JavaScript continued in
a goroutine. Its VM and script registry were shared mutable state, and it could
not enforce memory, CPU, host-access, cancellation, or cleanup boundaries. A
safe general-purpose worker would require an operating-system-specific resource
and process lifecycle contract. V4 already excluded scripts, and maintaining
that worker contract was not justified by the remaining legacy capability.

## Contract

- JSON source containing a `scripts` field is rejected, including an explicitly
  empty object. An action string enclosed in braces, such as `{calculate}`, is
  rejected as a retired script call.
- Both the default v4 compiler and explicit legacy-v3 compiler apply that rule.
  The embedded `GenerateBytecode` API applies it to programmatically built ASTs,
  including a non-nil empty script map.
- The v3 opcode numbers `SCRIPT_DEF` and `SCRIPT_CALL` remain reserved so a
  loader can identify an old artifact and return a deterministic migration
  error. Neither opcode has an execution path.
- `engine.scripts_enabled` remains in configuration as a migration tripwire.
  `false` is accepted; `true` prevents daemon startup. The embedded
  `SetScriptsEnabled(false)` call remains source compatible and enabling it
  returns an error.
- Script-free v3 artifacts keep their bytecode version and behavior. V4 meaning
  and replay remain unchanged.

With no script runtime, rules cannot read clocks, generate randomness, access
the host, retain VM state, or return a partial script result. The rejection is
platform-independent across every release target. This is a capability boundary,
not a claim that arbitrary rule authorship or the daemon as a whole forms a
security sandbox.

## Consequences

Otto and its transitive dependencies leave the production dependency graph.
Infinite loops, allocation loops, malformed results, runtime crashes, timeouts,
and caller cancellation in JavaScript are bounded at zero execution: the source
or artifact fails before an engine is returned. There are no script workers or
goroutines to orphan and no script result to commit.

This intentionally narrows legacy-v3 source and artifact compatibility. It does
not reinterpret an accepted artifact: a script-bearing v3 artifact now fails
load and must be migrated. The source ruleset remains the record from which a
script-free artifact is compiled.

## Verification

Regression tests cover explicit empty script fields, script action calls,
nonterminating and allocation-growth bodies, programmatic compiler inputs,
both retired v3 opcodes, and the daemon/API enable tripwire. The normal and race
test suites, vet, build, release cross-builds, dependency checks, vulnerability
scan, and decoder fuzz smoke run provide the milestone evidence recorded in the
roadmap.
