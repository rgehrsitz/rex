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
								"value": 30.0
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
	bytecode := compiler.GenerateBytecode(ruleset)

	// Generate indices
	ruleExecIndex, factRuleLookupIndex, factDependencyIndex := compiler.GenerateIndices(ruleset, bytecode)

	// Create bytecode file
	bytecodeFile := compiler.BytecodeFile{
		Header: compiler.Header{
			Version:       1,
			Checksum:      0,
			ConstPoolSize: 1,
			NumRules:      uint16(len(ruleset.Rules)),
		},
		Instructions:        bytecode,
		RuleExecIndex:       ruleExecIndex,
		FactRuleLookupIndex: factRuleLookupIndex,
		FactDependencyIndex: factDependencyIndex,
	}

	filename := "e2e_test_bytecode.bin"
	err = compiler.WriteBytecodeToFile(filename, bytecodeFile)
	assert.NoError(t, err)

	// Create runtime engine from bytecode file
	engine, err := runtime.NewEngineFromFile(filename)
	assert.NoError(t, err)
	assert.NotNil(t, engine)

	// Process fact update
	engine.ProcessFactUpdate("temperature", 31.0)

	// Verify the fact update
	facts := engine.GetFacts()
	assert.Equal(t, true, facts["temperature_status"])

	// Clean up
	os.Remove(filename)
}
