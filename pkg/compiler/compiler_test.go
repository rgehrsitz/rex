// rex/pkg/compiler/compiler_test.go

package compiler

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseJSON(t *testing.T) {
	jsonData := []byte(`
		{
			"rules": [
				{
					"name": "rule-1",
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
		}
	`)

	var ruleset Ruleset
	err := json.Unmarshal(jsonData, &ruleset)
	assert.NoError(t, err)
	assert.Len(t, ruleset.Rules, 1)
	assert.Equal(t, "rule-1", ruleset.Rules[0].Name)
}

func TestParse(t *testing.T) {
	jsonData := []byte(`
        {
            "rules": [
                {
                    "name": "rule1",
                    "conditions": {
                        "all": [
                            {
                                "fact": "fact1",
                                "operator": "EQ",
                                "value": "value1"
                            }
                        ]
                    },
                    "actions": [
                        {
                            "type": "updateStore",
                            "target": "fact2",
                            "value": true
                        }
                    ]
                }
            ]
        }
    `)

	ruleset, err := Parse(jsonData)
	assert.NoError(t, err)
	assert.NotNil(t, ruleset)
	assert.Equal(t, 1, len(ruleset.Rules))
}

func TestGenerateBytecode(t *testing.T) {
	ruleset := &Ruleset{
		Rules: []Rule{
			{
				Name: "rule1",
				Conditions: ConditionGroup{
					All: []*ConditionOrGroup{
						{
							Fact:     "fact1",
							Operator: "EQ",
							Value:    "value1",
						},
					},
				},
				Actions: []Action{
					{
						Type:   "updateStore",
						Target: "fact2",
						Value:  true,
					},
				},
			},
		},
	}

	bytecode := GenerateBytecode(ruleset)
	assert.NotEmpty(t, bytecode)
}

func TestWriteBytecodeToFile(t *testing.T) {
	bytecodeFile := BytecodeFile{
		Header: Header{
			Version:       1,
			Checksum:      0,
			ConstPoolSize: 0,
			NumRules:      1,
		},
		Instructions: []byte{
			byte(HEADER_START),
			byte(VERSION), 1, 0,
			byte(CHECKSUM), 0, 0, 0, 0,
			byte(CONST_POOL_SIZE), 0, 0,
			byte(NUM_RULES), 1,
			byte(HEADER_END),
			// Rule instructions...
		},
		RuleExecIndex: []RuleExecutionIndex{
			{
				RuleName:   "rule1",
				ByteOffset: 0,
			},
		},
		FactRuleLookupIndex: map[string][]string{
			"fact1": {"rule1"},
		},
		FactDependencyIndex: []FactDependencyIndex{
			{
				RuleName: "rule1",
				Facts:    []string{"fact1"},
			},
		},
	}

	err := WriteBytecodeToFile("test_bytecode.bin", bytecodeFile)
	assert.NoError(t, err)
}
