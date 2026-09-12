# D13 — Measure concurrent partitions before building a supervisor

Accepted for the REX-M8.8 experiment, 2026-09-10.

Run one serial processor per exact-ownership partition in an opt-in test harness
before adding a production multi-partition supervisor. Each partition receives
its own Redis store, engine, durable queue, protocol streams, immutable fact
claim, and lease. The partitions share only one explicitly owned Redis process
and the measurement process. One worker is the control; two and four workers are
the experimental cases.

The experiment preserves per-partition order and uses existing M7 recovery and
M8.6 ownership fencing. Any processing failure fails the case; the two fault
scenarios deliberately recover a post-commit completion failure or one expired
lease and then verify exact terminal state. A future production supervisor must
instead define process-wide cancellation, readiness, lease release, bounded
shutdown, metrics labels, and fatal-versus-recoverable failure policy.

The owner-loss case expires the lease after the first event's effects commit. It
proves that the stale owner cannot complete or reprocess that pending event and
that a successor can recover it while sibling processors run. M8.6 separately
proves stale-commit fencing. The experiment does not run daemon-style lease
renewers, so command and CPU results characterize processor-loop scaling rather
than full daemon overhead.

Production configuration, routing, reload and rollback, online ownership
migration, dynamic partition counts, in-process restart, and temporal artifacts
are excluded. Reload currently coordinates one manager and one queue. Temporal
keys retain artifact identity rather than an independent partition identity;
the exact disjoint-artifact experiment does not establish safe shared-artifact
timer cleanup. Those contracts require a separate decision before production
workers can be enabled.

The one-worker control must first have a throughput coefficient of variation no
greater than 20%; a noisier control makes the decision inconclusive. Given a
stable control, the primary performance gate is at least 1.5x median paired
sparse- and dense-balanced throughput for two workers versus one across five
shuffled repetitions. Two-worker service p99 must not exceed the observed
one-worker maximum, the 90/10 two-worker hot-partition p99 must not exceed the
one-worker maximum, and non-fault per-event command counts must remain exact.
Finite-backlog completion time is diagnostic and is not an arrival-latency gate.
Four-worker data diagnoses scaling and Redis CPU saturation but is not a
substitute for the two-worker gate. If a stable run fails, record the evidence
and keep the daemon single-partition.

Claude recommended this experimental split after identifying the single durable
store, manager, reloader, readiness, and signal assumptions in the current
daemon. The recommendation is adopted. Its suggested bounded production
configuration is deferred because M8.7 did not establish a stable capacity
baseline and the experiment must first show that shared-Redis concurrency helps.
Claude's implementation review also led to explicit stability gating, processing
overlap evidence, fixed per-partition warmup, lease-removal checks, stronger
fault assertions, host CPU/load provenance, and processor-loop-only wording.
