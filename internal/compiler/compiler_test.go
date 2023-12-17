package engine

import (
	"reflect"
	"rgehrsitz/rex/internal/bytecode"
	"rgehrsitz/rex/internal/rule"
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
				Action: rule.Action{
					Type:   "updateStore",
					Target: "alertLevel",
					Value:  "high",
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
				Action: rule.Action{
					Type:   "sendMessage",
					Target: "admin@example.com",
					Value:  "Alert level high",
				},
			},
		},
		// Add more rules if needed for testing
	}

	expectedInstructionsRule1 := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{30}},
		{Opcode: bytecode.OpTriggerEvent, Operands: []interface{}{"TemperatureHigh", nil}},
	}

	expectedInstructionsRule2 := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"alertLevel"}},
		{Opcode: bytecode.OpEqual, Operands: []interface{}{"high"}},
		{Opcode: bytecode.OpTriggerEvent, Operands: []interface{}{"SendAlert", nil}},
	}

	// Expected output
	expected := []CompiledRule{
		{
			Instructions: expectedInstructionsRule1,
			Dependencies: []string{"Rule2"}, // Rule2 is dependent on Rule1
		},
		{
			Instructions: expectedInstructionsRule2,
			Dependencies: []string{}, // Rule2 has no dependencies
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

// Additional tests for getWritesFacts, getReadsFacts, isDependent, etc.
func TestGetWritesFacts(t *testing.T) {
	r := rule.Rule{
		Event: rule.Event{
			Action: rule.Action{
				Type:   "updateStore",
				Target: "alertLevel",
			},
		},
	}

	expected := []string{"alertLevel"}
	writesFacts := getWritesFacts(r)

	if !reflect.DeepEqual(writesFacts, expected) {
		t.Errorf("getWritesFacts = %v, want %v", writesFacts, expected)
	}
}

func TestGetReadsFacts(t *testing.T) {
	r := rule.Rule{
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{Fact: "temperature"},
				{Fact: "humidity"},
			},
		},
	}

	expected := []string{"temperature", "humidity"}
	readsFacts := getReadsFacts(r)

	if !reflect.DeepEqual(readsFacts, expected) {
		t.Errorf("getReadsFacts = %v, want %v", readsFacts, expected)
	}
}

func TestIsDependent(t *testing.T) {
	rule1 := rule.Rule{
		Event: rule.Event{
			Action: rule.Action{
				Type:   "updateStore",
				Target: "alertLevel",
			},
		},
	}

	rule2 := rule.Rule{
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{Fact: "alertLevel"},
			},
		},
	}

	writesFacts := getWritesFacts(rule1)
	if !isDependent(writesFacts, rule2) {
		t.Errorf("Expected rule2 to be dependent on rule1")
	}
}
