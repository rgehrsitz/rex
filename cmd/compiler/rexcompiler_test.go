package main

import (
	"os"
	"reflect"
	"rgehrsitz/rex/internal/rule"
	"testing"
)

// TestReadAndParseRules tests the readAndParseRules function
func TestReadAndParseRules(t *testing.T) {
	// Create a temporary directory to store test rule files
	tempDir, err := os.MkdirTemp("", "testrules")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %s", err)
	}
	defer os.RemoveAll(tempDir) // Clean up after the test

	// Define test cases
	testCases := []struct {
		name      string
		ruleData  string
		expectErr bool
		expected  rule.Rule
	}{
		{
			name: "ValidRule",
			ruleData: `[{
            "name": "TestTemperatureRule",
            "priority": 1,
            "conditions": {
                "all": [
                    {
                        "fact": "temperature",
                        "operator": "greaterThan",
                        "value": 30
                    }
                ]
            },
            "event": {
                "eventType": "alert",
                "actions": [
                    {
                        "type": "updateStore",
                        "target": "roomStatus",
                        "value": "hot"
                    }
                ]
            }
        }]`,
			expectErr: false,
			expected: rule.Rule{
				Name:     "TestTemperatureRule",
				Priority: 1,
				Conditions: rule.Conditions{
					All: []rule.Condition{
						{
							Fact:     "temperature",
							Operator: "greaterThan",
							Value:    30.0,
						},
					},
				},
				Event: rule.Event{
					EventType: "alert",
					Actions: []rule.Action{
						{
							Type:   "updateStore",
							Target: "roomStatus",
							Value:  "hot",
						},
					},
				},
			},
		},
		// ... other test cases
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a temporary file for the rule
			tempFile, err := os.CreateTemp(tempDir, "*.json")
			if err != nil {
				t.Fatalf("Failed to create temp file: %s", err)
			}
			tempFilePath := tempFile.Name()
			defer tempFile.Close()

			// Write the rule data to the file
			_, err = tempFile.WriteString(tc.ruleData)
			if err != nil {
				t.Fatalf("Failed to write to temp file: %s", err)
			}

			rules, err := readAndParseRules(tempFilePath)
			if tc.expectErr {
				if err == nil {
					t.Errorf("Expected an error, but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %s", err)
				}
				if len(rules) != 1 {
					t.Errorf("Expected 1 rule, got %d", len(rules))
				} else {
					parsedRule := rules[0]
					expectedRule := tc.expected

					if parsedRule.Name != expectedRule.Name {
						t.Errorf("Name mismatch: got %v, want %v", parsedRule.Name, expectedRule.Name)
					}
					if parsedRule.Priority != expectedRule.Priority {
						t.Errorf("Priority mismatch: got %v, want %v", parsedRule.Priority, expectedRule.Priority)
					}
					// Compare Conditions
					if !reflect.DeepEqual(parsedRule.Conditions, expectedRule.Conditions) {
						t.Errorf("Conditions mismatch: got %+v, want %+v", parsedRule.Conditions, expectedRule.Conditions)
					}
					// Compare Event
					if parsedRule.Event.EventType != expectedRule.Event.EventType {
						t.Errorf("EventType mismatch: got %v, want %v", parsedRule.Event.EventType, expectedRule.Event.EventType)
					}
					if !reflect.DeepEqual(parsedRule.Event.CustomProperty, expectedRule.Event.CustomProperty) {
						t.Errorf("CustomProperty mismatch: got %+v, want %+v", parsedRule.Event.CustomProperty, expectedRule.Event.CustomProperty)
					}
					if !reflect.DeepEqual(parsedRule.Event.Facts, expectedRule.Event.Facts) {
						t.Errorf("Facts mismatch: got %+v, want %+v", parsedRule.Event.Facts, expectedRule.Event.Facts)
					}
					if !reflect.DeepEqual(parsedRule.Event.Values, expectedRule.Event.Values) {
						t.Errorf("Values mismatch: got %+v, want %+v", parsedRule.Event.Values, expectedRule.Event.Values)
					}

					// Compare Conditions.All length
					if len(parsedRule.Conditions.All) != len(expectedRule.Conditions.All) {
						t.Errorf("Conditions.All length mismatch: got %d, want %d", len(parsedRule.Conditions.All), len(expectedRule.Conditions.All))
					} else {
						// Compare each condition in Conditions.All
						for i, condition := range parsedRule.Conditions.All {
							expectedCondition := expectedRule.Conditions.All[i]
							if condition.Fact != expectedCondition.Fact {
								t.Errorf("Condition %d Fact mismatch: got %v, want %v", i, condition.Fact, expectedCondition.Fact)
							}
							if condition.Operator != expectedCondition.Operator {
								t.Errorf("Condition %d Operator mismatch: got %v, want %v", i, condition.Operator, expectedCondition.Operator)
							}
							if !reflect.DeepEqual(condition.Value, expectedCondition.Value) {
								t.Errorf("Condition %d Value mismatch: got %+v, want %+v", i, condition.Value, expectedCondition.Value)
							}
						}
					}

					// Compare Actions length
					if len(parsedRule.Event.Actions) != len(expectedRule.Event.Actions) {
						t.Errorf("Actions length mismatch: got %d, want %d", len(parsedRule.Event.Actions), len(expectedRule.Event.Actions))
					} else {
						// Compare each action
						for i, action := range parsedRule.Event.Actions {
							expectedAction := expectedRule.Event.Actions[i]
							if action.Type != expectedAction.Type {
								t.Errorf("Action %d Type mismatch: got %v, want %v", i, action.Type, expectedAction.Type)
							}
							if action.Target != expectedAction.Target {
								t.Errorf("Action %d Target mismatch: got %v, want %v", i, action.Target, expectedAction.Target)
							}
							if !reflect.DeepEqual(action.Value, expectedAction.Value) {
								t.Errorf("Action %d Value mismatch: got %+v, want %+v", i, action.Value, expectedAction.Value)
							}
						}
					}
				}
			}
		})
	}
}
