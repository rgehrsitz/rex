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

	engine, err := NewEngineFromFile(filename, redisStore, 0, false)
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

	engine, err := NewEngineFromFile(filename, redisStore, 0, false)
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

	engine, _ := NewEngineFromFile(filename, redisStore, 0, false)

	// Synchronize engine's fact store with Redis store
	facts, _ := redisStore.MGetFacts("temperature", "humidity", "pressure", "status")
	for k, v := range facts {
		engine.Facts[k] = v
	}

	return engine
}

// func TestPerformanceMonitoring(t *testing.T) {
// 	s, redisStore := setupMiniredis(t)
// 	defer s.Close()

// 	// Test with performance monitoring enabled
// 	engineEnabled := createTestEngine(redisStore, `{
//         "rules": [{
//             "name": "test_rule",
//             "conditions": {
//                 "all": [{
//                     "fact": "temperature",
//                     "operator": "GT",
//                     "value": 30
//                 }]
//             },
//             "actions": [{
//                 "type": "updateStore",
//                 "target": "status",
//                 "value": "hot"
//             }]
//         }]
//     }`)
// 	engineEnabled.enablePerformanceMonitoring = true

// 	// Process a fact update
// 	engineEnabled.ProcessFactUpdate("temperature", 35)

// 	assert.Greater(t, engineEnabled.Stats.TotalFactsProcessed, int64(0))
// 	assert.Greater(t, engineEnabled.Stats.TotalRulesProcessed, int64(0))
// 	assert.NotZero(t, engineEnabled.Stats.LastUpdateTime)
// 	assert.NotNil(t, engineEnabled.FactStats["temperature"])

// 	// Test with performance monitoring disabled
// 	engineDisabled := createTestEngine(redisStore, `{
//         "rules": [{
//             "name": "test_rule",
//             "conditions": {
//                 "all": [{
//                     "fact": "temperature",
//                     "operator": "GT",
//                     "value": 30
//                 }]
//             },
//             "actions": [{
//                 "type": "updateStore",
//                 "target": "status",
//                 "value": "hot"
//             }]
//         }]
//     }`)
// 	engineDisabled.enablePerformanceMonitoring = false

// 	// Process a fact update
// 	engineDisabled.ProcessFactUpdate("temperature", 35)

// 	assert.Equal(t, int64(0), engineDisabled.Stats.TotalFactsProcessed)
// 	assert.Equal(t, int64(0), engineDisabled.Stats.TotalRulesProcessed)
// 	assert.Zero(t, engineDisabled.Stats.LastUpdateTime)
// 	assert.Nil(t, engineDisabled.FactStats["temperature"])
// }

func TestCalculateRates(t *testing.T) {
	engine := &Engine{
		Stats: EngineStats{
			EngineStartTime: time.Now().Add(-10 * time.Second),
		},
		RuleStats: make(map[string]*RuleStats),
	}

	engine.RuleStats["testRule"] = &RuleStats{
		ExecutionCount:     10,
		TotalExecutionTime: 500 * time.Millisecond,
	}

	engine.Stats.TotalFactsProcessed = 100
	engine.Stats.TotalRulesProcessed = 10

	engine.calculateRates()

	if engine.Stats.FactProcessingRate <= 0 {
		t.Errorf("Expected FactProcessingRate to be greater than 0, got %f", engine.Stats.FactProcessingRate)
	}

	if engine.Stats.RuleExecutionRate <= 0 {
		t.Errorf("Expected RuleExecutionRate to be greater than 0, got %f", engine.Stats.RuleExecutionRate)
	}

	if engine.Stats.AverageRuleEvaluationTime <= 0 {
		t.Errorf("Expected AverageRuleEvaluationTime to be greater than 0, got %d", engine.Stats.AverageRuleEvaluationTime)
	}
}
