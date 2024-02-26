package compiler

import (
	"rgehrsitz/rex/internal/bytecode"
	"rgehrsitz/rex/internal/rule"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTranslateRuleToBytecode(t *testing.T) {
	r := rule.Rule{
		Name: "TestRule",
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{Fact: "temperature", Operator: "greaterThan", Value: 25},
			},
		},
		Event: rule.Event{
			Actions: []rule.Action{
				{Type: "updateStore", Target: "status", Value: "hot"},
			},
		},
	}

	// Adjusting expected bytecode sequence to use OpJumpIfFalse.
	expected := []bytecode.Instruction{
		// Load the "temperature" fact.
		{
			Opcode: bytecode.OpLoadFact,
			Operands: []bytecode.Operand{
				bytecode.FactOperand{FactName: "temperature"},
			},
		},
		// Compare it to 25.
		{
			Opcode: bytecode.OpGreaterThan,
			Operands: []bytecode.Operand{
				bytecode.ValueOperand{Value: 25},
			},
		},
		// Jump over the action if the condition is false.
		{
			Opcode: bytecode.OpJumpIfFalse,
			Operands: []bytecode.Operand{
				// This is simplified. Actual implementation needs to calculate the correct offset.
				bytecode.JumpOffsetOperand{Offset: 1}, // Assuming the next instruction is the action if condition is true.
			},
		},
		// Update store action, executed if the condition is true.
		{
			Opcode: bytecode.OpUpdateStore,
			Operands: []bytecode.Operand{
				bytecode.ValueOperand{Value: "status"},
				bytecode.ValueOperand{Value: "hot"},
			},
		},
	}

	instructions, err := TranslateRuleToBytecode(r)
	assert.NoError(t, err, "TranslateRuleToBytecode should not return an error")
	assert.Equal(t, expected, instructions, "TranslateRuleToBytecode did not produce the expected bytecode sequence")
}

func TestAnalyzeRuleDependencies(t *testing.T) {
	// Setup rules with known dependencies
	rules := []rule.Rule{
		// Define some rules that have dependencies
	}

	// Expected dependency graph representation
	expectedGraph := DependencyGraph{
		Edges: map[string][]string{
			// Define expected dependencies
		},
	}

	AnalyzeRuleDependencies(rules)
	actualGraph := BuildDependencyGraph(rules)

	assert.Equal(t, expectedGraph, actualGraph, "The generated dependency graph does not match the expected graph")
}

func TestCompileRuleSet(t *testing.T) {
	rules := []rule.Rule{
		// Define a set of rules for testing
	}

	// Expected bytecode sequence after compilation
	expected := []bytecode.Instruction{
		// Define the expected bytecode sequence
	}

	program, err := CompileRuleSet(rules)

	assert.NoError(t, err, "CompileRuleSet should not return an error")
	assert.Equal(t, expected, program, "CompileRuleSet did not produce the expected bytecode sequence")
}

func TestTranslateRuleToBytecodeWithMixedConditions(t *testing.T) {
	r := rule.Rule{
		Name: "ComplexRuleWithLogicalGrouping",
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{
					Fact:     "temperature",
					Operator: "greaterThan",
					Value:    20,
				},
				{
					Any: []rule.Condition{
						{
							Fact:     "humidity",
							Operator: "lessThan",
							Value:    50,
						},
						{
							Fact:     "light",
							Operator: "greaterThan",
							Value:    300,
						},
					},
				},
			},
		},
		Event: rule.Event{
			Actions: []rule.Action{
				{
					Type:   "updateStore",
					Target: "systemStatus",
					Value:  "active",
				},
			},
		},
	}

	expected := []bytecode.Instruction{
		// Temperature check (>20)
		{Opcode: bytecode.OpLoadFact, Operands: []bytecode.Operand{bytecode.FactOperand{FactName: "temperature"}}},
		{Opcode: bytecode.OpGreaterThan, Operands: []bytecode.Operand{bytecode.ValueOperand{Value: 20}}},
		{Opcode: bytecode.OpJumpIfFalse, Operands: []bytecode.Operand{bytecode.JumpOffsetOperand{Offset: 7}}}, // Jump over all to end

		// Humidity check (<50)
		{Opcode: bytecode.OpLoadFact, Operands: []bytecode.Operand{bytecode.FactOperand{FactName: "humidity"}}},
		{Opcode: bytecode.OpLessThan, Operands: []bytecode.Operand{bytecode.ValueOperand{Value: 50}}},
		{Opcode: bytecode.OpJumpIfTrue, Operands: []bytecode.Operand{bytecode.JumpOffsetOperand{Offset: 3}}}, // Jump to action

		// Light check (>300)
		{Opcode: bytecode.OpLoadFact, Operands: []bytecode.Operand{bytecode.FactOperand{FactName: "light"}}},
		{Opcode: bytecode.OpGreaterThan, Operands: []bytecode.Operand{bytecode.ValueOperand{Value: 300}}},
		{Opcode: bytecode.OpJumpIfFalse, Operands: []bytecode.Operand{bytecode.JumpOffsetOperand{Offset: 1}}}, // Skip action

		// Action
		{Opcode: bytecode.OpUpdateStore, Operands: []bytecode.Operand{bytecode.ValueOperand{Value: "systemStatus"}, bytecode.ValueOperand{Value: "active"}}},
	}

	instructions, err := TranslateRuleToBytecode(r)
	assert.NoError(t, err, "TranslateRuleToBytecode should not return an error")
	assert.Equal(t, expected, instructions, "TranslateRuleToBytecode did not produce the expected bytecode sequence")
}
