// rex/pkg/runtime/engine_test.go

package runtime

import (
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"
)

func setupMiniredis(t *testing.T) (*miniredis.Miniredis, *store.RedisStore) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}

	redisStore := store.NewRedisStore(s.Addr(), "", 0)
	return s, redisStore
}

func createTestBytecodeFile(t *testing.T, ruleset *compiler.Ruleset) string {
	bytecode := compiler.GenerateBytecode(ruleset)
	filename := "test_bytecode.bin"
	err := compiler.WriteBytecodeToFile(filename, bytecode)
	assert.NoError(t, err)
	return filename
}

func TestProcessFactUpdate(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

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

	filename := createTestBytecodeFile(t, ruleset)
	defer os.Remove(filename)

	engine, err := NewEngineFromFile(filename, redisStore, 0)
	assert.NoError(t, err)

	engine.ProcessFactUpdate("temperature", 35.0)

	// Verify the fact was updated in miniredis
	alertValue, err := s.Get("alert")
	assert.NoError(t, err)
	assert.Equal(t, "true", alertValue)
}

func TestMultipleRules(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

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

	filename := createTestBytecodeFile(t, ruleset)
	defer os.Remove(filename)

	engine, err := NewEngineFromFile(filename, redisStore, 0)
	assert.NoError(t, err)

	engine.ProcessFactUpdate("temperature", 35.0)
	alertValue, err := s.Get("alert")
	assert.NoError(t, err)
	assert.Equal(t, "true", alertValue)

	engine.ProcessFactUpdate("humidity", 35.0)
	humidifierValue, err := s.Get("humidifier")
	assert.NoError(t, err)
	assert.Equal(t, "true", humidifierValue)
}

// Add more tests here...

