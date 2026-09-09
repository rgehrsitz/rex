// Package observability provides lightweight health and metrics support for
// long-running Rex processes without requiring a metrics backend dependency.
package observability

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var eventLatencyBounds = [...]time.Duration{
	time.Millisecond, 5 * time.Millisecond, 10 * time.Millisecond,
	25 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond,
	250 * time.Millisecond, 500 * time.Millisecond, time.Second,
}

// Metrics collects process-local runtime measurements. Its methods are safe
// for concurrent use by the Redis consumer and the HTTP metrics handler.
type Metrics struct {
	ready                atomic.Bool
	readinessMu          sync.Mutex
	redisReady           bool
	subscriptionReady    bool
	eventsReceived       atomic.Uint64
	eventFailures        atomic.Uint64
	eventProcessingNanos atomic.Uint64
	rulesFired           atomic.Uint64
	actionsSucceeded     atomic.Uint64
	actionsSkipped       atomic.Uint64
	actionFailures       atomic.Uint64
	eventLatencyBuckets  [len(eventLatencyBounds) + 1]atomic.Uint64
	redisDisconnects     atomic.Uint64
	redisReconnects      atomic.Uint64
	eventSourceErrors    atomic.Uint64
}

// NewMetrics initializes an empty metrics collector.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// SetReady records whether the daemon has an active event subscription.
func (m *Metrics) SetReady(ready bool) {
	m.ready.Store(ready)
}

// Ready reports the current readiness state.
func (m *Metrics) Ready() bool { return m.ready.Load() }

// SetRedisReady updates Redis command connectivity and reports whether overall
// readiness changed.
func (m *Metrics) SetRedisReady(ready bool) bool {
	m.readinessMu.Lock()
	defer m.readinessMu.Unlock()
	m.redisReady = ready
	return m.recomputeReady()
}

// SetSubscriptionReady updates event-subscription connectivity and reports
// whether overall readiness changed.
func (m *Metrics) SetSubscriptionReady(ready bool) bool {
	m.readinessMu.Lock()
	defer m.readinessMu.Unlock()
	m.subscriptionReady = ready
	return m.recomputeReady()
}

func (m *Metrics) recomputeReady() bool {
	ready := m.redisReady && m.subscriptionReady
	return m.ready.Swap(ready) != ready
}

// RecordRedisDisconnect records a transition from connected to disconnected.
func (m *Metrics) RecordRedisDisconnect() { m.redisDisconnects.Add(1) }

// RecordRedisReconnect records a transition from disconnected to connected.
func (m *Metrics) RecordRedisReconnect() { m.redisReconnects.Add(1) }

// RecordEventSourceError records an error reported by the subscription transport.
func (m *Metrics) RecordEventSourceError() { m.eventSourceErrors.Add(1) }

// RecordEvent records the result and elapsed time of processing one incoming
// event, which may contain multiple fact updates.
func (m *Metrics) RecordEvent(elapsed time.Duration, err error) {
	m.eventsReceived.Add(1)
	m.eventProcessingNanos.Add(uint64(elapsed))
	bucket := len(eventLatencyBounds)
	for i, bound := range eventLatencyBounds {
		if elapsed <= bound {
			bucket = i
			break
		}
	}
	m.eventLatencyBuckets[bucket].Add(1)
	if err != nil {
		m.eventFailures.Add(1)
	}
}

// RuleFired records a rule evaluation that reached at least one action.
func (m *Metrics) RuleFired(string) {
	m.rulesFired.Add(1)
}

// ActionSucceeded records a completed action.
func (m *Metrics) ActionSucceeded(string) {
	m.actionsSucceeded.Add(1)
}

// ActionSkipped retains the observer metric contract. Current engines do not
// emit this outcome after M6 removed scripting.
func (m *Metrics) ActionSkipped(string) {
	m.actionsSkipped.Add(1)
}

// ActionFailed records an action that returned an error.
func (m *Metrics) ActionFailed(string, error) {
	m.actionFailures.Add(1)
}

// Handler exposes liveness, readiness, and Prometheus text-format metrics.
func (m *Metrics) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeHealth(w, http.StatusOK, "ok")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !m.ready.Load() {
			writeHealth(w, http.StatusServiceUnavailable, "not_ready")
			return
		}
		writeHealth(w, http.StatusOK, "ready")
	})
	mux.HandleFunc("GET /metrics", m.writeMetrics)
	return mux
}

