# Typed fact migration

Typed facts are optional. An existing ruleset without a `facts` member continues
to compile as byte-for-byte compatible v4. Add declarations when you are ready
to opt into the v5 closed input contract.

```json
{
  "facts": {
    "temperature": {"type": "number"},
    "note": {"type": "string", "nullable": true},
    "alert": {"type": "boolean"}
  },
  "rules": [
    {
      "name": "high-temperature",
      "conditions": {
        "all": [
          {"fact": "temperature", "operator": "GTE", "value": 30}
        ]
      },
      "actions": [
        {"type": "updateStore", "target": "alert", "value": true}
      ]
    }
  ]
}
```

Declare every producer input, condition dependency, and action target. Unused
declarations are allowed, which makes staged producer migrations possible.
Choose among `number`, `string`, and `boolean`; set `nullable` only when explicit
JSON null is part of the producer contract. Omitted facts need no nullable flag.

Validate the rules and a representative scenario before deployment:

```sh
rexc validate -rules examples/m8/typed-rules.json
rexc bundle -rules examples/m8/typed-rules.json \
  -scenario examples/m8/typed-scenario.json > /tmp/rex-typed-bundle.json
rexc simulate -bundle /tmp/rex-typed-bundle.json
```

The compiler rejects declaration mismatches in condition constants and action
values. Simulation rejects bad initial state and events with the same runtime
diagnostics. Live Pub/Sub and Streams inputs are checked before REX commits
state; durable Streams inputs are checked before their input journal write.

Plan producer rollout around the closed namespace. First inventory all emitted
fact names and types, add declarations for the complete set, and exercise recorded
traffic through simulation. Then publish the v5 artifact through the normal M8.1
atomic reload path. A rejected event is not coerced. Fix the producer or restore
the previous artifact; reverting to archived v4 bytes restores the earlier open
namespace for later events but does not alter already committed facts.

Stored data written before migration can still contain an incompatible value.
REX reports that dependency as invalid and evaluates its condition as unknown.
Repair or migrate that stored fact explicitly before depending on it to fire a
rule.
