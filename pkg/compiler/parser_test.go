// rex/pkg/compiler/parser_test.go

package compiler

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestManualParse(t *testing.T) {
	jsonData := `{"all":[{"fact":"temperature","operator":"GT","value":30.0},{"any":[{"fact":"humidity","operator":"LT","value":50},{"fact":"pressure","operator":"GT","value":1000}]}]}`
	var cg ConditionGroup
	err := json.Unmarshal([]byte(jsonData), &cg)
	assert.NoError(t, err)
	assert.NotNil(t, cg.All)
	assert.Len(t, cg.All, 2) // Make sure two groups are parsed
}

// TestSingleCondition verifies parsing of a simple single condition
func TestSingleCondition(t *testing.T) {
	jsonData := `{
		"all": [
			{"fact": "temperature", "operator": "GT", "value": 30}
		]
	}`
	var cg ConditionGroup
	err := json.Unmarshal([]byte(jsonData), &cg)
	assert.NoError(t, err)
	assert.Len(t, cg.All, 1)
	assert.Nil(t, cg.Any)
	assert.Equal(t, "temperature", cg.All[0].Fact)
	assert.Equal(t, "GT", cg.All[0].Operator)
	assert.Equal(t, 30.0, cg.All[0].Value.(float64))
}

// TestNestedGroup validates nested groups within All and Any
func TestNestedGroup(t *testing.T) {
	jsonData := `{
		"all": [
			{"fact": "temperature", "operator": "GT", "value": 30},
			{"any": [
				{"fact": "humidity", "operator": "LT", "value": 50},
				{"fact": "pressure", "operator": "GT", "value": 1000}
			]}
		]
	}`
	var cg ConditionGroup
	err := json.Unmarshal([]byte(jsonData), &cg)
	assert.NoError(t, err)
	assert.Len(t, cg.All, 2)
	assert.Equal(t, "humidity", cg.All[1].Any[0].Fact)
	assert.Equal(t, "LT", cg.All[1].Any[0].Operator)
	assert.Equal(t, 50.0, cg.All[1].Any[0].Value.(float64))
	assert.Equal(t, "pressure", cg.All[1].Any[1].Fact)
	assert.Equal(t, "GT", cg.All[1].Any[1].Operator)
	assert.Equal(t, 1000.0, cg.All[1].Any[1].Value.(float64))
}

// TestComplexNestedGroup tests more complex nesting and combinations of All and Any
func TestComplexNestedGroup(t *testing.T) {
	jsonData := `{
		"all": [
			{"any": [
				{"fact": "humidity", "operator": "LT", "value": 50},
				{"all": [
					{"fact": "pressure", "operator": "GT", "value": 1000},
					{"fact": "temperature", "operator": "GT", "value": 30}
				]}
			]}
		]
	}`
	var cg ConditionGroup
	err := json.Unmarshal([]byte(jsonData), &cg)
	assert.NoError(t, err)
	assert.Len(t, cg.All, 1)
	assert.Len(t, cg.All[0].Any, 2)
	assert.Equal(t, "pressure", cg.All[0].Any[1].All[0].Fact)
	assert.Equal(t, "GT", cg.All[0].Any[1].All[0].Operator)
	assert.Equal(t, 1000.0, cg.All[0].Any[1].All[0].Value.(float64))
	assert.Equal(t, "temperature", cg.All[0].Any[1].All[1].Fact)
	assert.Equal(t, "GT", cg.All[0].Any[1].All[1].Operator)
	assert.Equal(t, 30.0, cg.All[0].Any[1].All[1].Value.(float64))
}

