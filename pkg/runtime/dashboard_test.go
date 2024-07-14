// rex/pkg/runtime/dashboard_test.go

package runtime

import (
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
