# Redis Compose demo

This demo starts Redis, compiles a sample ruleset, runs `rexd`, publishes a
sample JSON fact event, and checks the resulting fact. It is self-contained:
no local Redis, Go installation, or compiled bytecode is required.

From the repository root, run:

```bash
docker compose -f demo/compose.yaml up --build --abort-on-container-exit --exit-code-from smoke
```

The `smoke` service seeds `temperature` with `35`, publishes
[`events/temperature-high.json`](events/temperature-high.json) to
`rex_updates`, and waits for the `high_temperature` rule to write
`demo:status` as the JSON value `"hot"`. A zero exit code means the complete
compiler -> Redis -> runtime -> action path succeeded.

Clean up the isolated containers and network with:

```bash
docker compose -f demo/compose.yaml down
```

The demo keeps scripts disabled, matching the safe default for `rexd`.
