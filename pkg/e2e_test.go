// rex/e2e_test.go
package main

import (
	"os"
	"testing"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"

	"github.com/stretchr/testify/assert"
)

func TestEndToEnd(t *testing.T) {
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

	// Parse JSON ruleset
	ruleset, err := compiler.Parse(jsonData)
	assert.NoError(t, err)

	// Generate bytecode
	BytecodeFile := compiler.GenerateBytecode(ruleset)

	filename := "e2e_test_bytecode.bin"
	err = compiler.WriteBytecodeToFile(filename, BytecodeFile)
	assert.NoError(t, err)

	redisStore := store.NewRedisStore("localhost:6379", "", 0)
	// Create runtime engine from bytecode file
	engine, err := runtime.NewEngineFromFile(filename, redisStore)
	assert.NoError(t, err)
	assert.NotNil(t, engine)

	// Process fact update
	engine.ProcessFactUpdate("temperature", 30.11)

	testStore := store.NewRedisStore("localhost:6379", "", 0)

	// Verify the fact update
	facts, _ := testStore.GetFact("temperature_status")
	assert.Equal(t, true, facts)

	// Clean up
	os.Remove(filename)
}

func TestEndToEndWithMultipleRules(t *testing.T) {
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

	// Parse JSON ruleset
	ruleset, err := compiler.Parse(jsonData)
	assert.NoError(t, err)

	// Generate bytecode
	BytecodeFile := compiler.GenerateBytecode(ruleset)

	filename := "e2e_test_bytecode.bin"
	err = compiler.WriteBytecodeToFile(filename, BytecodeFile)
	assert.NoError(t, err)

	redisStore := store.NewRedisStore("localhost:6379", "", 0)
	// Create runtime engine from bytecode file
	engine, err := runtime.NewEngineFromFile(filename, redisStore)

	assert.NoError(t, err)
	assert.NotNil(t, engine)

	// Process fact update
	engine.ProcessFactUpdate("humi", 59.1)

	testStore := store.NewRedisStore("localhost:6379", "", 0)

	// Verify the fact update
	fact, _ := testStore.GetFact("temperature_status")
	assert.Equal(t, true, fact)

	// Clean up
	os.Remove(filename)
}

func TestComplexConditions(t *testing.T) {
	jsonData := []byte(`
{
  "rules": [
    {
      "name": "rule-1",
      "priority": 10,
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

	// Parse JSON ruleset
	ruleset, err := compiler.Parse(jsonData)
	assert.NoError(t, err)

	// Generate bytecode
	BytecodeFile := compiler.GenerateBytecode(ruleset)

	filename := "e2e_test_bytecode.bin"
	err = compiler.WriteBytecodeToFile(filename, BytecodeFile)
	assert.NoError(t, err)

	redisStore := store.NewRedisStore("localhost:6379", "", 0)
	// Create runtime engine from bytecode file
	engine, err := runtime.NewEngineFromFile(filename, redisStore)

	assert.NoError(t, err)
	assert.NotNil(t, engine)

	// Process fact updates
	//engine.ProcessFactUpdate("temperature", 30.2)
	//engine.ProcessFactUpdate("humidity", 59.9)
	engine.ProcessFactUpdate("pressure", 1013)

	testStore := store.NewRedisStore("localhost:6379", "", 0)

	// Verify update in the store
	complexStatus, _ := testStore.GetFact("complex_status")
	assert.Equal(t, true, complexStatus)
}

func TestDifferentActions(t *testing.T) {
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

	// Parse JSON ruleset
	ruleset, err := compiler.Parse(jsonData)
	assert.NoError(t, err)

	// Generate bytecode
	BytecodeFile := compiler.GenerateBytecode(ruleset)

	filename := "e2e_test_bytecode.bin"
	err = compiler.WriteBytecodeToFile(filename, BytecodeFile)
	assert.NoError(t, err)

	redisStore := store.NewRedisStore("localhost:6379", "", 0)
	// Create runtime engine from bytecode file
	engine, err := runtime.NewEngineFromFile(filename, redisStore)

	assert.NoError(t, err)
	assert.NotNil(t, engine)

	// Process fact update
	engine.ProcessFactUpdate("temperature", 30.2)

	testStore := store.NewRedisStore("localhost:6379", "", 0)

	// Verify updates in the store
	tempStatus, _ := testStore.GetFact("temperature_status")
	assert.Equal(t, true, tempStatus)

	alert, _ := testStore.GetFact("alert")
	assert.Equal(t, "high temperature", alert)
}
