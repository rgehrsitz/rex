package compiler

import (
	"reflect"
	"rgehrsitz/rex/internal/bytecode"
	"rgehrsitz/rex/internal/rule"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCompileSimpleRule tests compiling a rule with simple conditions.
func TestCompileSimpleRule(t *testing.T) {
	r := rule.Rule{
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{Fact: "temperature", Operator: "greaterThan", Value: 30},
			},
		},
	}

	expected := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{30}},
	}

	instructions, err := CompileRule(&r)
	if err != nil {
		t.Fatalf("CompileRule failed: %v", err)
	}

	if !reflect.DeepEqual(instructions, expected) {
		t.Errorf("Expected %v, got %v", expected, instructions)
	}
}

// TestCompileRuleWithAnyConditions tests compiling a rule with 'Any' conditions.
func TestCompileRuleWithAnyConditions(t *testing.T) {
	r := rule.Rule{
		Conditions: rule.Conditions{
			Any: []rule.Condition{
				{Fact: "humidity", Operator: "lessThan", Value: 50},
				{Fact: "windSpeed", Operator: "greaterThan", Value: 20},
			},
		},
	}

	// Expected bytecode includes jump instructions for 'Any' logic
	expected := []bytecode.Instruction{
		// First condition
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"humidity"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{50}},
		// Corrected jump instruction: should jump over the next condition if true
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{2}}, // Corrected to jump past just the next condition
		// Second condition
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"windSpeed"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{20}},
	}

	instructions, err := CompileRule(&r)
	assert.NoError(t, err, "CompileRule should not fail")

	assert.Equal(t, expected, instructions, "Compiled instructions do not match expected")
}

// TestCompileEmptyConditions tests compiling a rule with no conditions.
func TestCompileEmptyConditions(t *testing.T) {
	r := rule.Rule{
		Conditions: rule.Conditions{
			All: []rule.Condition{}, // No conditions specified
			Any: []rule.Condition{},
		},
	}

	// An empty slice for expected instructions
	var expected []bytecode.Instruction

	instructions, err := CompileRule(&r)
	if err != nil {
		t.Fatalf("CompileRule failed: %v", err)
	}

	if !isEmpty(instructions) || !isEmpty(expected) {
		t.Errorf("Expected an empty instruction set, got %v", instructions)
	}
}

// isEmpty checks if a slice of instructions is empty.
func isEmpty(instructions []bytecode.Instruction) bool {
	return len(instructions) == 0
}

// TestCompileInvalidCondition tests error handling for an invalid condition.
func TestCompileInvalidCondition(t *testing.T) {
	// Create a rule with an invalid operator
	r := rule.Rule{
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{Fact: "temperature", Operator: "invalidOperator", Value: 30},
			},
		},
	}

	_, err := CompileRule(&r)
	if err == nil {
		t.Errorf("Expected an error for invalid operator, but got none")
	}

	// Optionally, you can check for specific error messages if your implementation
	// returns descriptive errors for different types of invalid conditions.
	expectedErrMsg := "unsupported operator: invalidOperator"
	if err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}
}

// TestCompileNestedConditions tests compiling a rule with nested conditions.
func TestCompileNestedConditions(t *testing.T) {
	r := rule.Rule{
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{
					Fact:     "temperature",
					Operator: "greaterThan",
					Value:    30,
				},
				{
					Any: []rule.Condition{
						{
							Fact:     "humidity",
							Operator: "lessThan",
							Value:    50,
						},
						{
							Fact:     "windSpeed",
							Operator: "greaterThan",
							Value:    20,
						},
					},
				},
			},
		},
	}

	expected := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{30}},
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"humidity"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{50}},
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{2}}, // Corrected jump operand
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"windSpeed"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{20}},
	}

	instructions, err := CompileRule(&r)

	assert.NoError(t, err, "CompileRule should not fail")
	assert.Equal(t, expected, instructions, "Compiled instructions do not match expected")
}

// TestCompileComplexRule tests compiling a rule with a mix of 'All', 'Any', and nested conditions.
func TestCompileComplexRule(t *testing.T) {
	r := rule.Rule{
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{
					Fact:     "temperature",
					Operator: "greaterThan",
					Value:    30,
				},
				{
					Any: []rule.Condition{
						{
							All: []rule.Condition{
								{
									Fact:     "humidity",
									Operator: "lessThan",
									Value:    50,
								},
								{
									Fact:     "windSpeed",
									Operator: "greaterThan",
									Value:    20,
								},
							},
						},
						{
							All: []rule.Condition{
								{
									Fact:     "isRaining",
									Operator: "equal",
									Value:    true,
								},
								{
									Fact:     "dayOfWeek",
									Operator: "equal",
									Value:    "Saturday",
								},
							},
						},
					},
				},
			},
		},
	}

	// Expected bytecode should reflect the complex logic described above
	expected := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{30}},
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"humidity"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{50}},
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"windSpeed"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{20}},
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{4}}, // Corrected jump operand to 4
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"isRaining"}},
		{Opcode: bytecode.OpEqual, Operands: []interface{}{true}},
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"dayOfWeek"}},
		{Opcode: bytecode.OpEqual, Operands: []interface{}{"Saturday"}},
	}

	instructions, err := CompileRule(&r)
	assert.NoError(t, err, "CompileRule should not fail")
	assert.Equal(t, expected, instructions, "Compiled instructions do not match expected")
}

