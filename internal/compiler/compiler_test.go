package compiler

import (
	"reflect"
	"rgehrsitz/rex/internal/bytecode"
	"rgehrsitz/rex/internal/rule"
	"strings"
	"testing"
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

	instructions, err := CompileRule(r)
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
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{6}}, // Jump past second condition
		// Second condition
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"windSpeed"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{20}},
	}

	instructions, err := CompileRule(r)
	if err != nil {
		t.Fatalf("CompileRule failed: %v", err)
	}

	if !reflect.DeepEqual(instructions, expected) {
		t.Errorf("Expected %v, got %v", expected, instructions)
	}
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

	instructions, err := CompileRule(r)
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

	_, err := CompileRule(r)
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

	// Expected bytecode includes instructions for nested 'Any' logic within 'All'
	expected := []bytecode.Instruction{
		// First 'All' condition
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{30}},
		// Nested 'Any' conditions
		// First 'Any' condition
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"humidity"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{50}},
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{6}}, // Jump past second 'Any' condition
		// Second 'Any' condition
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"windSpeed"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{20}},
	}

	instructions, err := CompileRule(r)
	if err != nil {
		t.Fatalf("CompileRule failed: %v", err)
	}

	if !reflect.DeepEqual(instructions, expected) {
		t.Errorf("Expected %v, got %v", expected, instructions)
	}
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

	// Expected bytecode includes a mix of instructions for 'All', 'Any', and nested conditions
	expected := []bytecode.Instruction{
		// Instructions for first condition in 'All'
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{30}},
		// Instructions for 'Any' block
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"humidity"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{50}},
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{6}}, // Jump to end of 'Any' block (corrected destination)
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"windSpeed"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{20}},
	}

	instructions, err := CompileRule(r)
	if err != nil {
		t.Fatalf("CompileRule failed: %v", err)
	}

	if !reflect.DeepEqual(instructions, expected) {
		t.Errorf("Expected %v, got %v", expected, instructions)
	}
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
			CustomProperty: "Temperature too high",
		},
	}

	// Define expected bytecode, assuming specific opcodes for event handling
	expected := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{30}},
		// Assuming an Opcode for triggering an event, with event details as operands
		{Opcode: bytecode.OpTriggerEvent, Operands: []interface{}{"Alert", "Temperature too high"}},
	}

	instructions, err := CompileRule(r)
	if err != nil {
		t.Fatalf("CompileRule failed: %v", err)
	}

	if !reflect.DeepEqual(instructions, expected) {
		t.Errorf("Expected %v, got %v", expected, instructions)
	}
}

// TestCompileRulesWithDependencies tests the compilation of rules with dependency analysis
func TestCompileRulesWithDependencies(t *testing.T) {
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

	expectedInstructionsRule1 := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{30}},
		{Opcode: bytecode.OpUpdateStore, Operands: []interface{}{"alertLevel", "high"}},
		{Opcode: bytecode.OpTriggerEvent, Operands: []interface{}{"TemperatureHigh", nil}},
	}

	expectedInstructionsRule2 := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"alertLevel"}},
		{Opcode: bytecode.OpEqual, Operands: []interface{}{"high"}},
		{Opcode: bytecode.OpSendMessage, Operands: []interface{}{"192.168.0.1", "Alert level high"}},
		{Opcode: bytecode.OpTriggerEvent, Operands: []interface{}{"SendAlert", nil}},
	}

	// Expected output
	expected := []CompiledRule{
		{
			Instructions: expectedInstructionsRule1,
			Dependencies: []string{}, // Rule1 has no dependencies
		},
		{
			Instructions: expectedInstructionsRule2,
			Dependencies: []string{"Rule1"}, // Rule2 is dependent on Rule1
		},
		// Add more expected compiled rules if needed
	}

	// Perform the compilation
	compiledRules, err := CompileRulesWithDependencies(rules)
	if err != nil {
		t.Fatalf("CompileRulesWithDependencies returned an error: %v", err)
	}

	// Compare the result with the expected output
	if !reflect.DeepEqual(compiledRules, expected) {
		t.Errorf("CompileRulesWithDependencies = %v, want %v", compiledRules, expected)
	}
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
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"humidity"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{50}},
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{8}}, // Assuming jump logic for 'Any'
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"windSpeed"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{20}},
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{25}},
	}

	instructions, err := CompileRule(r)
	if err != nil {
		t.Fatalf("CompileRule failed: %v", err)
	}

	if !reflect.DeepEqual(instructions, expected) {
		t.Errorf("Expected %v, got %v", expected, instructions)
	}
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

	instructions, err := CompileRule(r)
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

	// Compile the rule
	instructions, err := CompileRule(unsupportedRule)

	// Check that no instructions were generated and an error was returned
	if instructions != nil {
		t.Errorf("Expected no instructions to be generated for an unsupported operator, got: %v", instructions)
	}

	if err == nil {
		t.Fatal("Expected an error to be returned for an unsupported operator, got nil")
	}

	if !containsSubstring(err.Error(), "unsupported operator") {
		t.Errorf("Expected error message to contain 'unsupported operator', got: %s", err.Error())
	}
}

// containsSubstring checks if a string contains a specific substring.
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

	// Compile the complex rule
	compiledInstructions, err := CompileRule(complexRule)
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

	// Compile the rule
	compiled, err := CompileRule(testRule)
	if err == nil {
		t.Errorf("CompileRule did not return an error for invalid targets")
	}

	// Check if the compiled output is empty as expected in case of an error
	if len(compiled) != 0 {
		t.Errorf("CompileRule returned compiled instructions despite invalid targets")
	}
}
