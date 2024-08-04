// rex/pkg/runtime/scripting_test.go

package runtime

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"rgehrsitz/rex/pkg/compiler"
)

func TestScriptingEndToEnd(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	ruleset := &compiler.Ruleset{
		Rules: []compiler.Rule{
			{
				Name: "script_rule",
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
						Target: "status",
						Value:  "hot",
					},
					{
						Type:   "updateStore",
						Target: "heat_index",
						Value:  "{calculate_heat_index}", // This should trigger the script call
					},
				},
				Scripts: map[string]compiler.Script{
					"calculate_heat_index": {
						Params: []string{"temperature", "humidity"},
						Body:   "return (temperature * 1.8 + 32) + (humidity / 100) * 10;",
					},
				},
			},
		},
	}

	bytecodeFile := compiler.GenerateBytecode(ruleset)

	// Verify that the fact index includes script parameters
	assert.Contains(t, bytecodeFile.FactRuleLookupIndex, "temperature")
	assert.Contains(t, bytecodeFile.FactRuleLookupIndex, "humidity")

	// Log the fact rule lookup index for debugging
	t.Logf("Fact Rule Lookup Index: %v", bytecodeFile.FactRuleLookupIndex)

	// Log bytecode for debugging
	t.Logf("Generated Bytecode: %v", bytecodeFile.Instructions)

	tempFile := "temp_bytecode.bin"
	err := compiler.WriteBytecodeToFile(tempFile, bytecodeFile)
	assert.NoError(t, err)
	defer os.Remove(tempFile)

	engine, err := NewEngineFromFile(tempFile, redisStore, 0)
	assert.NoError(t, err)

	// Set the script in the engine's script engine
	err = engine.ScriptEngine.SetScript("calculate_heat_index", compiler.Script{
		Params: []string{"temperature", "humidity"},
		Body:   "return temperature * 1.8 + 32 + (humidity / 100) * 10;",
	})
	assert.NoError(t, err)

	// Set initial facts
	err = redisStore.SetFact("temperature", 35.0)
	assert.NoError(t, err)
	err = redisStore.SetFact("humidity", 60.0)
	assert.NoError(t, err)

	// Log initial facts
	temp, _ := redisStore.GetFact("temperature")
	humidity, _ := redisStore.GetFact("humidity")
	t.Logf("Initial facts: temperature = %v, humidity = %v", temp, humidity)

	// Process fact update to trigger rule evaluation
	engine.ProcessFactUpdate("temperature", 35.0)

	// Wait for a short time to allow for async processing
	time.Sleep(100 * time.Millisecond)

	// Log engine facts after processing
	t.Logf("Engine facts after processing: %v", engine.Facts)

	// Verify rule execution
	status, err := redisStore.GetFact("status")
	assert.NoError(t, err)
	t.Logf("Status after rule execution: %v", status)
	assert.Equal(t, "hot", status)

	// Verify script execution
	heatIndex, exists := engine.Facts["heat_index"]
	assert.True(t, exists, "Heat index calculation result not found in engine facts")
	if exists {
		t.Logf("Calculated heat index: %v", heatIndex)
		assert.InDelta(t, 101.0, heatIndex.(float64), 0.1)
	}

	// Log all keys in Redis store
	keys := s.Keys()
	t.Logf("All keys in Redis store: %v", keys)
}