func writeHealth(w http.ResponseWriter, status int, value string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"status":%q}`+"\n", value)
}

func (m *Metrics) writeMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	events := m.eventsReceived.Load()
	processingSeconds := float64(m.eventProcessingNanos.Load()) / float64(time.Second)

	_, _ = fmt.Fprintf(w, `# HELP rex_events_received_total Number of Redis events received by rexd.
# TYPE rex_events_received_total counter
rex_events_received_total %d
# HELP rex_event_failures_total Number of Redis events that failed processing.
# TYPE rex_event_failures_total counter
rex_event_failures_total %d
# HELP rex_event_processing_seconds_total Total time spent processing Redis events.
# TYPE rex_event_processing_seconds_total counter
rex_event_processing_seconds_total %.9f
# HELP rex_event_processing_seconds_average Average processing time per Redis event.
# TYPE rex_event_processing_seconds_average gauge
rex_event_processing_seconds_average %.9f
# HELP rex_rules_fired_total Number of rule evaluations that reached an action.
# TYPE rex_rules_fired_total counter
rex_rules_fired_total %d
# HELP rex_actions_succeeded_total Number of actions completed successfully.
# TYPE rex_actions_succeeded_total counter
rex_actions_succeeded_total %d
# HELP rex_actions_skipped_total Compatibility counter for skipped actions; current engines do not emit this outcome.
# TYPE rex_actions_skipped_total counter
rex_actions_skipped_total %d
# HELP rex_action_failures_total Number of actions that failed.
# TYPE rex_action_failures_total counter
rex_action_failures_total %d
# HELP rex_event_queue_lag_seconds Redis Pub/Sub does not expose queue lag; this metric is unavailable.
# TYPE rex_event_queue_lag_seconds gauge
rex_event_queue_lag_seconds NaN
# HELP rex_event_queue_drops Redis Pub/Sub does not expose broker-side drops; this metric is unavailable.
# TYPE rex_event_queue_drops gauge
rex_event_queue_drops NaN
# HELP rex_redis_disconnects_total Number of observed Redis connectivity losses.
# TYPE rex_redis_disconnects_total counter
rex_redis_disconnects_total %d
# HELP rex_redis_reconnects_total Number of observed Redis connectivity recoveries.
# TYPE rex_redis_reconnects_total counter
rex_redis_reconnects_total %d
# HELP rex_event_source_errors_total Number of errors reported by the event subscription.
# TYPE rex_event_source_errors_total counter
rex_event_source_errors_total %d
# HELP rex_rule_outcomes_total Rule outcomes with a fixed, bounded label set.
# TYPE rex_rule_outcomes_total counter
rex_rule_outcomes_total{outcome="fired"} %d
# HELP rex_action_outcomes_total Action outcomes with a fixed, bounded label set.
# TYPE rex_action_outcomes_total counter
rex_action_outcomes_total{outcome="succeeded"} %d
rex_action_outcomes_total{outcome="skipped"} %d
rex_action_outcomes_total{outcome="failed"} %d
`, events, m.eventFailures.Load(), processingSeconds, average(processingSeconds, events), m.rulesFired.Load(), m.actionsSucceeded.Load(), m.actionsSkipped.Load(), m.actionFailures.Load(), m.redisDisconnects.Load(), m.redisReconnects.Load(), m.eventSourceErrors.Load(), m.rulesFired.Load(), m.actionsSucceeded.Load(), m.actionsSkipped.Load(), m.actionFailures.Load())

	_, _ = fmt.Fprintln(w, "# HELP rex_event_processing_duration_seconds Time spent processing Redis events.")
	_, _ = fmt.Fprintln(w, "# TYPE rex_event_processing_duration_seconds histogram")
	cumulative := uint64(0)
	for i, bound := range eventLatencyBounds {
		cumulative += m.eventLatencyBuckets[i].Load()
		_, _ = fmt.Fprintf(w, "rex_event_processing_duration_seconds_bucket{le=\"%.3f\"} %d\n", bound.Seconds(), cumulative)
	}
	cumulative += m.eventLatencyBuckets[len(eventLatencyBounds)].Load()
	_, _ = fmt.Fprintf(w, "rex_event_processing_duration_seconds_bucket{le=\"+Inf\"} %d\n", cumulative)
	_, _ = fmt.Fprintf(w, "rex_event_processing_duration_seconds_sum %.9f\n", processingSeconds)
	_, _ = fmt.Fprintf(w, "rex_event_processing_duration_seconds_count %d\n", events)
}

func average(total float64, count uint64) float64 {
	if count == 0 {
		return 0
	}
	return total / float64(count)
}
