# REX-M8.6 — Enforced partition ownership

Streams deployments can opt into an immutable exact-name ownership policy.
This is a correctness boundary before further concurrency work, not an
in-process worker pool or a throughput claim. Durable profiling is M8.7.

## Configure and check

Add to the existing durable configuration:

```json
{
  "redis": {
    "event_mode": "streams",
    "durable": {
      "namespace": "east",
      "stream": "east.inputs",
      "group": "rex",
      "consumer": "worker-1",
      "output_stream": "east.outputs",
      "dead_letter_stream": "east.dead",
      "ownership": {"facts": ["east.temperature", "east.alert", "east.fan"]}
    }
  }
}
```

Ownership is optional. Omit `ownership.facts` for legacy behavior; an explicit
empty list is rejected. Names are exact, case-sensitive strings, with no prefix,
wildcard or shared-read semantics. A comma inside a fact name stays literal.
Lists reject duplicates, empty/reserved names, names longer than 1,024 bytes,
and more than 100,000 entries. The serialized claim has a stricter 64 KiB bound;
there are at most 256 managed namespaces in a Redis database.

All condition reads, action writes, change-only target reads and typed fact
declarations (including unused declarations) must be included for the active
artifact and every retained recovery/rollback artifact. Extra input-only facts
are allowed. An event with any unowned input is rejected before any input facts
are written, then follows the existing retry/dead-letter policy. Whole events
are never split across owners. A changed ruleset is validated before activation;
invalid retained history fails initialization. A pinned artifact incompatible
with ownership remains pending as an infrastructure failure, never poison.

For an offline check, save `{"facts":["east.temperature","east.alert","east.fan"]}`
as `ownership.json` and run:

```sh
go run ./cmd/rexc partition-check -rules rules.json -ownership ownership.json
# Or substitute -artifact rules.bytecode for -rules rules.json.
```

The command supports batch v4–v7 and uses the runtime's full public footprint.
It emits schema-1 diagnostics: exit 0 for coverage, exit 2 with `REX-P001` for
unowned facts, exit 1 for invalid source/artifact/specification or I/O errors.
Unlike `partition-plan`, this check includes unused typed declarations. It checks
artifact coverage only; Redis deployment claims and protocol-key collisions are
validated when opening the durable adapter.

## Claims, leases and recovery

`rex:durable:ownership` stores one canonical JSON claim per namespace. Claims
include owned public facts, the three protocol stream keys, and the consumer
group. Redis ACLs must permit access to the registry as well as the existing
stream/journal keys. Fact/fact, fact/stream and stream/stream overlap between managed
namespaces is rejected atomically. Claim reservations precede stream creation,
so a rejected open cannot create a stream in another namespace's owned fact.
Input, output and dead-letter key types are checked before reservation.
An ACL/network failure after reservation may still leave a claim; retry the
identical configuration, or use the offline recovery procedure. Claims are not
automatically released on an uncertain outcome.

Claims outlive lease expiry, process exit, stream trimming and journal expiry.
Restarting with the same namespace, policy, streams and group is idempotent;
the consumer name may change. Changing or shrinking a managed claim, or turning
it off, requires offline administration. First enable on an older unregistered
partition is refused if its group has pending events or an active owner lease.

Managed input and output transactions watch the owner lease and registry,
verify the owner token and immutable claim, and fail if either changes before
`EXEC`. Managed optimistic conflicts retry with a fresh ownership check
(up to eight attempts for renewal, snapshots, cleanup and terminal operations;
existing begin/input/commit loops retain three). Unchanged claims do not rewrite
the registry, and unchanged/legacy acquisition does not watch producer streams.
Begin, terminal completion, dead-lettering, acknowledgement, snapshot
recording and private timer cleanup are likewise guarded. Lease loss and
ownership failures in snapshot/output operations are infrastructure failures.
Public ownership is checked before historical snapshots/commit markers can
bypass validation. Compiled private timer writes remain a trusted runtime
operation, not producer-addressable public facts.

Timer keys are unchanged. Every temporal rule reads a public fact, so complete,
disjoint claims prevent two managed namespaces from retaining the same temporal
artifact. Claims cannot shrink while an old artifact might still be needed.
Lease-guarded timer cleanup also protects a successor within the same namespace.

## Deployment and migration boundary

All upgraded legacy durable adapters install one persistent `:` hash field
with value `legacy`, regardless of how many namespaces are opened. This field
cannot collide with a valid namespace and does not grow per deployment. Legacy
partitions can coexist with other legacy partitions as before, but any mixture
of managed claims and the legacy-mode marker is rejected. Legacy deployments
therefore gain bounded registry metadata; metadata behavior is intentionally
changed even when fact ownership is omitted.
Give every managed partition distinct input, output and dead-letter stream
names. The default output/dead-letter names are shared defaults: a second
managed partition using them is intentionally rejected.

The protocol also reserves `rex:durable:` and the private timer prefix against
use as stream names. This prevents collision with registry/journal state.

**Use a dedicated Redis database/commit domain for managed partitions.** Before
initial adoption, stop older binaries, Pub/Sub engines, and direct writers in
that domain. They do not participate in this protocol and cannot be fenced by
these claims. This is application ownership, not Redis ACL isolation. The
supported Redis boundary remains standalone Redis 6.2+; Cluster is unsupported.

For a migration or decommission, stop every writer/owner in the domain and
wait for leases to expire. Preserve a backup of claims, facts, streams, journals
and all retained artifacts. Drain/reconcile pending and undelivered work under
the original contract. Retire the rollback/recovery history that uses facts
being reassigned, and explicitly migrate or retire its private timer state.
Only then may an operator remove the relevant hash field with
`HDEL rex:durable:ownership <namespace>` and deploy the replacement claim.
Initial conversion from a registered legacy domain requires removing its `:`
marker after the same domain-wide shutdown and reconciliation. First-enable
pending checks examine the configured stream/group only; they do not discover
old topologies or authorize changing streams during migration. Do not
remove claims merely because a process crashed or a lease expired. No online
claim transfer, shrink, automatic deletion or release command is provided.

See [decision D12](decisions/REX-M8-PARTITION-OWNERSHIP.md) and
[validation evidence](baselines/rex-m8.6/README.md).
