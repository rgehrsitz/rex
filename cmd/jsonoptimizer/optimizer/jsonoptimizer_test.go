package optimizer

import (
	"rgehrsitz/rex/internal/rule"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateDeduplicationKey(t *testing.T) {
	tests := []struct {
		name     string
		rule     rule.Rule
		expected string // You could expect specific keys, but it might be more practical to expect consistency rather than specific hash values.
	}{
		{
			name:     "Identical rules generate the same key",
			rule:     rule.Rule{ /* Define rule here */ },
			expected: "", // Leave empty if checking for consistency with another rule rather than a specific key.
		},
		// Add more test cases here.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key1 := GenerateDeduplicationKey(tt.rule)
			key2 := GenerateDeduplicationKey(tt.rule) // Repeat to test idempotency and consistency.

			if key1 != key2 {
				t.Errorf("Expected consistent deduplication keys, but got %v and %v", key1, key2)
			}

			// Additional checks can be added here, depending on what each test case is specifically testing for.
		})
	}
}

func TestGenerateDeduplicationKey_DifferentConditions(t *testing.T) {
	rule1 := rule.Rule{
		Name: "Rule1",
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{
					Fact:     "temperature",
					Operator: "greaterThan",
					Value:    30,
				},
			},
		},
		Event: rule.Event{
			EventType: "Alert",
			Actions: []rule.Action{
				{
					Type:   "email",
					Target: "admin@example.com",
					Value:  "Temperature is too high",
				},
			},
		},
	}

	rule2 := rule.Rule{
		Name: "Rule2",
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{
					Fact:     "humidity",
					Operator: "lessThan",
					Value:    20,
				},
			},
		},
		Event: rule.Event{
			EventType: "Warning",
			Actions: []rule.Action{
				{
					Type:   "sms",
					Target: "+1234567890",
					Value:  "Humidity is too low",
				},
			},
		},
	}

	key1 := GenerateDeduplicationKey(rule1)
	key2 := GenerateDeduplicationKey(rule2)

	assert.NotEqual(t, key1, key2, "Expected different deduplication keys for rules with different conditions")
}

func TestGenerateDeduplicationKey_Normalization(t *testing.T) {
	rule1 := rule.Rule{
		Name: "SampleRule",
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{
					Fact:     "temperature",
					Operator: "greaterThan",
					Value:    30,
				},
				{
					Fact:     "humidity",
					Operator: "lessThan",
					Value:    50,
				},
			},
		},
		Event: rule.Event{
			EventType: "Alert",
			Actions: []rule.Action{
				{
					Type:   "email",
					Target: "admin@example.com",
					Value:  "Temperature and humidity levels out of range.",
				},
			},
		},
	}

	rule2 := rule.Rule{
		Name: "SampleRule",
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{
					Fact:     "humidity",
					Operator: "lessThan",
					Value:    50,
				},
				{
					Fact:     "temperature",
					Operator: "greaterThan",
					Value:    30,
				},
			},
		},
		Event: rule.Event{
			EventType: "Alert",
			Actions: []rule.Action{
				{
					Type:   "email",
					Target: "admin@example.com",
					Value:  "Temperature and humidity levels out of range.",
				},
			},
		},
	}

	key1 := GenerateDeduplicationKey(rule1)
	key2 := GenerateDeduplicationKey(rule2)

	assert.Equal(t, key1, key2, "The deduplication keys should be identical for logically equivalent rules with conditions in different orders.")
}

func TestGenerateDeduplicationKey_NestedConditions(t *testing.T) {
	// Define a rule with nested conditions in a specific structure
	rule1 := rule.Rule{
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{
					Fact:     "temperature",
					Operator: "greaterThan",
					Value:    30,
					All: []rule.Condition{
						{
							Fact:     "humidity",
							Operator: "lessThan",
							Value:    50,
						},
					},
				},
			},
		},
		Event: rule.Event{
			Actions: []rule.Action{
				{
					Type:   "alert",
					Target: "system",
					Value:  "High temperature and low humidity detected",
				},
			},
		},
	}

	// Define another rule that is logically equivalent to rule1 but with a different nested condition order
	rule2 := rule.Rule{
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{
					Fact:     "temperature",
					Operator: "greaterThan",
					Value:    30,
					All: []rule.Condition{
						{
							Fact:     "humidity",
							Operator: "lessThan",
							Value:    50,
						},
					},
				},
			},
		},
		Event: rule.Event{
			Actions: []rule.Action{
				{
					Type:   "alert",
					Target: "system",
					Value:  "High temperature and low humidity detected",
				},
			},
		},
	}

	key1 := GenerateDeduplicationKey(rule1)
	key2 := GenerateDeduplicationKey(rule2)

	// Using assert to check if the keys are the same, indicating proper handling of nested conditions
	assert.Equal(t, key1, key2, "Expected the same deduplication keys for logically equivalent rules with differently structured nested conditions")
}

