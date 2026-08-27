// rex/cmd/rexd/main_test.go

package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/observability"
	"rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"
)

// Mock implementations for testing purposes
type MockStoreFactory struct{}

func (f *MockStoreFactory) NewStore(addr, password string, db int) store.ContextStore {
	return store.NewRedisStore(addr, password, db)
}

type MockEngineFactory struct{}

func (f *MockEngineFactory) NewEngine(bytecodeFile string, store store.ContextStore, priorityThreshold int) (*runtime.Engine, error) {
	// Updated to include priorityThreshold parameter
	return &runtime.Engine{Facts: make(map[string]interface{})}, nil
}

type actionCountingStore struct {
	publishCount int
}

type traceCapturingEngine struct {
	factNames []string
	traceIDs  []string
}

func (e *traceCapturingEngine) ProcessFactUpdateContext(ctx context.Context, factName string, _ interface{}) error {
	e.factNames = append(e.factNames, factName)
	e.traceIDs = append(e.traceIDs, runtime.TraceIDFromContext(ctx))
	return nil
}

func (s *actionCountingStore) Close() error { return nil }

func (s *actionCountingStore) SetFactContext(context.Context, string, interface{}) error { return nil }

func (s *actionCountingStore) SetAndPublishFactContext(context.Context, string, interface{}) error {
	s.publishCount++
	return nil
}

func (s *actionCountingStore) GetFactContext(context.Context, string) (interface{}, error) {
	return nil, nil
}

func (s *actionCountingStore) MGetFactsContext(context.Context, ...string) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func newTestRuntimeEngine(t *testing.T, ruleset *compiler.Ruleset, factStore store.ContextStore) *runtime.Engine {
	t.Helper()

	filename := t.TempDir() + "/rules.bytecode"
	require.NoError(t, compiler.WriteBytecodeToFile(filename, compiler.GenerateBytecode(ruleset)))

	engine, err := runtime.NewEngineFromFile(filename, factStore, 0)
	require.NoError(t, err)
	return engine
}

func TestParseConfig(t *testing.T) {
	// Reset the flag set before each test run
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	viper.Reset()
	t.Cleanup(viper.Reset)

	configFile, err := os.CreateTemp("", "rex_config.json")
	require.NoError(t, err)
	defer os.Remove(configFile.Name())

	configContent := `{
		"bytecode_file": "test.bytecode",
		"logging.level": "debug",
		"logging.output": "file",
		"logging.time_format": "RFC3339",
		"redis.address": "localhost:6379",
		"redis.password": "password",
		"redis.database": 1,
		"redis.channels": ["rex_updates"],
		"engine.scripts_enabled": true,
		"observability.enabled": true,
		"observability.address": "127.0.0.1:9091",
		"engine.update_interval": 10,
		"dashboard.enabled": true,
		"dashboard.port": 9090,
		"dashboard.update_interval": 15
	}`
	_, err = configFile.WriteString(configContent)
	require.NoError(t, err)
	configFile.Close()

	args := []string{"rexd", "--config", configFile.Name()}
	config, err := parseConfig(args)
	require.NoError(t, err)

	assert.Equal(t, "test.bytecode", config.BytecodeFile)
	assert.Equal(t, "debug", config.LogLevel)
	assert.Equal(t, "file", config.LogDestination)
	assert.Equal(t, "RFC3339", config.LogTimeFormat)
	assert.Equal(t, "localhost:6379", config.RedisAddress)
	assert.Equal(t, "password", config.RedisPassword)
	assert.Equal(t, 1, config.RedisDB)
	assert.Equal(t, []string{"rex_updates"}, config.RedisChannels)
	assert.True(t, config.ScriptsEnabled)
	assert.True(t, config.ObservabilityEnabled)
	assert.Equal(t, "127.0.0.1:9091", config.ObservabilityAddress)
}

func TestParseConfigDefaultsScriptsDisabled(t *testing.T) {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	viper.Reset()
	t.Cleanup(viper.Reset)

	configFile, err := os.CreateTemp("", "rex_config.json")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(configFile.Name()) })
	_, err = configFile.WriteString(`{"bytecode_file":"test.bytecode"}`)
	require.NoError(t, err)
	require.NoError(t, configFile.Close())

	config, err := parseConfig([]string{"rexd", "--config", configFile.Name()})
	require.NoError(t, err)
	assert.False(t, config.ScriptsEnabled)
}

