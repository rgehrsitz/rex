package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"rgehrsitz/rex/pkg/store"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
)

func setupTestEnvironment(t *testing.T) (*miniredis.Miniredis, *Engine, *Dashboard) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}

	redisStore := store.NewRedisStore(s.Addr(), "", 0)
	engine := createMockEngine(redisStore) // Use the createMockEngine function from engine_benchmark_test.go
	dashboard := NewDashboard(engine, 8080, time.Millisecond)

	return s, engine, dashboard
}

func TestNewDashboard(t *testing.T) {
	s, engine, dashboard := setupTestEnvironment(t)
	defer s.Close()

	assert.NotNil(t, dashboard)
	assert.Equal(t, engine, dashboard.engine)
	assert.Equal(t, 8080, dashboard.port)
	assert.Equal(t, time.Millisecond, dashboard.updateInterval)
	assert.NotNil(t, dashboard.clients)
}

func TestHandleHome(t *testing.T) {
	s, _, dashboard := setupTestEnvironment(t)
	defer s.Close()

	req, err := http.NewRequest("GET", "/", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(dashboard.handleHome)

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "REX Dashboard")
}

func TestHandleStats(t *testing.T) {
	s, _, dashboard := setupTestEnvironment(t)
	defer s.Close()

	req, err := http.NewRequest("GET", "/api/stats", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(dashboard.handleStats)

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var stats map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &stats)
	assert.NoError(t, err)
	assert.Contains(t, stats, "TotalFactsProcessed")
	assert.Contains(t, stats, "TotalRulesProcessed")
}

func TestHandleSSE(t *testing.T) {
	s, _, dashboard := setupTestEnvironment(t)
	defer s.Close()

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
	s, _, dashboard := setupTestEnvironment(t)
	defer s.Close()

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
		assert.Contains(t, stats, "TotalFactsProcessed")
		assert.Contains(t, stats, "TotalRulesProcessed")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for broadcast message")
	}
}
