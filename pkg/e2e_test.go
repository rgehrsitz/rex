// rex/e2e_test.go
package main

import (
	"os"
	"testing"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/runtime"

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

	// Create runtime engine from bytecode file
	engine, err := runtime.NewEngineFromFile(filename)
	assert.NoError(t, err)
	assert.NotNil(t, engine)

	// Process fact update
	engine.ProcessFactUpdate("temperature", 31.0)

	// Verify the fact update
	// facts := engine.GetFacts()
	// assert.Equal(t, true, facts["temperature_status"])

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

	// Create runtime engine from bytecode file
	engine, err := runtime.NewEngineFromFile(filename)
	assert.NoError(t, err)
	assert.NotNil(t, engine)

	// Process fact update
	engine.ProcessFactUpdate("temp", 60.1)

	// Verify the fact update
	// facts := engine.GetFacts()
	// assert.Equal(t, true, facts["temperature_status"])

	// Clean up
	os.Remove(filename)
}
