# REX-M8.5 — Partition readiness

This milestone supplies an offline ownership analysis and a current batch
performance baseline. It does not enable concurrent workers. M7's one ordered
owner per durable namespace remains the supported execution contract.

## Inspect potential boundaries

```sh
go run ./cmd/rexc partition-plan -rules examples/m8-partitions/rules.json
# Or use the exact artifact retained for deployment/recovery:
go run ./cmd/rexc partition-plan -artifact /path/to/rules.bytecode
```

The example produces two groups: `east-hot` and `east-fan` share the derived
`east.alert` fact; `west-hot` is independent in this artifact. The prefixes are
only readable names, not routing configuration.

The command accepts exactly one source or artifact, supports batch v4–v7,
requires no Redis, and emits deterministic JSON. Exit 0 means analysis
succeeded, even if the entire artifact is one group; exit 1 means invalid input
or I/O failure. `advisory_only: true` is always present. Schema version 1 includes
the artifact SHA-256 and execution contract, groups, and unused typed fact
declarations. Group IDs are zero-based in first-rule source order; rules remain
in source order and fact names are sorted. IDs are local to this report and
must not be used as persistent routing or recovery identities.

Each rule joins every condition fact and every action target into one group.
Groups sharing any fact merge transitively. This covers nested conditions,
derived chains, common writers, shared readers, temporal predicate facts, and
change-only target reads. Even a fact used only as a read dependency is mutable
under the current contract; there is no immutable/shared-read exemption.
Unused declarations are reported separately, without assigning ownership.
No condition satisfiability assumptions or name-prefix heuristics are used.

Private timer keys are not public routing facts. Keeping a whole rule in one
group keeps its timers conceptually with its owner, but this report does not
assign or migrate existing timer keys. Timer keys currently depend on the
program digest, not the durable namespace: two namespaces running the same
artifact can share timers, and retiring one copy can delete the other
partition's state. Separate namespaces alone are not safe isolation.
All retained rollback and durable-retry
artifacts must be considered before changing ownership; a plan for the active
artifact alone cannot prove a safe deployment.

## Remaining concurrency gates

Before enabling multiple workers, a separate milestone must establish:

- Explicit, enforced ownership of every input, read dependency, action target,
  and private timer key, including otherwise unused/untyped producer facts.
  Different durable namespaces do not isolate public Redis fact keys.
- Routing that rejects cross-owner events before persisting input. Splitting a
  multi-fact event would change atomic batch/conflict semantics and is not an
  implicit permission granted by this report.
- Disjoint input/output/dead-letter protocol keys and one fenced owner per
  partition, with per-partition ordering and bounded backpressure.
- Recovery, retained-artifact compatibility, reload and rollback validation,
  and an explicit quiescence/migration procedure for ownership changes.
- Representative durable Redis measurements, skewed workloads and p95/p99
  latency, followed by race, owner-loss, retry, failure and load tests showing
  improvement without semantic regression. The offline memory benchmark is
  not evidence of production durable throughput or parallel speedup.

The next concrete step is a representative durable profiling harness and an
explicit ownership/routing contract. Only then should a worker implementation
be selected. Additional transports and external actions remain later M8 items.

See [acceptance evidence](baselines/rex-m8.5/README.md).