func TestCompileRuleWithEvents(t *testing.T) {
	r := rule.Rule{
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{Fact: "temperature", Operator: "greaterThan", Value: 30},
			},
		},
		Event: rule.Event{
			EventType:      "Alert",
			CustomProperty: "Temperature too high", // Note: This might not directly translate to an operand based on your CompileRule implementation.
		},
	}

	// Define expected bytecode, assuming specific opcodes for event handling
	expected := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{30}},
		// Adjust according to the actual implementation for event handling
		{Opcode: bytecode.OpTriggerEvent, Operands: []interface{}{"Alert", "Temperature too high"}},
	}

	instructions, err := CompileRule(&r)
	assert.NoError(t, err, "CompileRule should not fail")

	assert.Equal(t, expected, instructions, "Compiled instructions do not match expected")
}

// TestCompileRuleSet tests the compilation of rules with dependency analysis
func TestCompileRuleSet(t *testing.T) {
	// Define a set of sample rules
	rules := []rule.Rule{
		{
			Name: "Rule1",
			Conditions: rule.Conditions{
				All: []rule.Condition{
					{Fact: "temperature", Operator: "greaterThan", Value: 30},
				},
			},
			Event: rule.Event{
				EventType: "TemperatureHigh",
				Actions: []rule.Action{ // Now a slice of actions
					{
						Type:   "updateStore",
						Target: "alertLevel",
						Value:  "high",
					},
				},
			},
		},
		{
			Name: "Rule2",
			Conditions: rule.Conditions{
				All: []rule.Condition{
					{Fact: "alertLevel", Operator: "equal", Value: "high"},
				},
			},
			Event: rule.Event{
				EventType: "SendAlert",
				Actions: []rule.Action{ // Now a slice of actions
					{
						Type:   "sendMessage",
						Target: "192.168.0.1",
						Value:  "Alert level high",
					},
				},
			},
		},
		// Add more rules if needed for testing
	}

	expected := []CompiledRule{
		{
			Name: "Rule1", // Add the correct rule name
			Instructions: []bytecode.Instruction{
				{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
				{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{30}},
				{Opcode: bytecode.OpUpdateStore, Operands: []interface{}{"alertLevel", "high"}},
				{Opcode: bytecode.OpTriggerEvent, Operands: []interface{}{"TemperatureHigh", nil}},
			},
			Dependencies: nil, // Use nil to represent no dependencies, matching the actual output
		},
		{
			Name: "Rule2", // Add the correct rule name
			Instructions: []bytecode.Instruction{
				{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"alertLevel"}},
				{Opcode: bytecode.OpEqual, Operands: []interface{}{"high"}},
				{Opcode: bytecode.OpSendMessage, Operands: []interface{}{"192.168.0.1", "Alert level high"}},
				{Opcode: bytecode.OpTriggerEvent, Operands: []interface{}{"SendAlert", nil}},
			},
			Dependencies: []string{"Rule1"}, // Correctly reflect dependencies
		},
	}

	// Perform the compilation
	compiledRules, err := CompileRuleSet(rules)
	assert.NoError(t, err, "CompileRuleSet should not return an error")

	// Using assert.Equal to compare the compiledRules and expected output
	assert.Equal(t, expected, compiledRules, "CompileRuleSet output does not match expected output")

}

func TestCompileComplexNestedConditions(t *testing.T) {
	r := rule.Rule{
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{
					Any: []rule.Condition{
						{Fact: "humidity", Operator: "lessThan", Value: 50},
						{
							All: []rule.Condition{
								{Fact: "windSpeed", Operator: "greaterThan", Value: 20},
								{Fact: "temperature", Operator: "lessThan", Value: 25},
							},
						},
					},
				},
			},
		},
	}

	// Define the correct expected bytecode for the complex nested conditions
	expected := []bytecode.Instruction{
		// First 'Any' condition
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"humidity"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{50}},
		// Jump if "humidity" condition is true, skipping the nested 'All'
		// The jump should skip the next 4 instructions to go beyond the 'All' conditions
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{4}}, // This assumes 4 instructions to skip
		// Nested 'All' conditions
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"windSpeed"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{20}},
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{25}},
	}

	instructions, err := CompileRule(&r)
	assert.NoError(t, err, "CompileRule should not return an error")

	// Using assert.Equal to compare the instructions and expected output
	assert.Equal(t, expected, instructions, "Compiled instructions do not match expected output")
}

