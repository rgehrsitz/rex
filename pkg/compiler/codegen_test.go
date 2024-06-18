// rex/pkg/compiler/codegen_test.go

package compiler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoveLabels(t *testing.T) {
	instructions := []Instruction{
		{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 1")},
		{Opcode: LABEL, Operands: []byte("L0")},
		{Opcode: ACTION_START},
	}

	expectedInstructions := []Instruction{
		{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 1")},
		{Opcode: ACTION_START},
	}

	finalInstructions := RemoveLabels(instructions)
	assert.Equal(t, expectedInstructions, finalInstructions)
}

func TestGenerateBytecodeComplexConditions(t *testing.T) {
	ruleset := &Ruleset{
		Rules: []Rule{
			{
				Name: "ComplexRule",
				Conditions: ConditionGroup{
					All: []*ConditionOrGroup{
						{
							Fact:     "temperature",
							Operator: "GT",
							Value:    30.0,
						},
						{
							Any: []*ConditionOrGroup{
								{
									Fact:     "humidity",
									Operator: "LT",
									Value:    50,
								},
								{
									Fact:     "pressure",
									Operator: "GT",
									Value:    1000,
								},
							},
						},
					},
				},
				Actions: []Action{
					{
						Type:   "updateFact",
						Target: "alert",
						Value:  true,
					},
				},
			},
		},
	}

	bytecode := GenerateBytecode(ruleset)
	assert.NotEmpty(t, bytecode)
}
