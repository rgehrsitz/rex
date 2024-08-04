// rex/e2e_test.go
package main

import (
	"os"
	"testing"
	"time"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
)

func setupMiniredis(t *testing.T) (*miniredis.Miniredis, *store.RedisStore) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}

	redisStore := store.NewRedisStore(s.Addr(), "", 0)
	return s, redisStore
}

func setupEngine(t *testing.T, jsonData []byte, redisStore *store.RedisStore) *runtime.Engine {
	// Parse JSON ruleset
	ruleset, err := compiler.Parse(jsonData)
	assert.NoError(t, err)

	// Generate bytecode
	BytecodeFile := compiler.GenerateBytecode(ruleset)

	filename := "e2e_test_bytecode.bin"
	err = compiler.WriteBytecodeToFile(filename, BytecodeFile)
	assert.NoError(t, err)

	// Create runtime engine from bytecode file
	engine, err := runtime.NewEngineFromFile(filename, redisStore, 10)
	assert.NoError(t, err)
	assert.NotNil(t, engine)

	return engine
}

func setupPreconditions(redisStore *store.RedisStore) {
	redisStore.SetFact("temperature_status", false)
	redisStore.SetFact("humidity_status", false)
	redisStore.SetFact("complex_status", false)
	redisStore.SetFact("intermediate_status", false)
	redisStore.SetFact("final_status", false)
	redisStore.SetFact("alert", "")
	redisStore.SetFact("temperature", 0.0)
	redisStore.SetFact("humidity", 0.0)
	redisStore.SetFact("pressure", 0.0)
}

func TestEndToEnd(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	jsonData := []byte(`
		{
			"rules": [
				{
					"name": "rule-1",
					"priority": 10,
					"conditions": {
						"any": [
							{
								"fact": "temperature",
								"operator": "GT",
								"value": 30.1
							},
							{
								"fact": "humidity",
								"operator": "LT",
								"value": 60
							}
						]
					},
					"actions": [
						{
							"type": "updateStore",
							"target": "temperature_status",
							"value": true
						}
					]
				}
			]
		}
	`)

	engine := setupEngine(t, jsonData, redisStore)
	redisStore.SetFact("humidity", 70.0)
	redisStore.SetFact("temperature", 0.0)

	// Process fact update
	engine.ProcessFactUpdate("temperature", 30.11)

	// Verify the fact update
	facts, _ := redisStore.GetFact("temperature_status")
	assert.Equal(t, true, facts)

	// Clean up
	os.Remove("e2e_test_bytecode.bin")
}

func TestEndToEndWithMultipleRules(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	jsonData := []byte(`
		{
			"rules": [
				{
					"name": "rule-1",
					"priority": 10,
					"conditions": {
						"all": [
							{
								"fact": "temp",
								"operator": "GT",
								"value": 30.1
							}
						]
					},
					"actions": [
						{
							"type": "updateStore",
							"target": "temp_status",
							"value": true
						}
					]
				},
				{
					"name": "rule-2",
					"priority": 2,
					"conditions": {
						"all": [
							{
								"fact": "humi",
								"operator": "LT",
								"value": 60
							}
						]
					},
					"actions": [
						{
							"type": "updateStore",
							"target": "humi_status",
							"value": true
						}
					]
				}
			]
		}
	`)

	engine := setupEngine(t, jsonData, redisStore)

	// Process fact update
	engine.ProcessFactUpdate("humi", 59.1)

	// Verify the fact update
	fact, _ := redisStore.GetFact("humi_status")
	assert.Equal(t, true, fact)

	// Clean up
	os.Remove("e2e_test_bytecode.bin")
}

func TestComplexConditions(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	jsonData := []byte(`
		{
			"rules": [
				{
					"name": "rule-1",
					"priority": 1,
					"conditions": {
						"any": [
							{
								"fact": "pressure",
								"operator": "EQ",
								"value": 1013
							},
							{
								"all": [
									{
										"fact": "temperature",
										"operator": "GT",
										"value": 30.1
									},
									{
										"fact": "humidity",
										"operator": "LT",
										"value": 60
									}
								]
							}
						]
					},
					"actions": [
						{
							"type": "updateStore",
							"target": "complex_status",
							"value": true
						}
					]
				}
			]
		}
	`)

	setupPreconditions(redisStore)
	engine := setupEngine(t, jsonData, redisStore)

	// Set initial values for the facts
	redisStore.SetFact("temperature", 30)
	redisStore.SetFact("humidity", 59.9)
	redisStore.SetFact("pressure", 1012)
	// Process fact updates
	engine.ProcessFactUpdate("temperature", 30.2)

	// Set initial values for the facts
	redisStore.SetFact("temperature", 30.2)
	redisStore.SetFact("humidity", 59.9)
	redisStore.SetFact("pressure", 1012)
	engine.ProcessFactUpdate("humidity", 59.9)

	// Set initial values for the facts
	redisStore.SetFact("temperature", 30)
	redisStore.SetFact("humidity", 61)
	redisStore.SetFact("pressure", 1010)
	engine.ProcessFactUpdate("pressure", 1013)

	// Verify update in the store
	complexStatus, _ := redisStore.GetFact("complex_status")
	assert.Equal(t, true, complexStatus)

	// Clean up
	os.Remove("e2e_test_bytecode.bin")
}