func TestCompare(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name       string
		factValue  interface{}
		constValue interface{}
		opcode     compiler.Opcode
		expected   bool
	}{
		{"EQ_FLOAT True", 5.0, 5.0, compiler.EQ_FLOAT, true},
		{"EQ_FLOAT False", 5.0, 6.0, compiler.EQ_FLOAT, false},
		{"NEQ_FLOAT True", 5.0, 6.0, compiler.NEQ_FLOAT, true},
		{"NEQ_FLOAT False", 5.0, 5.0, compiler.NEQ_FLOAT, false},
		{"LT_FLOAT True", 5.0, 6.0, compiler.LT_FLOAT, true},
		{"LT_FLOAT False", 6.0, 5.0, compiler.LT_FLOAT, false},
		{"LTE_FLOAT True", 5.0, 5.0, compiler.LTE_FLOAT, true},
		{"LTE_FLOAT False", 6.0, 5.0, compiler.LTE_FLOAT, false},
		{"GT_FLOAT True", 6.0, 5.0, compiler.GT_FLOAT, true},
		{"GT_FLOAT False", 5.0, 6.0, compiler.GT_FLOAT, false},
		{"GTE_FLOAT True", 5.0, 5.0, compiler.GTE_FLOAT, true},
		{"GTE_FLOAT False", 5.0, 6.0, compiler.GTE_FLOAT, false},
		{"EQ_STRING True", "test", "test", compiler.EQ_STRING, true},
		{"EQ_STRING False", "test", "Test", compiler.EQ_STRING, false},
		{"NEQ_STRING True", "test", "Test", compiler.NEQ_STRING, true},
		{"NEQ_STRING False", "test", "test", compiler.NEQ_STRING, false},
		{"CONTAINS_STRING True", "teststring", "test", compiler.CONTAINS_STRING, true},
		{"CONTAINS_STRING False", "teststring", "TEST", compiler.CONTAINS_STRING, false},
		{"NOT_CONTAINS_STRING True", "teststring", "TEST", compiler.NOT_CONTAINS_STRING, true},
		{"NOT_CONTAINS_STRING False", "teststring", "test", compiler.NOT_CONTAINS_STRING, false},
		{"EQ_BOOL True", true, true, compiler.EQ_BOOL, true},
		{"EQ_BOOL False", true, false, compiler.EQ_BOOL, false},
		{"NEQ_BOOL True", true, false, compiler.NEQ_BOOL, true},
		{"NEQ_BOOL False", true, true, compiler.NEQ_BOOL, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.compare(tt.factValue, tt.constValue, tt.opcode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProcessFactUpdateSimpleRule(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	engine := createTestEngine(redisStore, `{
        "rules": [{
            "name": "simple_rule",
            "conditions": {
                "all": [{
                    "fact": "temperature",
                    "operator": "GT",
                    "value": 30
                }]
            },
            "actions": [{
                "type": "updateStore",
                "target": "status",
                "value": "hot"
            }]
        }]
    }`)

	engine.ProcessFactUpdate("temperature", 35)

	status, err := redisStore.GetFact("status")
	assert.NoError(t, err)
	assert.Equal(t, "hot", status)
}

func TestProcessFactUpdateComplexRule(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	engine := createTestEngine(redisStore, `{
        "rules": [{
            "name": "complex_rule",
            "conditions": {
                "all": [
                    {
                        "fact": "temperature",
                        "operator": "GT",
                        "value": 30
                    },
                    {
                        "any": [
                            {
                                "fact": "humidity",
                                "operator": "LT",
                                "value": 50
                            },
                            {
                                "fact": "pressure",
                                "operator": "GT",
                                "value": 1000
                            }
                        ]
                    }
                ]
            },
            "actions": [{
                "type": "updateStore",
                "target": "status",
                "value": "alert"
            }]
        }]
    }`)

	// Initialize Redis store with initial facts
	initialFacts := map[string]interface{}{
		"temperature": 25.0,
		"humidity":    49.0,
		"pressure":    900.0,
		"status":      "",
	}
	for k, v := range initialFacts {
		redisStore.SetFact(k, v)
	}

	// Test case 1: Should trigger the rule
	t.Log("Test case 1: Should trigger the rule")
	engine.ProcessFactUpdate("temperature", 35.0)

	status, err := redisStore.GetFact("status")
	assert.NoError(t, err)
	assert.Equal(t, "alert", status, "Rule should have been triggered")

	// Reset status in Redis
	redisStore.SetFact("status", "")
	redisStore.SetFact("temperature", 35)
	redisStore.SetFact("humidity", 60)
	redisStore.SetFact("pressure", 900)

	// Test case 2: Should trigger the rule with different conditions
	t.Log("Test case 2: Should trigger the rule with different conditions")
	engine.ProcessFactUpdate("humidity", 30.0)

	status, err = redisStore.GetFact("status")
	assert.NoError(t, err)
	assert.Equal(t, "alert", status, "Rule should have been triggered")

	// Test case 3: Should not trigger the rule
	t.Log("Test case 3: Should not trigger the rule")
	redisStore.SetFact("status", "")
	engine.ProcessFactUpdate("pressure", 900.0)

	status, err = redisStore.GetFact("status")
	assert.NoError(t, err)
	assert.Equal(t, "", status, "Rule should not have been triggered")
}

// Helper function to create a test engine
func createTestEngine(redisStore *store.RedisStore, jsonRuleset string) *Engine {
	ruleset, _ := compiler.Parse([]byte(jsonRuleset))
	bytecodeFile := compiler.GenerateBytecode(ruleset)

	filename := "test_bytecode.bin"
	compiler.WriteBytecodeToFile(filename, bytecodeFile)
	defer os.Remove(filename)

	engine, _ := NewEngineFromFile(filename, redisStore, 0)

	// Synchronize engine's fact store with Redis store
	facts, _ := redisStore.MGetFacts("temperature", "humidity", "pressure", "status")
	for k, v := range facts {
		engine.Facts[k] = v
	}

	return engine
}

func TestNestedScriptCalls(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	ruleset := &compiler.Ruleset{
		Rules: []compiler.Rule{
			{
				Name: "nested_script_rule",
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
						Target: "heat_index",
						Value:  "{calculate_heat_index}",
					},
				},
				Scripts: map[string]compiler.Script{
					"calculate_heat_index": {
						Params: []string{"temperature", "humidity"},
						Body:   "return calculate_adjusted_index(temperature * 1.8 + 32, humidity);",
					},
					"calculate_adjusted_index": {
						Params: []string{"heat_index", "humidity"},
						Body:   "return heat_index + (humidity / 100) * 10;",
					},
				},
			},
		},
	}

	bytecodeFile := compiler.GenerateBytecode(ruleset)
	tempFile := "temp_nested_bytecode.bin"
	err := compiler.WriteBytecodeToFile(tempFile, bytecodeFile)
	assert.NoError(t, err)
	defer os.Remove(tempFile)

	engine, err := NewEngineFromFile(tempFile, redisStore, 0)
	assert.NoError(t, err)

	// Register the nested script as a global function
	err = engine.ScriptEngine.RegisterGlobalFunction("calculate_adjusted_index", compiler.Script{
		Params: []string{"heat_index", "humidity"},
		Body:   "return heat_index + (humidity / 100) * 10;",
	})
	assert.NoError(t, err)

	// Then set the main script
	err = engine.ScriptEngine.SetScript("calculate_heat_index", compiler.Script{
		Params: []string{"temperature", "humidity"},
		Body:   "return calculate_adjusted_index(temperature * 1.8 + 32, humidity);",
	})
	assert.NoError(t, err)

	err = redisStore.SetFact("temperature", 35.0)
	assert.NoError(t, err)
	err = redisStore.SetFact("humidity", 60.0)
	assert.NoError(t, err)

	engine.ProcessFactUpdate("temperature", 35.0)

	time.Sleep(100 * time.Millisecond)

	heatIndex, exists := engine.Facts["heat_index"]
	assert.True(t, exists, "Heat index calculation result not found in engine facts")
	if exists {
		t.Logf("Calculated heat index: %v", heatIndex)
		assert.InDelta(t, 101.0, heatIndex.(float64), 0.1)
	}
}

func TestScriptErrorHandling(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	ruleset := &compiler.Ruleset{
		Rules: []compiler.Rule{
			{
				Name: "error_script_rule",
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
						Value:  "{error_script}",
					},
				},
				Scripts: map[string]compiler.Script{
					"error_script": {
						Params: []string{"temperature"},
						Body:   "return temperature.unknownMethod();",
					},
				},
			},
		},
	}

	bytecodeFile := compiler.GenerateBytecode(ruleset)
	tempFile := "temp_error_bytecode.bin"
	err := compiler.WriteBytecodeToFile(tempFile, bytecodeFile)
	assert.NoError(t, err)
	defer os.Remove(tempFile)

	engine, err := NewEngineFromFile(tempFile, redisStore, 0)
	assert.NoError(t, err)

	err = engine.ScriptEngine.SetScript("error_script", compiler.Script{
		Params: []string{"temperature"},
		Body:   "return temperature.unknownMethod();",
	})
	assert.NoError(t, err)

	err = redisStore.SetFact("temperature", 35.0)
	assert.NoError(t, err)

	engine.ProcessFactUpdate("temperature", 35.0)

	time.Sleep(100 * time.Millisecond)

	status, exists := engine.Facts["status"]
	assert.False(t, exists, "Error script execution should not result in a status fact")
	assert.Nil(t, status)
}

