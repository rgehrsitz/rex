package compiler

import (
	"rgehrsitz/rex/internal/bytecode"
	"rgehrsitz/rex/internal/rule"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompileRuleSet(t *testing.T) {
	// Define test rules
	testRules := []rule.Rule{
		{
			Name: "TestRule1",
			Conditions: rule.Conditions{
				All: []rule.Condition{
					{Fact: "X", Operator: ">", Value: 10},
					{Fact: "Y", Operator: ">", Value: 10},
				},
			},
			Event: rule.Event{
				Actions: []rule.Action{
					{Type: "updateStore", Target: "Z", Value: "faulted"},
				},
			},
		},
	}

	// Expected output (mock some expected bytecode instructions for illustration)
	expectedInstructions := []bytecode.Instruction{
		// Example instructions - adjust according to your actual instruction generation logic
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"X"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{10}},
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"Y"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{10}},
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{1 /* address to jump to, placeholder */}},
		{Opcode: bytecode.OpUpdateStore, Operands: []interface{}{"Z", "faulted"}},
	}

	expected := []CompiledRule{
		{
			Name:               "TestRule1",
			Instructions:       expectedInstructions,
			RuleDependencies:   []string{}, // Depends on the specifics of the rule
			SensorDependencies: []string{"X", "Y"},
		},
	}

	// Compile rule set
	result, err := CompileRuleSet(testRules)

	// Use assert to verify the results
	assert.NoError(t, err, "CompileRuleSet should not return an error")
	assert.Equal(t, expected, result, "The compiled rules do not match the expected output")
}
