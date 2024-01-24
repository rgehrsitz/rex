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
			ruleData: `{
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
                "action": {
                    "type": "updateStore",
                    "target": "roomStatus",
                    "value": "hot"
                }
            }
        }`,
			expectErr: false,
			expected: rule.Rule{
				Name:     "TestTemperatureRule",
				Priority: 1,
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
					EventType: "alert",
					Action: rule.Action{
						Type:   "updateStore",
						Target: "roomStatus",
						Value:  "hot",
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

			// Test readAndParseRules
			rules, err := readAndParseRules(tempFilePath)
			if tc.expectErr {
				if err == nil {
					t.Errorf("Expected an error, but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %s", err)
				}
				if len(rules) != 1 || !reflect.DeepEqual(rules[0], tc.expected) {
					t.Errorf("Parsed rule does not match expected: got %+v, want %+v", rules[0], tc.expected)
				}
			}
		})
	}
}
