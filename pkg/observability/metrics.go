// Package observability provides lightweight health and metrics support for
// long-running Rex processes without requiring a metrics backend dependency.
package observability

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// Metrics collects process-local runtime measurements. Its methods are safe
// for concurrent use by the Redis consumer and the HTTP metrics handler.
type Metrics struct {
	ready                atomic.Bool
	eventsReceived       atomic.Uint64
	eventFailures        atomic.Uint64
	eventProcessingNanos atomic.Uint64
	rulesFired           atomic.Uint64
	actionsSucceeded     atomic.Uint64
	actionsSkipped       atomic.Uint64
	actionFailures       atomic.Uint64
}

// NewMetrics initializes an empty metrics collector.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// SetReady records whether the daemon has an active event subscription.
func (m *Metrics) SetReady(ready bool) {
	m.ready.Store(ready)
}

// RecordEvent records the result and elapsed time of processing one incoming
// event, which may contain multiple fact updates.
func (m *Metrics) RecordEvent(elapsed time.Duration, err error) {
	m.eventsReceived.Add(1)
	m.eventProcessingNanos.Add(uint64(elapsed))
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

// ActionSkipped records an intentionally skipped action.
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
# HELP rex_actions_skipped_total Number of actions intentionally skipped.
# TYPE rex_actions_skipped_total counter
rex_actions_skipped_total %d
# HELP rex_action_failures_total Number of actions that failed.
# TYPE rex_action_failures_total counter
rex_action_failures_total %d
# HELP rex_event_queue_lag_seconds Redis Pub/Sub does not expose queue lag; this metric is unavailable.
# TYPE rex_event_queue_lag_seconds gauge
rex_event_queue_lag_seconds NaN
`, events, m.eventFailures.Load(), processingSeconds, average(processingSeconds, events), m.rulesFired.Load(), m.actionsSucceeded.Load(), m.actionsSkipped.Load(), m.actionFailures.Load())
}

func average(total float64, count uint64) float64 {
	if count == 0 {
		return 0
	}
	return total / float64(count)
}
