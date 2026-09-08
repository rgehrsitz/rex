# Bytecode compatibility and versioning

Rex bytecode is a compiled artifact, not the source of record for a ruleset.
Keep the JSON ruleset, the `rexc` version, and any deployment configuration
alongside a bytecode file so the artifact can be rebuilt when the format
changes.

## Current support

`rexc` writes **v4** by default, and `rexd` defaults to the v4 batch contract.
V3 is retained for script-free artifacts: compile using `rexc -legacy-v3` and
explicitly set `engine.allow_legacy_v3: true` to run it in the daemon.
Versions 1, 2, and unknown versions are rejected. Keep the source JSON and
recompile; changing a version field is not a migration.

M6 removed scripting from every contract. The compiler rejects script source,
and the v3 loader rejects `SCRIPT_DEF` and `SCRIPT_CALL` before execution. Their
opcode numbers remain reserved only to provide a deterministic migration error.

Embedded APIs `GenerateBytecode` / `WriteBytecodeToFile` and `compiler.Version`
remain explicitly v3 for existing integrations and the frozen semantics corpus.
New callers use `CompileBatch`, `BatchVersion`, and `LoadProgram` or the
version-dispatching `NewEngineFromFile`. `DecodeBatch` accepts only v4.

## Version-4 format and meaning

V4 uses a bounded structured rule IR so nested groups can retain three-valued
semantics. It does not reinterpret v3 jumps. The 16-byte header is:

| Offset | Field | Meaning |
| ---: | --- | --- |
| 0 | uint32 version | Little-endian `4`. |
| 4 | uint32 checksum | IEEE CRC-32 of the payload only. |
| 8 | uint32 payload length | Exact byte count after the header. |
| 12 | four bytes | ASCII `REXB`. |

The payload is canonical JSON for a validated rule AST with applied priority
defaults. Source-order rules/actions and boolean grouping are retained; map keys
are deterministically encoded. The compiler and loader reject scripts, invalid
operator/constant combinations, excessive nesting, more than 10,000 rules,
100,000 condition nodes, 65,536 distinct dependencies, and payloads above 8 MiB.
The CRC detects corruption, not malicious authorship; load-time semantic
validation remains required. The file loader bounds reads to 8 MiB plus header.

V4 means deduplicated batch rounds, a shared snapshot, Unknown propagation,
staged/coalesced writes, conflict rejection, and bounded local derived rounds.
See the [decision record](decisions/REX-M4.md) and [migration guide](M4_MIGRATION.md).
The default [source schema](../examples/rex-rules-schema.json) describes v4;
[the legacy schema](../examples/rex-v3-rules-schema.json) remains available.

## Version-3 format

All multi-byte integers are unsigned, little-endian 32-bit values unless a
section says otherwise. The fixed header is 28 bytes:

| Offset | Field | Meaning |
| ---: | --- | --- |
| 0 | `version` | Format version; legacy `3`. |
| 4 | `checksum` | IEEE CRC-32 of the entire artifact with bytes 4–7 treated as zero. |
| 8 | `constPoolSize` | Constant-pool size; currently `0`. |
| 12 | `numRules` | Number of rule starts and rule-execution-index entries. |
| 16 | `ruleExecIndexOffset` | Start of the rule execution index. |
| 20 | `factRuleIndexOffset` | Start of the fact-to-rule lookup index. |
| 24 | `factDepIndexOffset` | Start of the fact dependency index. |

The sections following the header are, in order:

1. The instruction stream, from byte 28 to `ruleExecIndexOffset`. Instruction
   strings use a one-byte length prefix and therefore cannot exceed 255 bytes.
2. The rule execution index, from `ruleExecIndexOffset` to
   `factRuleIndexOffset`. It contains exactly `numRules` entries, each encoded
   as a length-prefixed rule name followed by an instruction-stream byte
   offset and the rule priority.
3. The fact-to-rule lookup index, from `factRuleIndexOffset` to
   `factDepIndexOffset`. Each entry contains a fact name, a rule count, and
   that many rule names.
4. The fact dependency index, from `factDepIndexOffset` to end of file. Each
   entry contains a rule name, a fact count, and that many fact names.

Index strings use a four-byte length prefix. The runtime validates header
length, version, constant-pool size, checksum, section ordering, instruction
boundaries, index records, and declared rule count before execution. A failed
validation is an invalid-artifact error, never a best-effort load.

Conditional jumps use an unsigned four-byte forward offset measured from the
end of the jump instruction. A valid destination must be inside the
instruction stream, start an instruction, and immediately follow a `LABEL`
instruction. These constraints match the control flow emitted by `rexc` and
prevent a corrupted jump from entering an operand or bypassing its intended
branch boundary. The compiler encodes the distance from the start of the jump
instruction to the start of the `LABEL`; because both instructions occupy five
bytes, the runtime's end-of-jump-relative calculation resumes immediately
after that label. A jump and its destination must also belong to the same rule.

The v3 checksum detects accidental corruption; it is not a signature or an
authenticity mechanism. Do not treat an artifact as trusted merely because its
CRC matches.

### Version-2 migration

Version 3 adds priority to each rule-execution-index entry and defines candidate
execution order as ascending priority with stable ruleset order for ties.
Version 2 did not honor priority when selecting candidate order. Retain the JSON
ruleset and recompile it with the current `rexc`; do not rewrite the version
field or attempt an in-place index conversion.

## Compatibility contract

Within a format version, Rex preserves the meaning and binary layout of all
documented fields and opcodes. The legacy compiler API produces deterministic v3
artifacts for the same parsed ruleset: map-derived index data is sorted before
serialization. This reproducibility is useful for review and
deployment, but it is not a promise that a future *format version* will be
byte-identical.

Any change that alters the on-disk layout, checksum coverage, opcode encoding,
index interpretation, or execution meaning of an existing artifact **must**
increase the bytecode version. A new version must not reuse an old version
number to mean something different.

Changes that only improve compiler diagnostics, validation, or implementation
internals may retain the version when they neither change valid serialized
bytes nor alter their meaning. If there is uncertainty, create a new format
version and retain an explicit reader for the old version only when supporting
existing deployed artifacts is a release requirement.

M6 is a documented support narrowing: v3 script opcodes are rejected instead of
being assigned new meaning. Script-free v3 bytes and execution remain unchanged.
This exception does not make reserved opcodes available for reuse.

`compiler.GenerateBytecode` returns an error that callers must check. Label
resolution is an internal compiler step, so unresolved control-flow labels
cannot be reported as successfully compiled artifacts.

## Changing the format

For every new format version:

1. Update the compiler's `Version` constant and this document, including a
   migration note.
2. Add a validating runtime decoder for the new version before execution. Keep
   prior decoders only for versions the release explicitly supports.
3. Add tests for the new writer, supported-version fixtures, rejection of
   unsupported versions, corruption, truncation, and deterministic output.
4. State supported compiler/runtime and artifact versions in the release notes.
   If older artifacts are unsupported, instruct operators to recompile from
   the retained JSON source.
5. Treat artifact compatibility as a release-level decision. Do not mix a
   format change with unrelated dependency or runtime refactors.

## Operational guidance

- Deploy bytecode with its source ruleset and record the `rexc` release that
  produced it.
- Recompile artifacts when upgrading across a bytecode-format boundary, then
  validate them in a staging runtime before rollout.
- Verify file delivery independently when authenticity matters (for example,
  with a signed release or a separately authenticated digest). CRC-32 only
  protects against accidental corruption.
- A future format can add a magic prefix, generator metadata, and a signed
  artifact manifest. Those additions require a new version; they cannot be
  inferred safely by a v3 runtime.
