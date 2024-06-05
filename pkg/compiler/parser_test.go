package compiler

import (
	"encoding/json"
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

func TestInvalidJSON(t *testing.T) {
	jsonData := []byte(`{`)
	_, err := Parse(jsonData)
	assert.Error(t, err)
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
