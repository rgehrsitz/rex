// rex/cmd/rexd/main_test.go

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"
)

// Mock implementations for testing purposes
type MockStoreFactory struct{}

func (f *MockStoreFactory) NewStore(addr, password string, db int) store.Store {
	return store.NewRedisStore(addr, password, db)
}

type MockEngineFactory struct{}

func (f *MockEngineFactory) NewEngine(bytecodeFile string, store store.Store, priorityThreshold int, enablePerformanceMonitoring bool) (*runtime.Engine, error) {
	// Updated to include priorityThreshold parameter
	return &runtime.Engine{Facts: make(map[string]interface{})}, nil
}

func TestParseConfig(t *testing.T) {
	// Reset the flag set before each test run
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

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
	assert.Equal(t, 10, config.EngineInterval)
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
		RedisAddress:   mr.Addr(),
		RedisChannels:  []string{"rex_updates"},
		EngineInterval: 1,
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

	err = processMessage(engine, msg)
	require.NoError(t, err)

	assert.Equal(t, "value", engine.Facts["test:key"])
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