func TestEdgeCases(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	ruleset := &compiler.Ruleset{
		Rules: []compiler.Rule{
			{
				Name: "edge_case_script_rule",
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
						Value:  "{edge_case_script}",
					},
				},
				Scripts: map[string]compiler.Script{
					"edge_case_script": {
						Params: []string{"temperature"},
						Body:   "return temperature * 2 / 0;", // Division by zero
					},
				},
			},
		},
	}

	bytecodeFile := compiler.GenerateBytecode(ruleset)
	tempFile := "temp_edge_case_bytecode.bin"
	err := compiler.WriteBytecodeToFile(tempFile, bytecodeFile)
	assert.NoError(t, err)
	defer os.Remove(tempFile)

	engine, err := NewEngineFromFile(tempFile, redisStore, 0)
	assert.NoError(t, err)

	err = engine.ScriptEngine.SetScript("edge_case_script", compiler.Script{
		Params: []string{"temperature"},
		Body:   "return temperature * 2 / 0;", // Division by zero
	})
	assert.NoError(t, err)

	err = redisStore.SetFact("temperature", 35.0)
	assert.NoError(t, err)

	engine.ProcessFactUpdate("temperature", 35.0)

	time.Sleep(100 * time.Millisecond)

	status, exists := engine.Facts["status"]
	assert.False(t, exists, "Edge case script execution should not result in a status fact")
	assert.Nil(t, status)
}
