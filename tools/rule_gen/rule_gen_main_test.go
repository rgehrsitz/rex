// rex/tools/rule_gen/rule_gen_main_test.go

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseFlags(t *testing.T) {
	// Test case 1: Default values
	numRules, outputFile := parseFlags([]string{})
	assert.Equal(t, 1000, numRules)
	assert.Equal(t, "generated_ruleset.json", outputFile)

	// Test case 2: Custom values
	numRules, outputFile = parseFlags([]string{"-rules", "500", "-output", "custom_ruleset.json"})
	assert.Equal(t, 500, numRules)
	assert.Equal(t, "custom_ruleset.json", outputFile)
}

func TestGenerateRuleset(t *testing.T) {
	numRules := 10
	ruleset := generateRuleset(numRules)

	assert.Len(t, ruleset.Rules, numRules)
	for i, rule := range ruleset.Rules {
		assert.Equal(t, fmt.Sprintf("rule-%d", i+1), rule.Name)
		assert.True(t, rule.Priority > 0 && rule.Priority <= 20)
		assert.NotEmpty(t, rule.Conditions)
		assert.NotEmpty(t, rule.Actions)
	}
}

func TestGenerateRule(t *testing.T) {
	rule := generateRule(1)

	assert.Equal(t, "rule-1", rule.Name)
	assert.True(t, rule.Priority > 0 && rule.Priority <= 20)
	assert.NotEmpty(t, rule.Conditions)
	assert.NotEmpty(t, rule.Actions)
}

func TestGenerateCondition(t *testing.T) {
	condition := generateCondition(0)

	if condition.Fact != "" {
		assert.NotEmpty(t, condition.Fact)
		assert.NotEmpty(t, condition.Operator)
		assert.NotNil(t, condition.Value)
	} else {
		assert.True(t, len(condition.All) > 0 || len(condition.Any) > 0)
	}
}

func TestGenerateAction(t *testing.T) {
	action := generateAction()

	assert.Contains(t, []string{"updateStore", "sendMessage"}, action.Type)
	assert.NotEmpty(t, action.Target)
	assert.NotNil(t, action.Value)
}

func TestWriteRulesetToFile(t *testing.T) {
	ruleset := Ruleset{
		Rules: []Rule{
			{
				Name: "test-rule",
				Conditions: ConditionGroup{
					All: []Condition{
						{
							Fact:     "weather:temperature",
							Operator: "GT",
							Value:    25.0,
						},
					},
				},
				Actions: []Action{
					{
						Type:   "updateStore",
						Target: "weather:status",
						Value:  "hot",
					},
				},
			},
		},
	}

	tempFile, err := os.CreateTemp("", "test_ruleset_*.json")
	assert.NoError(t, err)
	defer os.Remove(tempFile.Name())

	err = writeRulesetToFile(ruleset, tempFile.Name())
	assert.NoError(t, err)

	// Read the file and verify its contents
	content, err := os.ReadFile(tempFile.Name())
	assert.NoError(t, err)

	var decodedRuleset Ruleset
	err = json.Unmarshal(content, &decodedRuleset)
	assert.NoError(t, err)

	assert.Equal(t, ruleset, decodedRuleset)
}

func TestGetRandomFact(t *testing.T) {
	channel, fact := getRandomFact()
	assert.Contains(t, channelFacts, channel)
	assert.Contains(t, channelFacts[channel], fact)
}

func TestGetFactType(t *testing.T) {
	assert.Equal(t, "numeric", getFactType("temperature"))
	assert.Equal(t, "boolean", getFactType("temperature_warning"))
	assert.Equal(t, "string", getFactType("unknown_fact"))
}

func TestGenerateValue(t *testing.T) {
	numericValue := generateValue("numeric")
	assert.IsType(t, float64(0), numericValue)

	boolValue := generateValue("boolean")
	assert.IsType(t, bool(false), boolValue)

	stringValue := generateValue("string")
	assert.IsType(t, "", stringValue)
}
