package engine_test

import (
	"rgehrsitz/rex/internal/engine"
	"rgehrsitz/rex/internal/store/mock"
	"rgehrsitz/rex/pkg/rule"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEvaluateRuleWithStore(t *testing.T) {
	// Create a mock store
	mockStore := mock.NewMockStore()
	mockStore.SetValue("SensorY", 15)
	mockStore.SetValue("SensorZ", 20)

	// Define a rule
	testRule := rule.Rule{
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{Fact: "SensorX", Operator: "greaterThan", Value: 10},
				{Fact: "SensorY", Operator: "lessThan", Value: 20},
			},
			Any: []rule.Condition{
				{Fact: "SensorZ", Operator: "equal", Value: 20},
			},
		},
		// ... other rule fields ...
	}

	// Evaluate the rule
	err := engine.EvaluateRuleWithStore(testRule, "SensorX", 12, mockStore)
	assert.NoError(t, err, "EvaluateRuleWithStore should not return an error")
	// Additional assertions based on expected behavior

	// Test with different sensor values
	err = engine.EvaluateRuleWithStore(testRule, "SensorX", 5, mockStore) // Value that should not satisfy the rule
	assert.NoError(t, err, "EvaluateRuleWithStore should not return an error for unsatisfied conditions")

	// Test action triggering
	// Assuming testRule has an action defined to update a value in the store
	testRule.Event.Action = rule.Action{
		Type:   "updateStore",
		Target: "SensorResult",
		Value:  "Passed",
	}
	err = engine.EvaluateRuleWithStore(testRule, "SensorX", 12, mockStore)
	assert.NoError(t, err)
	result, _ := mockStore.GetValue("SensorResult")
	assert.Equal(t, "Passed", result)

	// Test error handling in store interactions
	// Simulate error by fetching a non-existent sensor value
	err = engine.EvaluateRuleWithStore(testRule, "NonExistentSensor", 12, mockStore)
	assert.Error(t, err, "EvaluateRuleWithStore should return an error for non-existent sensor values")

	// Test with complex nested conditions
	// Define a rule with nested conditions and test its evaluation

	// Test rule evaluation with no actions
	testRule.Event.Action = rule.Action{} // No action
	err = engine.EvaluateRuleWithStore(testRule, "SensorX", 12, mockStore)
	assert.NoError(t, err, "EvaluateRuleWithStore should not return an error even if no actions are defined")
}