func TestParser(t *testing.T) {
	// Test case 1: Valid JSON input with single condition
	jsonData := []byte(`{
        "rules": [
            {
                "name": "rule1",
                "priority": 10,
                "conditions": {
                    "all": [
                        {
                            "fact": "temperature",
                            "operator": "GT",
                            "value": 30.0
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
    }`)

	ruleset, err := Parse(jsonData)
	assert.NoError(t, err)
	assert.NotNil(t, ruleset)
	assert.Len(t, ruleset.Rules, 1)
	assert.Equal(t, "rule1", ruleset.Rules[0].Name)
	assert.Equal(t, 10, ruleset.Rules[0].Priority)
	assert.Len(t, ruleset.Rules[0].Conditions.All, 1)
	assert.Equal(t, "temperature", ruleset.Rules[0].Conditions.All[0].Fact)
	assert.Equal(t, "GT", ruleset.Rules[0].Conditions.All[0].Operator)
	assert.Equal(t, 30.0, ruleset.Rules[0].Conditions.All[0].Value)
	assert.Len(t, ruleset.Rules[0].Actions, 1)
	assert.Equal(t, "updateStore", ruleset.Rules[0].Actions[0].Type)
	assert.Equal(t, "temperature_status", ruleset.Rules[0].Actions[0].Target)
	assert.Equal(t, true, ruleset.Rules[0].Actions[0].Value)

	// Test case 2: Valid JSON input with nested conditions
	jsonData = []byte(`{
        "rules": [
            {
                "name": "rule2",
                "priority": 20,
                "conditions": {
                    "all": [
                        {
                            "fact": "temperature",
                            "operator": "GT",
                            "value": 30.0
                        },
                        {
                            "any": [
                                {
                                    "fact": "humidity",
                                    "operator": "LT",
                                    "value": 50
                                },
                                {
                                    "fact": "pressure",
                                    "operator": "GT",
                                    "value": 1000
                                }
                            ]
                        }
                    ]
                },
                "actions": [
                    {
                        "type": "sendMessage",
                        "target": "alert_service",
                        "value": "High temperature and low humidity or high pressure detected"
                    }
                ]
            }
        ]
    }`)

	ruleset, err = Parse(jsonData)
	assert.NoError(t, err)
	assert.NotNil(t, ruleset)
	assert.Len(t, ruleset.Rules, 1)
	assert.Equal(t, "rule2", ruleset.Rules[0].Name)
	assert.Equal(t, 20, ruleset.Rules[0].Priority)
	assert.Len(t, ruleset.Rules[0].Conditions.All, 2)
	assert.Equal(t, "temperature", ruleset.Rules[0].Conditions.All[0].Fact)
	assert.Equal(t, "GT", ruleset.Rules[0].Conditions.All[0].Operator)
	assert.Equal(t, 30.0, ruleset.Rules[0].Conditions.All[0].Value)
	assert.Len(t, ruleset.Rules[0].Conditions.All[1].Any, 2)
	assert.Equal(t, "humidity", ruleset.Rules[0].Conditions.All[1].Any[0].Fact)
	assert.Equal(t, "LT", ruleset.Rules[0].Conditions.All[1].Any[0].Operator)
	assert.Equal(t, 50.0, ruleset.Rules[0].Conditions.All[1].Any[0].Value)
	assert.Equal(t, "pressure", ruleset.Rules[0].Conditions.All[1].Any[1].Fact)
	assert.Equal(t, "GT", ruleset.Rules[0].Conditions.All[1].Any[1].Operator)
	assert.Equal(t, 1000.0, ruleset.Rules[0].Conditions.All[1].Any[1].Value)
	assert.Len(t, ruleset.Rules[0].Actions, 1)
	assert.Equal(t, "sendMessage", ruleset.Rules[0].Actions[0].Type)
	assert.Equal(t, "alert_service", ruleset.Rules[0].Actions[0].Target)
	assert.Equal(t, "High temperature and low humidity or high pressure detected", ruleset.Rules[0].Actions[0].Value)
}

func TestParseInvalidJSON(t *testing.T) {
	invalidJSON := []byte(`{"rules": [{"name": "invalid_rule",}]}`)
	_, err := Parse(invalidJSON)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to unmarshal JSON data")
}

func TestParseInvalidRuleStructure(t *testing.T) {
	invalidRule := []byte(`{
        "rules": [{
            "name": "invalid_rule",
            "conditions": {
                "invalid": [{"fact": "temperature", "operator": "GT", "value": 30}]
            },
            "actions": [{"type": "updateStore", "target": "status", "value": true}]
        }]
    }`)
	_, err := Parse(invalidRule)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid rule")
}

func TestParseNestedConditions(t *testing.T) {
	nestedConditions := []byte(`{
        "rules": [{
            "name": "nested_rule",
            "conditions": {
                "all": [
                    {"fact": "temperature", "operator": "GT", "value": 30},
                    {"any": [
                        {"fact": "humidity", "operator": "LT", "value": 50},
                        {"fact": "pressure", "operator": "GT", "value": 1000}
                    ]}
                ]
            },
            "actions": [{"type": "updateStore", "target": "status", "value": true}]
        }]
    }`)
	ruleset, err := Parse(nestedConditions)
	assert.NoError(t, err)
	assert.Len(t, ruleset.Rules, 1)
	assert.Len(t, ruleset.Rules[0].Conditions.All, 2)
	assert.Len(t, ruleset.Rules[0].Conditions.All[1].Any, 2)
}