func TestSetupDependencies(t *testing.T) {
	// Reset the flag set before each test run
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	config := &Config{
		BytecodeFile:      "test.bytecode",
		RedisAddress:      mr.Addr(),
		RedisPassword:     "",
		RedisDB:           0,
		PriorityThreshold: 5, // Add PriorityThreshold to the config
	}

	deps, err := setupDependencies(config, &MockStoreFactory{}, &MockEngineFactory{})
	require.NoError(t, err)

	assert.NotNil(t, deps.Store)
	assert.NotNil(t, deps.Engine)
}

func TestRunMainLoop(t *testing.T) {
	// Reset the flag set before each test run
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	config := &Config{
		RedisAddress:  mr.Addr(),
		RedisChannels: []string{"rex_updates"},
	}

	deps := &RexDependencies{
		Store:  store.NewRedisStore(mr.Addr(), "", 0),
		Engine: &runtime.Engine{Facts: make(map[string]interface{})},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		time.Sleep(500 * time.Millisecond)
		mr.Publish("rex_updates", "test:key=value")
		cancel()
	}()

	err = runMainLoop(ctx, deps, config)
	assert.NoError(t, err)
}

func TestConsumeMessagesStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := consumeMessages(ctx, &runtime.Engine{Facts: make(map[string]interface{})}, make(chan *redis.Message), nil)
	assert.NoError(t, err)
}

func TestConsumeMessagesStopsOnSignal(t *testing.T) {
	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGTERM

	err := consumeMessages(context.Background(), &runtime.Engine{Facts: make(map[string]interface{})}, make(chan *redis.Message), signals)
	assert.NoError(t, err)
}

func TestConsumeMessagesContinuesAfterMalformedEvent(t *testing.T) {
	messages := make(chan *redis.Message, 2)
	messages <- &redis.Message{Channel: "rex_updates", Payload: "not a fact event"}
	messages <- &redis.Message{Channel: "rex_updates", Payload: `{"test:key":"value"}`}
	close(messages)

	engine := &runtime.Engine{Facts: make(map[string]interface{})}
	err := consumeMessages(context.Background(), engine, messages, nil)
	require.NoError(t, err)
	assert.Equal(t, "value", engine.Facts["test:key"])
}

func TestConsumeMessagesProcessesDuplicateDeliveries(t *testing.T) {
	factStore := &actionCountingStore{}
	engine := newTestRuntimeEngine(t, &compiler.Ruleset{Rules: []compiler.Rule{{
		Name: "temperature_rule",
		Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{
			Fact:     "temperature",
			Operator: "GT",
			Value:    30.0,
		}}},
		Actions: []compiler.Action{{Type: "updateStore", Target: "status", Value: "hot"}},
	}}}, factStore)

	messages := make(chan *redis.Message, 2)
	messages <- &redis.Message{Channel: "rex_updates", Payload: `{"temperature":35}`}
	messages <- &redis.Message{Channel: "rex_updates", Payload: `{"temperature":35}`}
	close(messages)

	require.NoError(t, consumeMessages(context.Background(), engine, messages, nil))
	assert.Equal(t, 2, factStore.publishCount)
}

func TestConsumeMessagesRecordsEventMetrics(t *testing.T) {
	messages := make(chan *redis.Message, 2)
	messages <- &redis.Message{Channel: "rex_updates", Payload: `{"temperature":35}`}
	messages <- &redis.Message{Channel: "rex_updates", Payload: "not a fact event"}
	close(messages)

	metrics := observability.NewMetrics()
	engine := &runtime.Engine{Facts: make(map[string]interface{})}
	require.NoError(t, consumeMessagesWithMetrics(context.Background(), engine, messages, nil, metrics))

	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Contains(t, response.Body.String(), "rex_events_received_total 2")
	assert.Contains(t, response.Body.String(), "rex_event_failures_total 1")
}

func TestRunMainLoopWiresMetricsObserver(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	factStore := &actionCountingStore{}
	engine := newTestRuntimeEngine(t, &compiler.Ruleset{Rules: []compiler.Rule{{
		Name: "temperature_rule",
		Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{
			Fact:     "temperature",
			Operator: "GT",
			Value:    30.0,
		}}},
		Actions: []compiler.Action{{Type: "updateStore", Target: "status", Value: "hot"}},
	}}}, factStore)
	redisStore := store.NewRedisStore(mr.Addr(), "", 0)
	defer redisStore.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(200 * time.Millisecond)
		mr.Publish("rex_updates", `{"temperature":35}`)
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	metrics := observability.NewMetrics()
	require.NoError(t, runMainLoopWithObservability(ctx, &RexDependencies{Store: redisStore, Engine: engine}, &Config{
		RedisChannels: []string{"rex_updates"},
	}, metrics))

	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Contains(t, response.Body.String(), "rex_rules_fired_total 1")
	assert.Contains(t, response.Body.String(), "rex_actions_succeeded_total 1")
}

