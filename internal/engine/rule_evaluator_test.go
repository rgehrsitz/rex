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
}
