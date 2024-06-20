// rex/pkg/runtime/engine_test.go

package runtime

import (
	"testing"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"

	"github.com/stretchr/testify/assert"
)

func TestProcessFactUpdate(t *testing.T) {
	ruleset := &compiler.Ruleset{
		Rules: []compiler.Rule{
			{
				Name: "UpdateRule",
				Conditions: compiler.ConditionGroup{
					All: []*compiler.ConditionOrGroup{
						{
							Fact:     "temperature",
							Operator: "GT",
							Value:    30.0,
						},
					},
				},
				Actions: []compiler.Action{
					{
						Type:   "updateStore",
						Target: "alert",
						Value:  true,
					},
				},
			},
		},
	}

	bytecode := compiler.GenerateBytecode(ruleset)
	err := compiler.WriteBytecodeToFile("test_engine_bytecode.bin", bytecode)
	assert.NoError(t, err)

	redisStore := store.NewRedisStore("localhost:6379", "", 0)
	engine, err := NewEngineFromFile("test_engine_bytecode.bin", redisStore)
	assert.NoError(t, err)

	engine.ProcessFactUpdate("temperature", 35.0)
	assert.Equal(t, true, engine.Facts["alert"])
}

func TestMultipleRules(t *testing.T) {
	ruleset := &compiler.Ruleset{
		Rules: []compiler.Rule{
			{
				Name: "Rule1",
				Conditions: compiler.ConditionGroup{
					All: []*compiler.ConditionOrGroup{
						{
							Fact:     "temperature",
							Operator: "GT",
							Value:    30.0,
						},
					},
				},
				Actions: []compiler.Action{
					{
						Type:   "updateStore",
						Target: "alert",
						Value:  true,
					},
				},
			},
			{
				Name: "Rule2",
				Conditions: compiler.ConditionGroup{
					All: []*compiler.ConditionOrGroup{
						{
							Fact:     "humidity",
							Operator: "LT",
							Value:    40.0,
						},
					},
				},
				Actions: []compiler.Action{
					{
						Type:   "updateStore",
						Target: "humidifier",
						Value:  true,
					},
				},
			},
		},
	}

	bytecode := compiler.GenerateBytecode(ruleset)
	err := compiler.WriteBytecodeToFile("test_engine_bytecode.bin", bytecode)
	assert.NoError(t, err)

	redisStore := store.NewRedisStore("localhost:6379", "", 0)
	engine, err := NewEngineFromFile("test_engine_bytecode.bin", redisStore)
	assert.NoError(t, err)

	engine.ProcessFactUpdate("temperature", 35.0)
	// Assert the expected behavior
	assert.Equal(t, true, engine.Facts["alert"])

	engine.ProcessFactUpdate("humidity", 35.0)
	assert.Equal(t, true, engine.Facts["humidifier"])
}

func TestConditionOperators(t *testing.T) {
	ruleset := &compiler.Ruleset{
		Rules: []compiler.Rule{
			{
				Name: "TemperatureRule",
				Conditions: compiler.ConditionGroup{
					All: []*compiler.ConditionOrGroup{
						{
							Fact:     "temperature",
							Operator: "GT",
							Value:    30.0,
						},
						{
							Fact:     "temperature",
							Operator: "LTE",
							Value:    40.0,
						},
					},
				},
				Actions: []compiler.Action{
					{
						Type:   "updateStore",
						Target: "temperatureAlert",
						Value:  true,
					},
				},
			},
			{
				Name: "PressureRule",
				Conditions: compiler.ConditionGroup{
					All: []*compiler.ConditionOrGroup{
						{
							Fact:     "pressure",
							Operator: "EQ",
							Value:    1000.0,
						},
						{
							Fact:     "unit",
							Operator: "EQ",
							Value:    "hPa",
						},
					},
				},
				Actions: []compiler.Action{
					{
						Type:   "updateStore",
						Target: "pressureAlert2",
						Value:  true,
					},
				},
			},
			{
				Name: "WindSpeedRule",
				Conditions: compiler.ConditionGroup{
					All: []*compiler.ConditionOrGroup{
						{
							Fact:     "windSpeed",
							Operator: "GTE",
							Value:    60.0,
						},
						{
							Fact:     "windDirection",
							Operator: "CONTAINS",
							Value:    "N",
						},
					},
				},
				Actions: []compiler.Action{
					{
						Type:   "updateStore",
						Target: "windAlert",
						Value:  true,
					},
				},
			},
		},
	}

	bytecode := compiler.GenerateBytecode(ruleset)
	err := compiler.WriteBytecodeToFile("test_engine_bytecode.bin", bytecode)
	assert.NoError(t, err)

	redisStore := store.NewRedisStore("localhost:6379", "", 0)
	engine, err := NewEngineFromFile("test_engine_bytecode.bin", redisStore)
	assert.NoError(t, err)

	// // Test GT and LTE operators
	// engine.ProcessFactUpdate("temperature", 35.0)
	// assert.Equal(t, true, engine.Facts["temperatureAlert"])

	// // Test LT and GT operators
	// engine.ProcessFactUpdate("humidity", 25.0)
	// assert.Equal(t, true, engine.Facts["humidityAlert"])
	// engine.ProcessFactUpdate("humidity", 75.0)
	// assert.Equal(t, true, engine.Facts["humidityAlert"])

	// Test EQ operator
	redisStore.SetFact("unit", "hPa")
	engine.ProcessFactUpdate("pressure", 1000.0)
	assert.Equal(t, true, engine.Facts["pressureAlert"])

	redisStore.SetFact("pressure", 1000.0)
	engine.ProcessFactUpdate("unit", "hPa")
	assert.Equal(t, true, engine.Facts["pressureAlert"])

	// Test GTE and CONTAINS operators
	redisStore.SetFact("windDirection", "NW")
	engine.ProcessFactUpdate("windSpeed", 65.0)
	assert.Equal(t, true, engine.Facts["windAlert"])

	redisStore.SetFact("windSpeed", 60.0)
	engine.ProcessFactUpdate("windDirection", "NW")
	assert.Equal(t, true, engine.Facts["windAlert"])
}