func TestDifferentActions(t *testing.T) {

	s, redisStore := setupMiniredis(t)
	defer s.Close()

	jsonData := []byte(`
		{
			"rules": [
				{
					"name": "rule-1",
					"priority": 10,
					"conditions": {
						"all": [
							{
								"fact": "temperature",
								"operator": "GT",
								"value": 30.1
							}
						]
					},
					"actions": [
						{
							"type": "updateStore",
							"target": "temperature_status",
							"value": true
						},
						{
							"type": "updateStore",
							"target": "alert",
							"value": "high temperature"
						}
					]
				}
			]
		}
	`)

	engine := setupEngine(t, jsonData, redisStore)

	// Set initial values for the facts
	redisStore.SetFact("temperature", 0.2)

	// Process fact update
	engine.ProcessFactUpdate("temperature", 30.2)

	// Verify updates in the store
	tempStatus, _ := redisStore.GetFact("temperature_status")
	assert.Equal(t, true, tempStatus)

	alert, _ := redisStore.GetFact("alert")
	assert.Equal(t, "high temperature", alert)

	// Clean up
	os.Remove("e2e_test_bytecode.bin")
}

// // This test is intentionally failing for now due to the chaining of rules being turned off
// // until we decide if we want that behavior or not
// func TestRuleChaining(t *testing.T) {
// 	jsonData := []byte(`
// 		{
// 			"rules": [
// 				{
// 					"name": "rule-1",
// 					"priority": 10,
// 					"conditions": {
// 						"all": [
// 							{
// 								"fact": "temperature",
// 								"operator": "GT",
// 								"value": 30.1
// 							}
// 						]
// 					},
// 					"actions": [
// 						{
// 							"type": "updateStore",
// 							"target": "intermediate_status",
// 							"value": true
// 						}
// 					]
// 				},
// 				{
// 					"name": "rule-2",
// 					"priority": 5,
// 					"conditions": {
// 						"all": [
// 							{
// 								"fact": "intermediate_status",
// 								"operator": "EQ",
// 								"value": true
// 							}
// 						]
// 					},
// 					"actions": [
// 						{
// 							"type": "updateStore",
// 							"target": "final_status",
// 							"value": true
// 						}
// 					]
// 				}
// 			]
// 		}
// 	`)

// 	// Parse JSON ruleset
// 	ruleset, err := compiler.Parse(jsonData)
// 	assert.NoError(t, err)

// 	// Generate bytecode
// 	BytecodeFile := compiler.GenerateBytecode(ruleset)

// 	filename := "e2e_test_bytecode.bin"
// 	err = compiler.WriteBytecodeToFile(filename, BytecodeFile)
// 	assert.NoError(t, err)

// 	redisStore := store.NewRedisStore("localhost:6379", "", 0)
// 	// Create runtime engine from bytecode file
// 	engine, err := runtime.NewEngineFromFile(filename, redisStore)

// 	assert.NoError(t, err)
// 	assert.NotNil(t, engine)

// 	// Process fact update
// 	engine.ProcessFactUpdate("temperature", 30.2)

// 	// Verify updates in the store
// 	intermediateStatus, _ := redisStore.GetFact("intermediate_status")
// 	assert.Equal(t, true, intermediateStatus)

// 	finalStatus, _ := redisStore.GetFact("final_status")
// 	assert.Equal(t, true, finalStatus)
// }

func TestNoRulesMatching(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	jsonData := []byte(`
		{
			"rules": [
				{
					"name": "rule-1",
					"priority": 10,
					"conditions": {
						"all": [
							{
								"fact": "temperature",
								"operator": "GT",
								"value": 100
							}
						]
					},
					"actions": [
						{
							"type": "updateStore",
							"target": "temperature_status",
							"value": true
						}
					]
				}
			]
		}
	`)

	engine := setupEngine(t, jsonData, redisStore)
	setupPreconditions(redisStore)

	// Set initial values for the facts
	redisStore.SetFact("temperature", 30.11)

	// Process fact update
	engine.ProcessFactUpdate("temperature", 30.11)

	// Verify no updates in the store
	facts, _ := redisStore.GetFact("temperature_status")
	assert.Equal(t, false, facts)

	// Clean up
	os.Remove("e2e_test_bytecode.bin")
}

