# M6 script removal and migration

REX no longer compiles or executes JavaScript. This applies to default v4,
explicit legacy v3, embedded compiler APIs, and daemon artifact loading.

## Detect affected rulesets

An affected source contains either a `scripts` member on a rule or an action
value whose first and last characters are braces. A `scripts` value of `null`
or `{}` is still a declaration and is rejected. Brace-form strings such as
`{name}`, `{}`, and `{literal text}` were script calls under the legacy encoder;
they are all rejected now. There is no escape form. Producers must rename or
restructure a constant that needs leading and trailing braces.

Current `rexc` reports the rule name and, for a call, the action index. `rexd`
also rejects an older v3 artifact containing `SCRIPT_DEF` or `SCRIPT_CALL`, even
if `engine.scripts_enabled` is false.

Run compilation in validation mode before deployment:

```sh
rexc -rules rules.json -validate
```

Also remove `engine.scripts_enabled: true` from daemon configuration. Leaving it
set to `false` is supported so existing script-free configurations need not
change immediately.

Embedded callers using `SetScriptsEnabled` must check its returned error. A
statement that ignores the return value still compiles, but a stale `true` call
does not enable any capability.

## Replace a calculation

Compute the value in the event producer and publish it as an ordinary fact, or
represent the decision directly with declarative conditions and constant
actions. For example, replace an action that writes
`"value": "{calculate_heat_index}"` with a producer-supplied `heat_index` fact,
then use that fact in rule conditions. Recompile retained source after removing
the script field and every brace-form call.

Script-free v3 artifacts remain loadable when `engine.allow_legacy_v3` is true.
Prefer compiling the migrated source to v4 so it receives batch snapshot,
conflict, staged commit, and bounded derived-round semantics.

## Rollback

A rollback to a pre-M6 binary can load the previous script artifact, but restores
the old in-process Otto behavior and its inability to stop or isolate hostile or
runaway code. Preserve the migrated JSON source and generated artifact together;
do not edit a bytecode version or opcode to bypass rejection.
