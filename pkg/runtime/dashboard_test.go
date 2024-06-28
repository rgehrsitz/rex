// rex/pkg/runtime/dashboard_test.go

package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// MockEngine is a mock implementation of the Engine struct
type MockEngine struct {
	stats map[string]interface{}
}

func (m *MockEngine) GetStats() map[string]interface{} {
	return m.stats
}

func TestNewDashboard(t *testing.T) {
	engine := &Engine{}
	port := 8080
	updateInterval := time.Second

	dashboard := NewDashboard(engine, port, updateInterval)

	assert.NotNil(t, dashboard)
	assert.Equal(t, engine, dashboard.engine)
	assert.Equal(t, port, dashboard.port)
	assert.Equal(t, updateInterval, dashboard.updateInterval)
	assert.NotNil(t, dashboard.clients)
}

func TestHandleHome(t *testing.T) {
	engine := &Engine{}
	dashboard := NewDashboard(engine, 8080, time.Second)

	req, err := http.NewRequest("GET", "/", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(dashboard.handleHome)

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "REX Dashboard")
}

func TestHandleStats(t *testing.T) {
	// mockStats := map[string]interface{}{
	// 	"TotalFactsProcessed": int64(100),
	// 	"TotalRulesProcessed": int64(50),
	// }
	mockEngine := &Engine{
		Stats: struct {
			TotalFactsProcessed int64
			TotalRulesProcessed int64
			TotalFactsUpdated   int64
			LastUpdateTime      time.Time
		}{
			TotalFactsProcessed: 100,
			TotalRulesProcessed: 50,
		},
	}

	dashboard := NewDashboard(mockEngine, 8080, time.Second)

	req, err := http.NewRequest("GET", "/api/stats", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(dashboard.handleStats)

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var stats map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &stats)
	assert.NoError(t, err)
	assert.Equal(t, float64(100), stats["TotalFactsProcessed"])
	assert.Equal(t, float64(50), stats["TotalRulesProcessed"])
}

func TestHandleSSE(t *testing.T) {
	engine := &Engine{}
	dashboard := NewDashboard(engine, 8080, time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "GET", "/events", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()

	// Use a channel to signal when the handler is done
	done := make(chan bool)

	go func() {
		dashboard.handleSSE(rr, req)
		done <- true
	}()

	// Wait a short time for the handler to set up
	time.Sleep(10 * time.Millisecond)

	// Check if a client was added
	dashboard.clientsMutex.Lock()
	assert.Equal(t, 1, len(dashboard.clients))
	dashboard.clientsMutex.Unlock()

	// Cancel the request context to terminate the handler
	cancel()

	// Wait for the handler to finish
	select {
	case <-done:
		// Handler finished successfully
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Handler did not finish in time")
	}

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "text/event-stream", rr.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", rr.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", rr.Header().Get("Connection"))

	// Check if the client was removed after the handler finished
	dashboard.clientsMutex.Lock()
	assert.Equal(t, 0, len(dashboard.clients))
	dashboard.clientsMutex.Unlock()
}

func TestBroadcastUpdates(t *testing.T) {
	mockEngine := &Engine{
		Stats: struct {
			TotalFactsProcessed int64
			TotalRulesProcessed int64
			TotalFactsUpdated   int64
			LastUpdateTime      time.Time
		}{
			TotalFactsProcessed: 100,
			TotalRulesProcessed: 50,
		},
	}

	dashboard := NewDashboard(mockEngine, 8080, 10*time.Millisecond)

	client := make(chan string, 1)
	dashboard.clients[client] = true

	// Start broadcasting in a goroutine
	go dashboard.broadcastUpdates()

	// Wait for a broadcast
	select {
	case msg := <-client:
		var stats map[string]interface{}
		err := json.Unmarshal([]byte(msg), &stats)
		assert.NoError(t, err)
		assert.Equal(t, float64(100), stats["TotalFactsProcessed"])
		assert.Equal(t, float64(50), stats["TotalRulesProcessed"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for broadcast message")
	}
}

// TestStart is removed as it's difficult to test the http.ListenAndServe call
// without significant changes to the Dashboard struct or using a third-party HTTP mocking library.