func TestMissingRules(t *testing.T) {
	jsonData := []byte(`{}`)
	_, err := Parse(jsonData)
	assert.Error(t, err)
}

func TestInvalidRuleName(t *testing.T) {
	jsonData := []byte(`{
        "rules": [
            {
                "name": "",
                "priority": 10,
                "conditions": {
                    "all": [
                        {
                            "fact": "temperature",
                            "operator": "GT",
                            "value": 30.0
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
    }`)
	_, err := Parse(jsonData)
	assert.Error(t, err)
}

func TestInvalidPriority(t *testing.T) {
	jsonData := []byte(`{
        "rules": [
            {
                "name": "rule1",
                "priority": -1,
                "conditions": {
                    "all": [
                        {
                            "fact": "temperature",
                            "operator": "GT",
                            "value": 30.0
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
    }`)
	_, err := Parse(jsonData)
	assert.Error(t, err)
}

func TestInvalidConditionFact(t *testing.T) {
	jsonData := []byte(`{
        "rules": [
            {
                "name": "rule1",
                "priority": 10,
                "conditions": {
                    "all": [
                        {
                            "fact": "",
                            "operator": "GT",
                            "value": 30.0
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
    }`)
	_, err := Parse(jsonData)
	assert.Error(t, err)
}

func TestInvalidConditionOperator(t *testing.T) {
	jsonData := []byte(`{
        "rules": [
            {
                "name": "rule1",
                "priority": 10,
                "conditions": {
                    "all": [
                        {
                            "fact": "temperature",
                            "operator": "",
                            "value": 30.0
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
    }`)
	_, err := Parse(jsonData)
	assert.Error(t, err)
}

func TestEmptyConditionValue(t *testing.T) {
	jsonData := []byte(`{
        "rules": [
            {
                "name": "rule1",
                "priority": 10,
                "conditions": {
                    "all": [
                        {
                            "fact": "temperature",
                            "operator": "GT",
                            "value": ""
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
    }`)
	_, err := Parse(jsonData)
	assert.Error(t, err)
}

func TestInvalidActionType(t *testing.T) {
	jsonData := []byte(`{
        "rules": [
            {
                "name": "rule1",
                "priority": 10,
                "conditions": {
                    "all": [
                        {
                            "fact": "temperature",
                            "operator": "GT",
                            "value": 30.0
                        }
                    ]
                },
                "actions": [
                    {
                        "type": "",
                        "target": "temperature_status",
                        "value": true
                    }
                ]
            }
        ]
    }`)
	_, err := Parse(jsonData)
	assert.Error(t, err)
}

func TestInvalidActionTarget(t *testing.T) {
	jsonData := []byte(`{
        "rules": [
            {
                "name": "rule1",
                "priority": 10,
                "conditions": {
                    "all": [
                        {
                            "fact": "temperature",
                            "operator": "GT",
                            "value": 30.0
                        }
                    ]
                },
                "actions": [
                    {
                        "type": "updateStore",
                        "target": "",
                        "value": true
                    }
                ]
            }
        ]
    }`)
	_, err := Parse(jsonData)
	assert.Error(t, err)
}

func TestAllOperators(t *testing.T) {
	operators := []string{"EQ", "NEQ", "LT", "LTE", "GT", "GTE"}
	for _, op := range operators {
		jsonData := []byte(fmt.Sprintf(`{
            "rules": [{
                "name": "test-%s",
                "conditions": {
                    "all": [{
                        "fact": "test",
                        "operator": "%s",
                        "value": 10
                    }]
                },
                "actions": [{
                    "type": "updateStore",
                    "target": "result",
                    "value": true
                }]
            }]
        }`, op, op))

		ruleset, err := Parse(jsonData)
		assert.NoError(t, err)
		assert.Len(t, ruleset.Rules, 1)
		assert.Equal(t, op, ruleset.Rules[0].Conditions.All[0].Operator)
	}
}

