# D12 — Exact immutable partition ownership

Accepted for local M8.6 implementation, 2026-09-10; integration pending.

Choose opt-in exact public fact ownership for Streams, with all reads and writes
local to the owner. Include all typed declarations and retained artifacts.
Reject partial input batches before persistence. An incompatible historical
program is an infrastructure/reconciliation issue, not poison.

Use a bounded persistent registry with canonical per-namespace claims covering
public facts and protocol streams. Reserve before creating consumer groups,
reject overlaps atomically, and guard managed effects with the current lease
and immutable registry claim. Claims never expire or shrink automatically.
Timer keys retain existing artifact identity: complete disjoint public
footprints exclude shared timed artifacts, and cleanup is lease-guarded.

Claude recommended ownership as a standalone milestone, with durable profiling
next. We adopt that split. We deliberately choose stricter immutable claims
instead of its online shrink/release proposal, and whole-domain legacy/managed
exclusion instead of a one-time legacy overlap probe. A probe alone would not
stop a legacy writer already running when a later claim was acquired. A single persistent legacy-mode marker has a documented metadata compatibility
cost without accumulating records for retired legacy namespaces. Older binaries,
Pub/Sub and direct writers require operational exclusion at initial adoption.

This does not establish a benefit from parallel execution. M8.7 must measure
representative durable latency/throughput and failure behavior before choosing
more workers. The [operator guide](../M8_PARTITION_OWNERSHIP.md) defines the
migration and retained-state boundaries; no artifact version changes.
