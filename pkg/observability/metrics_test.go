package observability

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlerReportsLivenessAndReadiness(t *testing.T) {
	metrics := NewMetrics()
	handler := metrics.Handler()

	live := httptest.NewRecorder()
	handler.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, live.Code)
	assert.JSONEq(t, `{"status":"ok"}`, live.Body.String())

	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusServiceUnavailable, ready.Code)
	assert.JSONEq(t, `{"status":"not_ready"}`, ready.Body.String())

	metrics.SetReady(true)
	ready = httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusOK, ready.Code)
	assert.JSONEq(t, `{"status":"ready"}`, ready.Body.String())
}

func TestMetricsHandlerReportsRuntimeCounters(t *testing.T) {
	metrics := NewMetrics()
	metrics.RecordEvent(20*time.Millisecond, nil)
	metrics.RecordEvent(30*time.Millisecond, errors.New("bad event"))
	metrics.RuleFired("temperature_rule")
	metrics.ActionSucceeded("updateStore")
	metrics.ActionSkipped("updateStore")
	metrics.ActionFailed("updateStore", errors.New("redis unavailable"))

	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)

	body := response.Body.String()
	assert.Contains(t, body, "rex_events_received_total 2")
	assert.Contains(t, body, "rex_event_failures_total 1")
	assert.Contains(t, body, "rex_event_processing_seconds_total 0.050000000")
	assert.Contains(t, body, "rex_event_processing_seconds_average 0.025000000")
	assert.Contains(t, body, "rex_rules_fired_total 1")
	assert.Contains(t, body, "rex_actions_succeeded_total 1")
	assert.Contains(t, body, "rex_actions_skipped_total 1")
	assert.Contains(t, body, "rex_action_failures_total 1")
	assert.Contains(t, body, "rex_event_queue_lag_seconds NaN")
}
