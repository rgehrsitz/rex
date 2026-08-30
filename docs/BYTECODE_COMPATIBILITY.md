# Bytecode compatibility and versioning

Rex bytecode is a compiled artifact, not the source of record for a ruleset.
Keep the JSON ruleset, the `rexc` version, and any deployment configuration
alongside a bytecode file so the artifact can be rebuilt when the format
changes.

## Current support

The compiler writes **format version 2**. The runtime accepts **only version
2** and rejects every other version before it attempts to decode or execute an
artifact. In particular, version-1 artifacts must be recompiled from their
source ruleset; Rex does not provide a version-1 reader or an in-place
migrator.

The version is the first little-endian `uint32` in the file. There is
currently no magic-number prefix, so tools should use the version together
with successful structural validation and checksum verification to recognize a
Rex artifact.

## Version-2 format

All multi-byte integers are unsigned, little-endian 32-bit values unless a
section says otherwise. The fixed header is 28 bytes:

| Offset | Field | Meaning |
| ---: | --- | --- |
| 0 | `version` | Format version; currently `2`. |
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
   offset.
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
branch boundary.

The v2 checksum detects accidental corruption; it is not a signature or an
authenticity mechanism. Do not treat an artifact as trusted merely because its
CRC matches.

## Compatibility contract

Within a format version, Rex preserves the meaning and binary layout of all
documented fields and opcodes. A current compiler produces deterministic v2
artifacts for the same parsed ruleset: map-derived index and script data are
sorted before serialization. This reproducibility is useful for review and
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
  inferred safely by a v2 runtime.