func TestCompileRuleWithMultipleActions(t *testing.T) {
	r := rule.Rule{
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{Fact: "temperature", Operator: "greaterThan", Value: 30},
			},
		},
		Event: rule.Event{
			EventType: "HighTemperature",
			Actions: []rule.Action{
				{
					Type:   "updateStore",
					Target: "alertLevel",
					Value:  "high",
				},
				{
					Type:   "sendMessage",
					Target: "192.168.0.101",
					Value:  "Temperature exceeded 30 degrees",
				},
			},
		},
	}

	expected := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{30}},
		{Opcode: bytecode.OpUpdateStore, Operands: []interface{}{"alertLevel", "high"}},
		{Opcode: bytecode.OpSendMessage, Operands: []interface{}{"192.168.0.101", "Temperature exceeded 30 degrees"}},
		{Opcode: bytecode.OpTriggerEvent, Operands: []interface{}{"HighTemperature", nil}},
	}

	instructions, err := CompileRule(&r)
	if err != nil {
		t.Fatalf("CompileRule failed: %v", err)
	}

	if !reflect.DeepEqual(instructions, expected) {
		t.Errorf("Expected %v, got %v", expected, instructions)
	}
}

func TestCompileRuleWithUnsupportedOperator(t *testing.T) {
	// Define a rule with an unsupported operator
	unsupportedRule := rule.Rule{
		Name: "TestRuleWithUnsupportedOperator",
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{
					Fact:     "temperature",
					Operator: "unsupportedOperator", // This operator is unsupported
					Value:    100,
				},
			},
		},
		Event: rule.Event{
			EventType: "alert",
		},
	}

	instructions, err := CompileRule(&unsupportedRule)

	// Check that no instructions were generated and an error was returned
	if len(instructions) != 0 {
		t.Errorf("Expected no instructions to be generated for an unsupported operator, got: %v", instructions)
	}

	if err == nil {
		t.Fatal("Expected an error to be returned for an unsupported operator, got nil")
	}

	if !containsSubstring(err.Error(), "unsupported operator") {
		t.Errorf("Expected error message to contain 'unsupported operator', got: %s", err.Error())
	}
}

// Helper function to check if a substring is present in a string
func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestCompileRuleWithMixedConditions(t *testing.T) {
	// Define a complex rule with mixed All and Any conditions
	complexRule := rule.Rule{
		Name: "ComplexRule",
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{
					Fact:     "temperature",
					Operator: "greaterThan",
					Value:    30,
				},
				{
					Any: []rule.Condition{
						{
							Fact:     "humidity",
							Operator: "lessThan",
							Value:    50,
						},
						{
							All: []rule.Condition{
								{
									Fact:     "windSpeed",
									Operator: "greaterThan",
									Value:    10,
								},
								{
									Fact:     "rain",
									Operator: "equal",
									Value:    true,
								},
							},
						},
					},
				},
			},
		},
		Event: rule.Event{
			EventType: "Alert",
			Actions: []rule.Action{
				{
					Type:   "sendMessage",
					Target: "192.168.1.1:1234",
					Value:  "Conditions are extreme",
				},
			},
		},
	}

	compiledInstructions, err := CompileRule(&complexRule)

	if err != nil {
		t.Errorf("Failed to compile rule: %v", err)
	}

	// Add assertions to check if the compiled instructions meet the expected criteria
	// This could include checking the length of the compiled instructions,
	// specific opcodes, or other relevant attributes
	if len(compiledInstructions) == 0 {
		t.Errorf("Compiled instructions are empty")
	}

	// Example assertion: check if the first opcode is OpLoadFact for temperature
	if compiledInstructions[0].Opcode != bytecode.OpLoadFact || compiledInstructions[0].Operands[0] != "temperature" {
		t.Errorf("Expected first instruction to load fact 'temperature', got: %v", compiledInstructions[0])
	}

	// Additional assertions can be added as needed to validate the compilation
}

// TestCompileRuleWithActionsHavingInvalidTargets tests the compiler's handling of actions with invalid targets.
func TestCompileRuleWithActionsHavingInvalidTargets(t *testing.T) {
	// Define a rule with an action that has an invalid target
	testRule := rule.Rule{
		Name: "TestRuleWithInvalidTarget",
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{
					Fact:     "sensor1",
					Operator: "greaterThan",
					Value:    50,
				},
			},
		},
		Event: rule.Event{
			Actions: []rule.Action{
				{
					Type:   "updateStore",
					Target: "invalidTargetKey", // Invalid target
					Value:  "newValue",
				},
				{
					Type:   "sendMessage",
					Target: "invalidAddress", // Invalid address
					Value:  "messageContent",
				},
			},
		},
	}

	compiled, err := CompileRule(&testRule)
	if err == nil {
		t.Errorf("CompileRule did not return an error for invalid targets")
	}

	// Check if the compiled output is empty as expected in case of an error
	if len(compiled) != 0 {
		t.Errorf("CompileRule returned compiled instructions despite invalid targets")
	}
}