func TestMultipleRulesMatching(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	jsonData := []byte(`
		{
			"rules": [
				{
					"name": "rule-1",
					"priority": 10,
					"conditions": {
						"all": [
							{
								"fact": "temperature",
								"operator": "GT",
								"value": 30.1
							}
						]
					},
					"actions": [
						{
							"type": "updateStore",
							"target": "temperature_status",
							"value": true
						}
					]
				},
				{
					"name": "rule-2",
					"priority": 5,
					"conditions": {
						"all": [
							{
								"fact": "humidity",
								"operator": "LT",
								"value": 60
							}
						]
					},
					"actions": [
						{
							"type": "updateStore",
							"target": "humidity_status",
							"value": true
						}
					]
				}
			]
		}
	`)

	engine := setupEngine(t, jsonData, redisStore)

	// Set initial values for the facts
	redisStore.SetFact("temperature", 30.11)
	redisStore.SetFact("humidity", 59.1)

	// Process fact updates
	engine.ProcessFactUpdate("temperature", 30.11)
	engine.ProcessFactUpdate("humidity", 59.1)

	// Verify updates in the store
	tempStatus, _ := redisStore.GetFact("temperature_status")
	assert.Equal(t, true, tempStatus)

	humidityStatus, _ := redisStore.GetFact("humidity_status")
	assert.Equal(t, true, humidityStatus)

	// Clean up
	os.Remove("e2e_test_bytecode.bin")
}

func TestEndToEndComplexRuleChaining(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	jsonData := []byte(`
    {
        "rules": [
            {
                "name": "temperature_rule",
                "conditions": {
                    "all": [
                        {
                            "fact": "temperature",
                            "operator": "GT",
                            "value": 30
                        }
                    ]
                },
                "actions": [
                    {
                        "type": "updateStore",
                        "target": "temperature_status",
                        "value": "high"
                    }
                ]
            },
            {
                "name": "alert_rule",
                "conditions": {
                    "all": [
                        {
                            "fact": "temperature_status",
                            "operator": "EQ",
                            "value": "high"
                        },
                        {
                            "fact": "humidity",
                            "operator": "GT",
                            "value": 70
                        }
                    ]
                },
                "actions": [
                    {
                        "type": "updateStore",
                        "target": "alert",
                        "value": "high_temp_and_humidity"
                    }
                ]
            }
        ]
    }`)

	engine := setupEngine(t, jsonData, redisStore)

	// Set initial values
	redisStore.SetFact("temperature", 25)
	redisStore.SetFact("humidity", 60)

	// Trigger the first rule
	engine.ProcessFactUpdate("temperature", 35)

	// Check intermediate result
	tempStatus, _ := redisStore.GetFact("temperature_status")
	assert.Equal(t, "high", tempStatus)

	// Trigger the second rule
	engine.ProcessFactUpdate("humidity", 75)

	// Check final result
	alert, _ := redisStore.GetFact("alert")
	assert.Equal(t, "high_temp_and_humidity", alert)
}

func TestEndToEndWithScript(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	jsonData := []byte(`
    {
        "rules": [
            {
                "name": "rule-with-script",
                "priority": 10,
                "conditions": {
                    "all": [
                        {
                            "fact": "temperature",
                            "operator": "GT",
                            "value": 30.1
                        }
                    ]
                },
                "actions": [
                    {
                        "type": "updateStore",
                        "target": "temperature_status",
                        "value": "{calculate_status}"
                    }
                ],
                "scripts": {
                    "calculate_status": {
                        "params": ["temperature"],
                        "body": "return temperature > 30 ? 'hot' : 'cold';"
                    }
                }
            }
        ]
    }`)

	engine := setupEngine(t, jsonData, redisStore)
	err := engine.ScriptEngine.SetScript("calculate_status", compiler.Script{
		Params: []string{"temperature"},
		Body:   "return temperature > 30 ? 'hot' : 'cold';",
	})
	assert.NoError(t, err)

	err = redisStore.SetFact("temperature", 31.0)
	assert.NoError(t, err)

	// Process fact update
	engine.ProcessFactUpdate("temperature", 31.0)

	// Wait for a short time to allow for async processing
	time.Sleep(100 * time.Millisecond)

	// Verify the fact update
	tempStatus, err := redisStore.GetFact("temperature_status")
	assert.NoError(t, err)
	t.Logf("Retrieved temperature_status: %v", tempStatus)

	// Check all facts in Redis
	allFacts, err := redisStore.MGetFacts("temperature", "temperature_status")
	assert.NoError(t, err)
	t.Logf("All facts in Redis: %v", allFacts)

	assert.Equal(t, "hot", tempStatus, "Expected temperature_status to be 'hot'")

	// Clean up
	os.Remove("e2e_test_bytecode.bin")
}