func TestComplexNestedConditions(t *testing.T) {
	jsonData := []byte(`{
        "rules": [{
            "name": "complex-nested",
            "conditions": {
                "all": [{
                    "any": [{
                        "all": [{
                            "fact": "a",
                            "operator": "EQ",
                            "value": 1
                        }, {
                            "fact": "b",
                            "operator": "GT",
                            "value": 2
                        }]
                    }, {
                        "any": [{
                            "fact": "c",
                            "operator": "LT",
                            "value": 3
                        }, {
                            "fact": "d",
                            "operator": "CONTAINS",
                            "value": "test"
                        }]
                    }]
                }]
            },
            "actions": [{
                "type": "updateStore",
                "target": "result",
                "value": true
            }]
        }]
    }`)

	ruleset, err := Parse(jsonData)
	assert.NoError(t, err)
	assert.Len(t, ruleset.Rules, 1)
	// Add more specific assertions to verify the nested structure
}

func TestMultipleRules(t *testing.T) {
	jsonData := []byte(`{
        "rules": [{
            "name": "rule1",
            "priority": 1,
            "conditions": {
                "all": [{
                    "fact": "a",
                    "operator": "EQ",
                    "value": 1
                }]
            },
            "actions": [{
                "type": "updateStore",
                "target": "result1",
                "value": true
            }]
        }, {
            "name": "rule2",
            "priority": 2,
            "conditions": {
                "all": [{
                    "fact": "b",
                    "operator": "GT",
                    "value": 2
                }]
            },
            "actions": [{
                "type": "updateStore",
                "target": "result2",
                "value": true
            }]
        }]
    }`)

	ruleset, err := Parse(jsonData)
	assert.NoError(t, err)
	assert.Len(t, ruleset.Rules, 2)
	assert.Equal(t, "rule1", ruleset.Rules[0].Name)
	assert.Equal(t, "rule2", ruleset.Rules[1].Name)
	assert.Equal(t, 1, ruleset.Rules[0].Priority)
	assert.Equal(t, 2, ruleset.Rules[1].Priority)
}

func BenchmarkParseLargeRuleset(b *testing.B) {
	// Generate a large ruleset JSON
	var rules []string
	for i := 0; i < 1000; i++ {
		rule := fmt.Sprintf(`{
            "name": "rule%d",
            "priority": %d,
            "conditions": {
                "all": [{
                    "fact": "fact%d",
                    "operator": "EQ",
                    "value": %d
                }]
            },
            "actions": [{
                "type": "updateStore",
                "target": "result%d",
                "value": true
            }]
        }`, i, i, i, i, i)
		rules = append(rules, rule)
	}
	jsonData := []byte(fmt.Sprintf(`{"rules": [%s]}`, strings.Join(rules, ",")))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Parse(jsonData)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestValidateRule(t *testing.T) {
	tests := []struct {
		name        string
		rule        Rule
		expectedErr string
	}{
		{
			name: "Valid Rule",
			rule: Rule{
				Name:     "ValidRule",
				Priority: 1,
				Conditions: ConditionGroup{
					All: []*ConditionOrGroup{{Fact: "temperature", Operator: "GT", Value: 30}},
				},
				Actions: []Action{{Type: "updateStore", Target: "alarm", Value: true}},
			},
			expectedErr: "",
		},
		{
			name: "Missing Name",
			rule: Rule{
				Priority: 1,
				Conditions: ConditionGroup{
					All: []*ConditionOrGroup{{Fact: "temperature", Operator: "GT", Value: 30}},
				},
				Actions: []Action{{Type: "updateStore", Target: "alarm", Value: true}},
			},
			expectedErr: "COMPILE: Rule name is required",
		},
		{
			name: "Negative Priority",
			rule: Rule{
				Name:     "NegativePriority",
				Priority: -1,
				Conditions: ConditionGroup{
					All: []*ConditionOrGroup{{Fact: "temperature", Operator: "GT", Value: 30}},
				},
				Actions: []Action{{Type: "updateStore", Target: "alarm", Value: true}},
			},
			expectedErr: "COMPILE: Rule priority must be non-negative",
		},
		{
			name: "Empty Conditions",
			rule: Rule{
				Name:       "EmptyConditions",
				Priority:   1,
				Conditions: ConditionGroup{},
				Actions:    []Action{{Type: "updateStore", Target: "alarm", Value: true}},
			},
			expectedErr: "COMPILE: Invalid condition group",
		},
		{
			name: "No Actions",
			rule: Rule{
				Name:     "NoActions",
				Priority: 1,
				Conditions: ConditionGroup{
					All: []*ConditionOrGroup{{Fact: "temperature", Operator: "GT", Value: 30}},
				},
				Actions: []Action{},
			},
			expectedErr: "COMPILE: At least one action is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRule(&tt.rule)
			if tt.expectedErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.expectedErr)
			}
		})
	}
}

