# Change-only emission

Add `"emit": "on_change"` to a rule when matching events should write only when the declared output differs from stored state:

```json
{
  "rules": [
    {
      "name": "high-temperature-alert",
      "emit": "on_change",
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

The first matching event writes `alert` when it is missing or has another value. Later matches still fire and appear in traces, but REX marks their equal actions `suppressed`; each suppressed action contributes no write, publication, or derived fact. Omit `emit` for commands or notifications whose repeated delivery is intentional.

Suppression compares exact scalar type and value against the persisted target fact. It survives restart without private state. If another writer changes `alert`, the next match writes `true` again. When several matching rules propose the same target, REX suppresses the coalesced write only if every writer uses `on_change`. Conflicting values still reject the round.

A ruleset using this option compiles as v7. V7 carries compiler-owned capability metadata and can compose change-only emission with typed declarations and temporal `for` conditions. A temporal rule remains level-triggered after its deadline, while `on_change` suppresses equal public outputs; private timer maintenance still commits when needed.

Live REX evaluates against the snapshot exposed by its state adapter. Durable
processing and offline replay persist input facts before evaluation, matching
the usual Redis producer sequence of setting facts before publishing. A replay
event therefore cannot also contain a target of a change-only rule: the tooling
rejects that ambiguous case because it cannot represent the target's value before
the event was persisted. Test an externally changed target through
`initial_state` in a separate scenario.

Adding `emit` changes the artifact digest. When it is added to an existing
temporal rule, that rule receives new private timer keys and its current `for`
window restarts on activation; normal artifact-history cleanup retires the old
keys.

Validate and replay the included example:

```sh
rexc validate -rules examples/m8/change-only-rules.json
rexc bundle -rules examples/m8/change-only-rules.json \
  -scenario examples/m8/change-only-scenario.json > /tmp/rex-change-only-bundle.json
rexc simulate -bundle /tmp/rex-change-only-bundle.json
```

In scenario `initial_state`, an omitted target means explicitly missing and causes the first matching event to emit. Suppressed actions continue to consume the normal action, work, and staged-byte budgets.

Durable input events also reject change-only output targets before `ApplyInput`
can persist any fact. These invalid inputs follow the configured bounded retry
and dead-letter policy. Pub/Sub and direct evaluation retain their snapshot
semantics. Output targets alone do not select rules: an event affecting a
condition fact must arrive before REX evaluates whether to repair external drift.