func TestScriptWithMultipleParams(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	jsonData := []byte(`
	{
		"rules": [
			{
				"name": "rule-with-script-multiple-params",
				"priority": 10,
				"conditions": {
					"all": [
						{
							"fact": "temperature",
							"operator": "GT",
							"value": 30.1
						},
						{
							"fact": "humidity",
							"operator": "GT",
							"value": 70
						}
					]
				},
				"actions": [
					{
						"type": "updateStore",
						"target": "heat_index",
						"value": "{calculate_heat_index}"
					}
				],
				"scripts": {
					"calculate_heat_index": {
						"params": ["temperature", "humidity"],
						"body": "return temperature * 1.8 + 32 + (humidity / 100) * 10;"
					}
				}
			}
		]
	}`)

	engine := setupEngine(t, jsonData, redisStore)
	err := engine.ScriptEngine.SetScript("calculate_heat_index", compiler.Script{
		Params: []string{"temperature", "humidity"},
		Body:   "return temperature * 1.8 + 32 + (humidity / 100) * 10;",
	})
	assert.NoError(t, err)

	err = redisStore.SetFact("temperature", 31.0)
	assert.NoError(t, err)
	err = redisStore.SetFact("humidity", 75.0)
	assert.NoError(t, err)

	// Process fact update
	engine.ProcessFactUpdate("temperature", 31.0)

	// Wait for a short time to allow for async processing
	time.Sleep(100 * time.Millisecond)

	// Verify the fact update
	heatIndex, err := redisStore.GetFact("heat_index")
	assert.NoError(t, err)
	t.Logf("Retrieved heat_index: %v", heatIndex)

	// Check all facts in Redis
	allFacts, err := redisStore.MGetFacts("temperature", "humidity", "heat_index")
	assert.NoError(t, err)
	t.Logf("All facts in Redis: %v", allFacts)

	if heatIndex != nil {
		expectedHeatIndex := 31.0*1.8 + 32 + (75.0/100)*10
		assert.InDelta(t, expectedHeatIndex, heatIndex.(float64), 0.1, "Heat index calculation is incorrect")
	} else {
		t.Error("heat_index is nil")
	}

	// Clean up
	os.Remove("e2e_test_bytecode.bin")
}

func TestScriptErrorHandling(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	jsonData := []byte(`
	{
		"rules": [
			{
				"name": "rule-with-error-script",
				"priority": 10,
				"conditions": {
					"all": [
						{
							"fact": "temperature",
							"operator": "GT",
							"value": 30.1
						}
					]
				},
				"actions": [
					{
						"type": "updateStore",
						"target": "status",
						"value": "{error_script}"
					}
				],
				"scripts": {
					"error_script": {
						"params": ["temperature"],
						"body": "return temperature.unknownMethod();"
					}
				}
			}
		]
	}`)

	engine := setupEngine(t, jsonData, redisStore)
	err := engine.ScriptEngine.SetScript("error_script", compiler.Script{
		Params: []string{"temperature"},
		Body:   "return temperature.unknownMethod();",
	})
	assert.NoError(t, err)

	redisStore.SetFact("temperature", 31.0)

	// Process fact update
	engine.ProcessFactUpdate("temperature", 31.0)

	// Verify no update in the store due to script error
	status, _ := redisStore.GetFact("status")
	assert.Nil(t, status)

	// Clean up
	os.Remove("e2e_test_bytecode.bin")
}

func TestScriptEdgeCases(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	jsonData := []byte(`
	{
		"rules": [
			{
				"name": "rule-with-edge-case-script",
				"priority": 10,
				"conditions": {
					"all": [
						{
							"fact": "temperature",
							"operator": "GT",
							"value": 30.1
						}
					]
				},
				"actions": [
					{
						"type": "updateStore",
						"target": "status",
						"value": "{edge_case_script}"
					}
				],
				"scripts": {
					"edge_case_script": {
						"params": ["temperature"],
						"body": "return temperature * 2 / 0;"
					}
				}
			}
		]
	}`)

	engine := setupEngine(t, jsonData, redisStore)
	err := engine.ScriptEngine.SetScript("edge_case_script", compiler.Script{
		Params: []string{"temperature"},
		Body:   "return temperature * 2 / 0;",
	})
	assert.NoError(t, err)

	redisStore.SetFact("temperature", 31.0)

	// Process fact update
	engine.ProcessFactUpdate("temperature", 31.0)

	// Verify no update in the store due to edge case handling
	status, _ := redisStore.GetFact("status")
	assert.Nil(t, status)

	// Clean up
	os.Remove("e2e_test_bytecode.bin")
}
