package optimizer_test

import (
	"encoding/json"
	"rgehrsitz/rex/cmd/jsonoptimizer/optimizer"
	"rgehrsitz/rex/internal/rule"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEstimateConditionCost(t *testing.T) {
	// Create an instance of Optimizer with any necessary initial state.
	optim := optimizer.New(false)

	// Define test cases.
	testCases := []struct {
		name     string
		cond     rule.Condition
		expected int
	}{
		{
			name:     "Simple equality condition",
			cond:     rule.Condition{Fact: "temperature", Operator: "equal", Value: 30},
			expected: 1, // Assuming "equal" has a base cost of 1.
		},
		{
			name: "Complex nested condition",
			cond: rule.Condition{
				All: []rule.Condition{
					{Fact: "windSpeed", Operator: "greaterThan", Value: 25},
					{Fact: "visibility", Operator: "lessThan", Value: 1000},
				},
			},
			expected: 4, // Sum of the individual costs if "greaterThan" and "lessThan" have a base cost of 2.
		},
		// Add more test cases for different conditions and complexities.
	}

	// Run test cases.
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Use assert.Equal to simplify the assertion logic
			cost := optim.EstimateConditionCost(tc.cond)
			assert.Equal(t, tc.expected, cost, "EstimateConditionCost(%+v)", tc.cond)
		})
	}
}

func TestOptimizeRule(t *testing.T) {
	// Setup an Optimizer instance for testing.
	optim := optimizer.New(false)

	// Initialize the assert instance
	assert := assert.New(t)

	// Define test cases.
	testCases := []struct {
		name         string
		rule         rule.Rule
		expectedRule rule.Rule
	}{
		{
			name: "Rule with mixed conditions",
			rule: rule.Rule{
				Conditions: rule.Conditions{
					Any: []rule.Condition{
						{Fact: "humidity", Operator: "greaterThan", Value: 80},
						{
							All: []rule.Condition{
								{Fact: "windSpeed", Operator: "greaterThan", Value: 25},
								{Fact: "visibility", Operator: "lessThan", Value: 1000},
							},
						},
						{Fact: "temperature", Operator: "greaterThan", Value: 30},
					},
				},
			},
			expectedRule: rule.Rule{
				Conditions: rule.Conditions{
					Any: []rule.Condition{
						{Fact: "humidity", Operator: "greaterThan", Value: 80},
						{Fact: "temperature", Operator: "greaterThan", Value: 30},
						{
							All: []rule.Condition{
								{Fact: "windSpeed", Operator: "greaterThan", Value: 25},
								{Fact: "visibility", Operator: "lessThan", Value: 1000},
							},
						},
					},
				},
			},
		},
		{
			name: "Redundant conditions elimination",
			rule: rule.Rule{
				Conditions: rule.Conditions{
					Any: []rule.Condition{
						{Fact: "temperature", Operator: "greaterThan", Value: 25},
						{Fact: "temperature", Operator: "greaterThan", Value: 25},
					},
				},
			},
			expectedRule: rule.Rule{
				Conditions: rule.Conditions{
					Any: []rule.Condition{
						{Fact: "temperature", Operator: "greaterThan", Value: 25},
					},
				},
			},
		},

		// ... more test cases ...
	}

	// Run test cases.
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Make a deep copy of the rule to avoid modifying the original test case data.
			ruleCopy := tc.rule
			// Apply optimization to the copy.
			optim.OptimizeRule(&ruleCopy)
			// Use assert.Equal to compare the optimized rule with the expected rule.
			assert.Equal(tc.expectedRule, ruleCopy, "OptimizeRule() did not produce the expected result")
		})
	}
}

func TestOptimizationEndToEnd(t *testing.T) {
	assert := assert.New(t) // Initialize assert object

	// Example JSON input
	inputJSON := `{
      "name": "AdultUser",
      "priority": 1,
      "conditions": {
        "all": [
          {
            "fact": "age",
            "operator": "greaterThanOrEqual",
            "value": 18
          }
        ]
      },
      "event": {
        "eventType": "UserIsAdult",
        "customProperty": "User has reached adulthood."
      }
    }`

	// Expected JSON output (for example, the same as input in this simplified case)
	expectedJSON := inputJSON // In a real test, this would reflect the expected optimizations.

	// Convert input JSON to internal rule representation
	var inputRule rule.Rule
	err := json.Unmarshal([]byte(inputJSON), &inputRule)
	assert.NoError(err, "Failed to unmarshal input JSON")

	// Optimize the rule
	optim := optimizer.New(false)
	optimizedRules, err := optim.OptimizeRules([]rule.Rule{inputRule})
	assert.NoError(err, "Failed to optimize rules")

	// Convert optimized rule back to JSON
	optimizedJSONBytes, err := json.Marshal(optimizedRules[0])
	assert.NoError(err, "Failed to marshal optimized rule to JSON")
	optimizedJSON := string(optimizedJSONBytes)

	// Using JSON structural equivalence for comparison
	var expected, actual interface{}
	err = json.Unmarshal([]byte(expectedJSON), &expected)
	assert.NoError(err, "Failed to unmarshal expected JSON")

	err = json.Unmarshal([]byte(optimizedJSON), &actual)
	assert.NoError(err, "Failed to unmarshal actual optimized JSON")

	// Compare expected and actual optimized JSON for structural equivalence
	assert.Equal(expected, actual, "Optimized JSON did not match expected output")
}

func TestDeeplyNestedConditionOptimization(t *testing.T) {
	optim := optimizer.New(false)

	// Define the deeply nested rule to optimize
	ruleToOptimize := rule.Rule{
		Conditions: rule.Conditions{
			Any: []rule.Condition{
				{Fact: "humidity", Operator: "lessThan", Value: 50},
				{Fact: "fud", Operator: "lessThan", Value: 50},
				{Fact: "blah", Operator: "lessThan", Value: 50},
				{
					All: []rule.Condition{
						{Fact: "temperature", Operator: "equal", Value: 30},
						{Fact: "windSpeed", Operator: "equal", Value: 10},
					},
				},
			},
		},
	}

	expectedOptimizedRule := rule.Rule{
		Conditions: rule.Conditions{
			Any: []rule.Condition{
				{Fact: "humidity", Operator: "lessThan", Value: 50},
				{Fact: "fud", Operator: "lessThan", Value: 50},
				{Fact: "blah", Operator: "lessThan", Value: 50},
				{
					All: []rule.Condition{
						{Fact: "temperature", Operator: "equal", Value: 30},
						{Fact: "windSpeed", Operator: "equal", Value: 10},
					},
				},
			},
		},
	}

	// Apply optimization
	optimizedRules, err := optim.OptimizeRules([]rule.Rule{ruleToOptimize})
	assert.NoError(t, err, "Optimization should not produce an error")

	assert.Equal(t, optimizedRules[0], expectedOptimizedRule, "Optimized rule does not match expected outcome")
}