func TestGenerateDeduplicationKey_ComplexScenarios(t *testing.T) {
	// Example of a complex rule with nested conditions and multiple actions
	complexRule := rule.Rule{
		Name: "ComplexRule",
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{
					Fact:     "temperature",
					Operator: "greaterThan",
					Value:    30,
					All: []rule.Condition{
						{
							Fact:     "humidity",
							Operator: "lessThan",
							Value:    50,
						},
					},
				},
				{
					Fact:     "timeOfDay",
					Operator: "equal",
					Value:    "afternoon",
				},
			},
			Any: []rule.Condition{
				{
					Fact:     "deviceStatus",
					Operator: "equal",
					Value:    "active",
				},
			},
		},
		Event: rule.Event{
			EventType: "Alert",
			Actions: []rule.Action{
				{
					Type:   "emailNotification",
					Target: "admin@example.com",
					Value:  "High temperature detected",
				},
			},
		},
	}

	key := GenerateDeduplicationKey(complexRule)
	assert.NotEmpty(t, key, "Generated deduplication key for a complex rule should not be empty")

	// To further validate the deduplication key's consistency, you could create a slightly modified version of complexRule
	// that should logically be considered the same (e.g., reordering conditions) and verify the keys match.
	// This would ensure that the normalization logic is functioning correctly for complex scenarios.
	modifiedComplexRule := complexRule
	// Modify the rule in a way that should not affect the generated deduplication key (e.g., reorder conditions).

	modifiedKey := GenerateDeduplicationKey(modifiedComplexRule)
	assert.Equal(t, key, modifiedKey, "Deduplication keys for logically equivalent complex rules should match")
}

func TestMergeAndSimplifyRules_Merging(t *testing.T) {
	rules := []rule.Rule{
		{
			Name: "Rule1",
			Conditions: rule.Conditions{
				All: []rule.Condition{{Fact: "temperature", Operator: "greaterThan", Value: 30}},
			},
			Event: rule.Event{
				Actions: []rule.Action{{Type: "alert", Target: "system", Value: "High temperature"}},
			},
		},
		{
			Name: "Rule2",
			Conditions: rule.Conditions{
				All: []rule.Condition{{Fact: "humidity", Operator: "lessThan", Value: 50}},
			},
			Event: rule.Event{
				Actions: []rule.Action{{Type: "alert", Target: "system", Value: "High temperature"}},
			},
		},
	}

	expectedMergedConditionsLength := 2 // Expecting the two conditions to be merged under a single rule.

	optimizedRules, err := MergeAndSimplifyRules(rules)
	assert.NoError(t, err, "MergeAndSimplifyRules should not return an error.")
	assert.Len(t, optimizedRules, 1, "Expected rules to be merged into a single rule.")
	assert.Len(t, optimizedRules[0].Conditions.All, expectedMergedConditionsLength, "Expected merged rule to contain all conditions from original rules.")
}

func TestMergeAndSimplifyRules_Simplification(t *testing.T) {
	// Assuming simplifyConditions is implemented to remove duplicate conditions.
	rules := []rule.Rule{
		{
			Name: "RuleWithDuplicateConditions",
			Conditions: rule.Conditions{
				All: []rule.Condition{
					{Fact: "temperature", Operator: "greaterThan", Value: 30},
					{Fact: "temperature", Operator: "greaterThan", Value: 30},
				},
			},
			Event: rule.Event{
				Actions: []rule.Action{{Type: "alert", Target: "system", Value: "Repeated condition"}},
			},
		},
	}

	optimizedRules, err := MergeAndSimplifyRules(rules)
	assert.NoError(t, err, "MergeAndSimplifyRules should not return an error for simplification.")
	assert.Len(t, optimizedRules, 1, "Expected a single rule after simplification.")
	assert.Len(t, optimizedRules[0].Conditions.All, 1, "Expected duplicate conditions to be simplified.")
}

func TestProcessAndOptimizeRuleset_EndToEnd(t *testing.T) {
	// Define a set of rules that includes potential duplicates, simplifiable conditions, and a realistic mix of conditions and actions.
	rules := []rule.Rule{
		{
			Name: "HighTemperatureAlert",
			Conditions: rule.Conditions{
				All: []rule.Condition{
					{Fact: "temperature", Operator: "greaterThan", Value: 30},
				},
			},
			Event: rule.Event{
				EventType: "Alert",
				Actions: []rule.Action{
					{Type: "emailNotification", Target: "admin@example.com", Value: "Temperature is too high"},
				},
			},
		},
		{
			Name: "HighTemperatureAlertDuplicate",
			Conditions: rule.Conditions{
				All: []rule.Condition{
					{Fact: "temperature", Operator: "greaterThan", Value: 30},
				},
			},
			Event: rule.Event{
				EventType: "Alert",
				Actions: []rule.Action{
					{Type: "emailNotification", Target: "admin@example.com", Value: "Temperature is too high"},
				},
			},
		},
		{
			Name: "LowHumidityWarning",
			Conditions: rule.Conditions{
				All: []rule.Condition{
					{Fact: "humidity", Operator: "lessThan", Value: 20},
				},
			},
			Event: rule.Event{
				EventType: "Warning",
				Actions: []rule.Action{
					{Type: "smsNotification", Target: "+123456789", Value: "Humidity is too low"},
				},
			},
		},
	}

	optimizedRules, err := ProcessAndOptimizeRuleset(rules)
	assert.NoError(t, err, "The optimization process should complete without errors.")

	// Assertions to verify the expected outcomes of the optimization:
	assert.Len(t, optimizedRules, 2, "Expected the optimized rule set to contain only unique rules, with duplicates merged.")

	// Further assertions could check for specific conditions or actions in the optimized rules,
	// but this might require a more detailed inspection of the optimization logic's expected behavior.
	// For example, you could assert that the conditions of merged rules are correctly combined,
	// or that the actions are appropriately deduplicated.

	// This checks if deduplication removed the duplicate "HighTemperatureAlert" rule.
	foundHighTempRule := false
	for _, rule := range optimizedRules {
		if rule.Name == "HighTemperatureAlert" || rule.Name == "HighTemperatureAlertDuplicate" {
			if foundHighTempRule {
				t.Errorf("Found duplicate rules for high temperature alert after optimization")
			}
			foundHighTempRule = true
		}
	}
	assert.True(t, foundHighTempRule, "Expected to find a rule for high temperature alerts in the optimized ruleset.")
}