func TestValidateConditionOrGroup(t *testing.T) {
	tests := []struct {
		name        string
		cog         *ConditionOrGroup
		expectedErr string
	}{
		{
			name:        "Valid Condition",
			cog:         &ConditionOrGroup{Fact: "temperature", Operator: "GT", Value: 30},
			expectedErr: "",
		},
		{
			name:        "Missing Fact",
			cog:         &ConditionOrGroup{Operator: "GT", Value: 30},
			expectedErr: "COMPILE: Empty or missing fact field",
		},
		{
			name:        "Invalid Operator",
			cog:         &ConditionOrGroup{Fact: "temperature", Operator: "INVALID", Value: 30},
			expectedErr: "COMPILE: Invalid condition operator",
		},
		{
			name:        "Missing Value",
			cog:         &ConditionOrGroup{Fact: "temperature", Operator: "GT"},
			expectedErr: "COMPILE: Invalid condition value for operator",
		},
		{
			name:        "Invalid Value Type",
			cog:         &ConditionOrGroup{Fact: "temperature", Operator: "GT", Value: "not a number"},
			expectedErr: "COMPILE: Invalid condition value for operator",
		},
		{
			name: "Valid Nested All",
			cog: &ConditionOrGroup{
				All: []*ConditionOrGroup{
					{Fact: "temperature", Operator: "GT", Value: 30},
					{Fact: "humidity", Operator: "LT", Value: 50},
				},
			},
			expectedErr: "",
		},
		{
			name: "Valid Nested Any",
			cog: &ConditionOrGroup{
				Any: []*ConditionOrGroup{
					{Fact: "temperature", Operator: "GT", Value: 30},
					{Fact: "humidity", Operator: "LT", Value: 50},
				},
			},
			expectedErr: "",
		},
		{
			name:        "Nil Condition",
			cog:         nil,
			expectedErr: "COMPILE: Nil condition or group received",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConditionOrGroup(tt.cog)
			if tt.expectedErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.expectedErr)
			}
		})
	}
}

func TestValidateAction(t *testing.T) {
	tests := []struct {
		name           string
		action         *Action
		expectedErrMsg string
	}{
		{
			name:           "Valid Action",
			action:         &Action{Type: "updateStore", Target: "alarm", Value: true},
			expectedErrMsg: "",
		},
		{
			name:           "Nil Action",
			action:         nil,
			expectedErrMsg: "Nil action received",
		},
		{
			name:           "Missing Type",
			action:         &Action{Target: "alarm", Value: true},
			expectedErrMsg: "Empty or missing type field",
		},
		{
			name:           "Missing Target",
			action:         &Action{Type: "updateStore", Value: true},
			expectedErrMsg: "Empty or missing target field",
		},
		{
			name:           "Invalid Value Type",
			action:         &Action{Type: "updateStore", Target: "alarm", Value: make(chan int)},
			expectedErrMsg: "Invalid action value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAction(tt.action)
			if tt.expectedErrMsg == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrMsg)
			}
		})
	}
}

func TestIsFactValid(t *testing.T) {
	tests := []struct {
		fact     string
		expected bool
	}{
		{"temperature", true},
		{"humidity", true},
		{"", false},
		{"invalid fact", true}, // Note: This function always returns true in the current implementation
	}

	for _, tt := range tests {
		t.Run(tt.fact, func(t *testing.T) {
			result := isFactValid(tt.fact)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsOperatorValid(t *testing.T) {
	tests := []struct {
		operator string
		expected bool
	}{
		{"EQ", true},
		{"NEQ", true},
		{"LT", true},
		{"LTE", true},
		{"GT", true},
		{"GTE", true},
		{"CONTAINS", true},
		{"NOT_CONTAINS", true},
		{"INVALID", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.operator, func(t *testing.T) {
			result := isOperatorValid(tt.operator)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsActionValueValid(t *testing.T) {
	tests := []struct {
		name       string
		actionType string
		value      interface{}
		expected   bool
	}{
		{"Valid Float", "updateStore", 3.14, true},
		{"Valid Integer", "updateStore", 42, true},
		{"Valid String", "updateStore", "test", true},
		{"Valid Boolean", "updateStore", true, true},
		{"Invalid Type", "updateStore", make(chan int), false},
		{"Invalid Action Type", "invalidType", "test", false},
		{"Valid sendMessage", "sendMessage", "test message", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isActionValueValid(tt.actionType, tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}
