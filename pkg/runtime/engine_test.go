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
				Name: "HumidityRule",
				Conditions: compiler.ConditionGroup{
					Any: []*compiler.ConditionOrGroup{
						{
							Fact:     "humidity",
							Operator: "LT",
							Value:    30.0,
						},
						{
							Fact:     "humidity",
							Operator: "GT",
							Value:    70.0,
						},
					},
				},
				Actions: []compiler.Action{
					{
						Type:   "updateStore",
						Target: "humidityAlert",
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

	// Test GT and LTE operators
	redisStore.SetFact("temperature", 25.0)
	redisStore.SetFact("temperatureAlert", false)
	engine.ProcessFactUpdate("temperature", 37.0)
	tempStatus, _ := redisStore.GetFact("temperatureAlert")
	assert.Equal(t, true, tempStatus)

	redisStore.SetFact("temperature", 25.0)
	redisStore.SetFact("temperatureAlert", false)
	engine.ProcessFactUpdate("temperature", 29.0)
	tempStatus, _ = redisStore.GetFact("temperatureAlert")
	assert.Equal(t, false, tempStatus)

	// Test LT and GT operators
	redisStore.SetFact("humidity", 50.0)
	redisStore.SetFact("humidityAlert", false)
	engine.ProcessFactUpdate("humidity", 60.0)
	tempStatus, _ = redisStore.GetFact("humidityAlert")
	assert.Equal(t, false, tempStatus)

	redisStore.SetFact("humidity", 50.0)
	redisStore.SetFact("humidityAlert", false)
	engine.ProcessFactUpdate("humidity", 70.01)
	tempStatus, _ = redisStore.GetFact("humidityAlert")
	assert.Equal(t, true, tempStatus)

	// Test EQ operator
	redisStore.SetFact("unit", "hPa")
	redisStore.SetFact("pressureAlert2", false)
	engine.ProcessFactUpdate("pressure", 1000.00000)
	tempStatus, _ = redisStore.GetFact("pressureAlert2")
	assert.Equal(t, true, tempStatus)

	redisStore.SetFact("pressure", 1000.0)
	redisStore.SetFact("pressureAlert2", false)
	engine.ProcessFactUpdate("unit", "hPa")
	tempStatus, _ = redisStore.GetFact("pressureAlert2")
	assert.Equal(t, true, tempStatus)

	// Test GTE and CONTAINS operators
	redisStore.SetFact("windDirection", "NW")
	redisStore.SetFact("windAlert", false)
	engine.ProcessFactUpdate("windSpeed", 65.0)
	tempStatus, _ = redisStore.GetFact("windAlert")
	assert.Equal(t, true, tempStatus)

	redisStore.SetFact("windSpeed", 60)
	redisStore.SetFact("windAlert", false)
	engine.ProcessFactUpdate("windDirection", "NNE")
	tempStatus, _ = redisStore.GetFact("windAlert")
	assert.Equal(t, true, tempStatus)
}

func TestMissingFacts(t *testing.T) {
	ruleset := &compiler.Ruleset{
		Rules: []compiler.Rule{
			{
				Name: "MissingFactRule",
				Conditions: compiler.ConditionGroup{
					All: []*compiler.ConditionOrGroup{
						{
							Fact:     "nonexistentFact",
							Operator: "GT",
							Value:    10.0,
						},
					},
				},
				Actions: []compiler.Action{
					{
						Type:   "updateStore",
						Target: "missingFactAlert",
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

	redisStore.SetFact("missingFactAlert", false)
	engine.ProcessFactUpdate("someOtherFact", 20.0)
	alertStatus, _ := redisStore.GetFact("missingFactAlert")
	assert.Equal(t, false, alertStatus)
}

func TestComplexConditions(t *testing.T) {
	ruleset := &compiler.Ruleset{
		Rules: []compiler.Rule{
			{
				Name: "ComplexRule",
				Conditions: compiler.ConditionGroup{
					All: []*compiler.ConditionOrGroup{
						{
							Fact:     "temperature",
							Operator: "GT",
							Value:    25.0,
						},
						{
							Any: []*compiler.ConditionOrGroup{
								{
									Fact:     "humidity",
									Operator: "LT",
									Value:    30.0,
								},
								{
									Fact:     "pressure",
									Operator: "GT",
									Value:    1010.0,
								},
							},
						},
					},
				},
				Actions: []compiler.Action{
					{
						Type:   "updateStore",
						Target: "complexAlert",
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

	// Test case 1: Should trigger the alert
	redisStore.SetFact("temperature", 26.0)
	redisStore.SetFact("humidity", 25.0)
	redisStore.SetFact("pressure", 1000.0)
	redisStore.SetFact("complexAlert", false)
	engine.ProcessFactUpdate("temperature", 26.0)
	alertStatus, _ := redisStore.GetFact("complexAlert")
	assert.Equal(t, true, alertStatus)

	// Test case 2: Should trigger the alert
	redisStore.SetFact("temperature", 26.0)
	redisStore.SetFact("humidity", 35.0)
	redisStore.SetFact("pressure", 1015.0)
	redisStore.SetFact("complexAlert", false)
	engine.ProcessFactUpdate("pressure", 1015.0)
	alertStatus, _ = redisStore.GetFact("complexAlert")
	assert.Equal(t, true, alertStatus)

	// Test case 3: Should not trigger the alert
	redisStore.SetFact("temperature", 24.0)
	redisStore.SetFact("humidity", 35.0)
	redisStore.SetFact("pressure", 1015.0)
	redisStore.SetFact("complexAlert", false)
	engine.ProcessFactUpdate("temperature", 24.0)
	alertStatus, _ = redisStore.GetFact("complexAlert")
	assert.Equal(t, false, alertStatus)
}

// // TODO: Determine if we want cascading rules to be supported
// func TestCascadingRules(t *testing.T) {
// 	ruleset := &compiler.Ruleset{
// 		Rules: []compiler.Rule{
// 			{
// 				Name: "FirstRule",
// 				Conditions: compiler.ConditionGroup{
// 					All: []*compiler.ConditionOrGroup{
// 						{
// 							Fact:     "initialFact",
// 							Operator: "GT",
// 							Value:    50.0,
// 						},
// 					},
// 				},
// 				Actions: []compiler.Action{
// 					{
// 						Type:   "updateStore",
// 						Target: "intermediateResult",
// 						Value:  true,
// 					},
// 				},
// 			},
// 			{
// 				Name: "SecondRule",
// 				Conditions: compiler.ConditionGroup{
// 					All: []*compiler.ConditionOrGroup{
// 						{
// 							Fact:     "intermediateResult",
// 							Operator: "EQ",
// 							Value:    true,
// 						},
// 					},
// 				},
// 				Actions: []compiler.Action{
// 					{
// 						Type:   "updateStore",
// 						Target: "finalResult",
// 						Value:  true,
// 					},
// 				},
// 			},
// 		},
// 	}

// 	bytecode := compiler.GenerateBytecode(ruleset)
// 	err := compiler.WriteBytecodeToFile("test_engine_bytecode.bin", bytecode)
// 	assert.NoError(t, err)

// 	redisStore := store.NewRedisStore("localhost:6379", "", 0)
// 	engine, err := NewEngineFromFile("test_engine_bytecode.bin", redisStore)
// 	assert.NoError(t, err)

// 	redisStore.SetFact("intermediateResult", false)
// 	redisStore.SetFact("finalResult", false)
// 	engine.ProcessFactUpdate("initialFact", 60.0)

// 	intermediateStatus, _ := redisStore.GetFact("intermediateResult")
// 	finalStatus, _ := redisStore.GetFact("finalResult")
// 	assert.Equal(t, true, intermediateStatus)
// 	assert.Equal(t, true, finalStatus)
// }

// func TestMultiplsRulesAndOperators(t *testing.T) {
// 	ruleset := &compiler.Ruleset{
// 		Rules: []compiler.Rule{
// 			{
// 				Name:     "rule-1",
// 				Priority: 10,
// 				Conditions: compiler.ConditionGroup{
// 					All: []*compiler.ConditionOrGroup{
// 						{
// 							Any: []*compiler.ConditionOrGroup{
// 								{
// 									Fact:     "pressure",
// 									Operator: "LT",
// 									Value:    1010,
// 								},
// 								{
// 									Fact:     "flow_rate",
// 									Operator: "GT",
// 									Value:    5.0,
// 								},
// 							},
// 						},
// 						{
// 							Any: []*compiler.ConditionOrGroup{
// 								{
// 									Fact:     "temperature",
// 									Operator: "LT",
// 									Value:    72,
// 								},
// 								{
// 									Fact:     "velocity",
// 									Operator: "GT",
// 									Value:    5.0,
// 								},
// 							},
// 						},
// 					},
// 				},
// 				Actions: []compiler.Action{
// 					{
// 						Type:   "updateStore",
// 						Target: "temperature_status",
// 						Value:  true,
// 					},
// 				},
// 			},
// 			{
// 				Name:     "rule-2",
// 				Priority: 15,
// 				Conditions: compiler.ConditionGroup{
// 					All: []*compiler.ConditionOrGroup{
// 						{
// 							Any: []*compiler.ConditionOrGroup{
// 								{
// 									Fact:     "pressure",
// 									Operator: "EQ",
// 									Value:    1013,
// 								},
// 								{
// 									Fact:     "flow_rate",
// 									Operator: "GTE",
// 									Value:    5.0,
// 								},
// 							},
// 						},
// 						{
// 							Any: []*compiler.ConditionOrGroup{
// 								{
// 									Fact:     "temperature",
// 									Operator: "EQ",
// 									Value:    72,
// 								},
// 								{
// 									Fact:     "flow_rate",
// 									Operator: "LT",
// 									Value:    5.0,
// 								},
// 							},
// 						},
// 					},
// 				},
// 				Actions: []compiler.Action{
// 					{
// 						Type:   "sendMessage",
// 						Target: "alert-service",
// 						Value:  "Alert: Pressure or flow rate exceeded limits!",
// 					},
// 				},
// 			},
// 		},
// 	}

// 	bytecode := compiler.GenerateBytecode(ruleset)
// 	err := compiler.WriteBytecodeToFile("test_engine_bytecode.bin", bytecode)
// 	assert.NoError(t, err)

// 	redisStore := store.NewRedisStore("localhost:6379", "", 0)
// 	engine, err := NewEngineFromFile("test_engine_bytecode.bin", redisStore)
// 	assert.NoError(t, err)

// 	// Test GT and LTE operators
// 	//redisStore.SetFact("weather:temperature", 30)
// 	//redisStore.SetFact("weather:flow_rate", 5)
// 	// redisStore.SetFact("weather:pressure", 1013.25)
// 	// redisStore.SetFact("weather:velocity", 6)
// 	redisStore.SetFact("weather:temperature_status", false)
// 	engine.ProcessFactUpdate("flow_rate", 5)

// 	alertStatus, _ := redisStore.GetFact("temperature_status")
// 	assert.Equal(t, true, alertStatus)

// }

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
