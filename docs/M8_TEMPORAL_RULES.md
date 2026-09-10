# Temporal rules

Add `for` to a condition leaf when the predicate must remain true before a rule
can fire:

```json
{
  "rules": [
    {
      "name": "sustained-high-temperature",
      "conditions": {
        "all": [
          {"fact": "temperature", "operator": "GTE", "value": 30, "for": "5m"}
        ]
      },
      "actions": [
        {"type": "updateStore", "target": "alert", "value": true}
      ]
    }
  ]
}
```

The first qualifying reading starts the five-minute interval. A qualifying event
before five minutes does not fire. A qualifying event at or after five minutes
fires. Any observed nonqualifying, missing, null, or invalid value resets the
interval. REX is event driven, so it does not wake itself when five minutes pass;
another event affecting the rule must arrive.

After the deadline, the condition is level-triggered: every later qualifying
event fires again until a reset. Add `emit: "on_change"` to suppress an equal
persisted output; this composes the temporal rule into v7.

`for` accepts positive Go durations such as `250ms`, `30s`, `5m`, or `24h`, up
to 365 days. A ruleset containing it compiles as v6. It can also use v5-style
typed declarations. The compiler allows at most 10,000 temporal conditions and
reserves fact names beginning with `__rex_temporal_` across all rulesets and
external inputs.

Validate and replay the included example:

```sh
rexc validate -rules examples/m8/temporal-rules.json
rexc bundle -rules examples/m8/temporal-rules.json \
  -scenario examples/m8/temporal-scenario.json > /tmp/rex-temporal-bundle.json
rexc simulate -bundle /tmp/rex-temporal-bundle.json
```

Every event in a temporal scenario needs an RFC 3339 `at` value. Values must be
nondecreasing and represent processing time. Live operation samples the runtime
clock once per input chain; embedded callers can replace it with `Engine.SetClock`.
Producer timestamps are ordinary facts and do not
change timers; v6 does not implement event-time windows or late-event correction.

Timer starts are private state. They survive restart and reload of the same exact
artifact, but do not appear in output notifications or public state snapshots.
Changing the artifact gives its temporal leaves new identities, so deploy rule
changes as new timing intervals. Restoring exact archived bytes reconnects to
their prior timer keys. REX retains those keys while the artifact remains in M8.1
history and removes them when the artifact is pruned from both in-memory and disk
history. Active timers deliberately have no TTL because a delayed qualifying
event must still observe the original deadline.

Set the durable `JournalTTL` longer than the maximum retry and recovery window.
Pinned processing time, program selection, and historical snapshots share that
retention boundary.