func TestStartObservabilityServerServesHealthEndpoint(t *testing.T) {
	metrics := observability.NewMetrics()
	server, err := startObservabilityServer(&Config{
		ObservabilityEnabled: true,
		ObservabilityAddress: "127.0.0.1:0",
	}, metrics)
	require.NoError(t, err)
	t.Cleanup(func() { shutdownObservabilityServer(server) })

	response, err := http.Get("http://" + server.Addr + "/healthz")
	require.NoError(t, err)
	defer response.Body.Close()
	assert.Equal(t, http.StatusOK, response.StatusCode)
}

func TestProcessMessage(t *testing.T) {
	// Reset the flag set before each test run
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	engine := &runtime.Engine{
		Facts: make(map[string]interface{}),
	}

	msg := &redis.Message{
		Channel: "rex_updates",
		Payload: "test:key=value",
	}

	err = processMessage(context.Background(), engine, msg)
	require.NoError(t, err)

	assert.Equal(t, "value", engine.Facts["test:key"])
}

func TestProcessMessagePreservesEqualsInValue(t *testing.T) {
	engine := &runtime.Engine{Facts: make(map[string]interface{})}

	err := processMessage(context.Background(), engine, &redis.Message{
		Channel: "rex_updates",
		Payload: "test:key=\"a=b\"",
	})
	require.NoError(t, err)

	assert.Equal(t, "a=b", engine.Facts["test:key"])
}

func TestProcessMessageDecodesJSONValues(t *testing.T) {
	engine := &runtime.Engine{Facts: make(map[string]interface{})}

	err := processMessage(context.Background(), engine, &redis.Message{
		Channel: "rex_updates",
		Payload: `{"test:string":"a=b","test:bool":true,"test:number":3.5}`,
	})
	require.NoError(t, err)

	assert.Equal(t, "a=b", engine.Facts["test:string"])
	assert.Equal(t, true, engine.Facts["test:bool"])
	assert.Equal(t, 3.5, engine.Facts["test:number"])
}

func TestProcessMessageAssignsOneTraceIDToAllFactsInEvent(t *testing.T) {
	engine := &traceCapturingEngine{}

	err := processMessage(context.Background(), engine, &redis.Message{
		Channel: "rex_updates",
		Payload: `{"zeta":1,"alpha":2}`,
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"alpha", "zeta"}, engine.factNames)
	require.Len(t, engine.traceIDs, 2)
	assert.NotEmpty(t, engine.traceIDs[0])
	assert.Equal(t, engine.traceIDs[0], engine.traceIDs[1])
}

func TestProcessMessagePreservesLegacyFloatValues(t *testing.T) {
	engine := &runtime.Engine{Facts: make(map[string]interface{})}

	err := processMessage(context.Background(), engine, &redis.Message{
		Channel: "rex_updates",
		Payload: "test:number=NaN",
	})
	require.NoError(t, err)

	assert.True(t, math.IsNaN(engine.Facts["test:number"].(float64)))
}

func TestSortedKeys(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "c"}, sortedKeys(map[string]interface{}{
		"b": true,
		"c": true,
		"a": true,
	}))
}

func TestRun(t *testing.T) {
	// Reset the flag set before each test run
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	configFile, err := os.CreateTemp("", "rex_config.json")
	require.NoError(t, err)
	defer os.Remove(configFile.Name())

	configContent := fmt.Sprintf(`{
		"redis.address": "%s",
		"engine.priority_threshold": 5
	}`, mr.Addr())
	_, err = configFile.WriteString(configContent)
	require.NoError(t, err)
	configFile.Close()

	args := []string{"rexd", "--config", configFile.Name()}

	// Use a context to control the runtime duration
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		time.Sleep(500 * time.Millisecond)
		mr.Publish("rex_updates", "test:key=value")
	}()

	err = run(ctx, args, &MockStoreFactory{}, &MockEngineFactory{})
	assert.NoError(t, err)
}
