package compiler

import (
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

	expectedInstructions := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{30}},
	}
	expectedSensorDependencies := []string{"temperature"}

	instructions, sensorDependencies, err := CompileRule(&r)

	assert.NoError(t, err, "CompileRule should not fail")
	assert.Equal(t, expectedInstructions, instructions, "Compiled instructions do not match expected")
	assert.ElementsMatch(t, expectedSensorDependencies, sensorDependencies, "Sensor dependencies do not match expected")
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
	expectedInstructions := []bytecode.Instruction{
		// First condition
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"humidity"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{50}},
		// Corrected jump instruction: should jump over the next condition if true
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{2}}, // Corrected to jump past just the next condition
		// Second condition
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"windSpeed"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{20}},
	}
	expectedSensorDependencies := []string{"humidity", "windSpeed"}

	instructions, sensorDependencies, err := CompileRule(&r)

	assert.NoError(t, err, "CompileRule should not fail")
	assert.Equal(t, expectedInstructions, instructions, "Compiled instructions do not match expected")
	assert.ElementsMatch(t, expectedSensorDependencies, sensorDependencies, "Sensor dependencies do not match expected")
}

// TestCompileEmptyConditions tests compiling a rule with no conditions.
func TestCompileEmptyConditions(t *testing.T) {
	r := rule.Rule{
		Conditions: rule.Conditions{
			All: []rule.Condition{}, // No conditions specified
			Any: []rule.Condition{},
		},
	}

	instructions, sensorDependencies, err := CompileRule(&r)

	assert.NoError(t, err, "CompileRule should not fail")
	assert.Empty(t, instructions, "Expected an empty instruction set")
	assert.Empty(t, sensorDependencies, "Expected no sensor dependencies")
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

	_, _, err := CompileRule(&r) // Adjusted to capture all return values
	assert.Error(t, err, "Expected an error for invalid operator")

	// Optionally, you can check for specific error messages if your implementation
	// returns descriptive errors for different types of invalid conditions.
	expectedErrMsg := "unsupported operator: invalidOperator"
	assert.EqualError(t, err, expectedErrMsg, "Error message does not match expected")
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

	expectedInstructions := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{30}},
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"humidity"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{50}},
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{2}}, // Corrected jump operand
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"windSpeed"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{20}},
	}
	// Since we're dealing with nested conditions, let's assume the sensors "temperature", "humidity", and "windSpeed" are involved.
	expectedSensorDependencies := []string{"temperature", "humidity", "windSpeed"}

	instructions, sensorDependencies, err := CompileRule(&r)

	assert.NoError(t, err, "CompileRule should not fail")
	assert.Equal(t, expectedInstructions, instructions, "Compiled instructions do not match expected")
	assert.ElementsMatch(t, expectedSensorDependencies, sensorDependencies, "Sensor dependencies do not match expected")
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
	expectedInstructions := []bytecode.Instruction{
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
	// Assuming expectedSensorDependencies based on the rule's conditions
	expectedSensorDependencies := []string{"temperature", "humidity", "windSpeed", "isRaining", "dayOfWeek"}

	instructions, sensorDependencies, err := CompileRule(&r)

	assert.NoError(t, err, "CompileRule should not fail")
	assert.Equal(t, expectedInstructions, instructions, "Compiled instructions do not match expected")
	assert.ElementsMatch(t, expectedSensorDependencies, sensorDependencies, "Sensor dependencies do not match expected")
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
			CustomProperty: "Temperature too high", // This might not directly translate to an operand based on your CompileRule implementation.
		},
	}

	// Define expected bytecode, assuming specific opcodes for event handling
	expectedInstructions := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{30}},
		// Adjust according to the actual implementation for event handling
		{Opcode: bytecode.OpTriggerEvent, Operands: []interface{}{"Alert", "Temperature too high"}},
	}
	expectedSensorDependencies := []string{"temperature"} // Assuming temperature is the only sensor dependency

	instructions, sensorDependencies, err := CompileRule(&r)

	assert.NoError(t, err, "CompileRule should not fail")
	assert.Equal(t, expectedInstructions, instructions, "Compiled instructions do not match expected")
	assert.ElementsMatch(t, expectedSensorDependencies, sensorDependencies, "Sensor dependencies do not match expected")
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
			RuleDependencies:   nil, // Use nil to represent no dependencies, matching the actual output
			SensorDependencies: []string{"temperature"},
		},
		{
			Name: "Rule2", // Add the correct rule name
			Instructions: []bytecode.Instruction{
				{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"alertLevel"}},
				{Opcode: bytecode.OpEqual, Operands: []interface{}{"high"}},
				{Opcode: bytecode.OpSendMessage, Operands: []interface{}{"192.168.0.1", "Alert level high"}},
				{Opcode: bytecode.OpTriggerEvent, Operands: []interface{}{"SendAlert", nil}},
			},
			RuleDependencies:   []string{"Rule1"}, // Correctly reflect dependencies
			SensorDependencies: []string{"alertLevel"},
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
	expectedInstructions := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"humidity"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{50}},
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{4}}, // Adjusted for dynamic calculation
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"windSpeed"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{20}},
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{25}},
	}
	// Since this rule involves complex nested conditions, identify the sensor dependencies
	expectedSensorDependencies := []string{"humidity", "windSpeed", "temperature"}

	instructions, sensorDependencies, err := CompileRule(&r)

	assert.NoError(t, err, "CompileRule should not return an error")
	assert.Equal(t, expectedInstructions, instructions, "Compiled instructions do not match expected output")
	assert.ElementsMatch(t, expectedSensorDependencies, sensorDependencies, "Sensor dependencies do not match expected")
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

	expectedInstructions := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{30}},
		{Opcode: bytecode.OpUpdateStore, Operands: []interface{}{"alertLevel", "high"}},
		{Opcode: bytecode.OpSendMessage, Operands: []interface{}{"192.168.0.101", "Temperature exceeded 30 degrees"}},
		{Opcode: bytecode.OpTriggerEvent, Operands: []interface{}{"HighTemperature", nil}},
	}
	// Assuming no sensor dependencies are directly specified by the actions in this case
	expectedSensorDependencies := []string{"temperature"}

	instructions, sensorDependencies, err := CompileRule(&r)

	assert.NoError(t, err, "CompileRule should not fail")
	assert.Equal(t, expectedInstructions, instructions, "Compiled instructions do not match expected")
	assert.ElementsMatch(t, expectedSensorDependencies, sensorDependencies, "Sensor dependencies do not match expected")
}

func TestCompileRuleWithUnsupportedOperator(t *testing.T) {
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

	instructions, sensorDependencies, err := CompileRule(&unsupportedRule)

	// Using assert library for cleaner checks
	assert.Empty(t, instructions, "Expected no instructions to be generated for an unsupported operator")
	assert.Empty(t, sensorDependencies, "Expected no sensor dependencies for an unsupported operator")
	assert.Error(t, err, "Expected an error to be returned for an unsupported operator")
	assert.Contains(t, err.Error(), "unsupported operator", "Expected error message to contain 'unsupported operator'")
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

	compiledInstructions, sensorDependencies, err := CompileRule(&complexRule)

	assert.NoError(t, err, "Failed to compile rule")
	assert.NotEmpty(t, compiledInstructions, "Compiled instructions are empty")

	// Example assertion: check if the first opcode is OpLoadFact for temperature
	expectedFirstInstruction := bytecode.Instruction{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}}
	assert.Equal(t, expectedFirstInstruction, compiledInstructions[0], "First instruction does not match expected load fact 'temperature'")

	// Since the rule involves sensors "temperature", "humidity", "windSpeed", and "rain",
	// assert that all are included in sensorDependencies
	expectedSensors := []string{"temperature", "humidity", "windSpeed", "rain"}
	assert.ElementsMatch(t, expectedSensors, sensorDependencies, "Sensor dependencies do not match expected")

	// Additional assertions can be added as needed to validate the compilation details
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

	instructions, sensorDependencies, err := CompileRule(&testRule)

	// Use assert to check if an error was returned due to invalid targets
	assert.Error(t, err, "CompileRule should return an error for invalid targets")

	// Additionally, check if the error is specific to invalid targets if your implementation supports detailed error messages

	// Check if the compiled output is empty as expected in case of an error
	assert.Empty(t, instructions, "CompileRule should not return instructions for rules with invalid targets")

	// Since sensorDependencies is not directly related to the validity of the targets, its assertion might not be necessary unless your logic specifies otherwise.
	// However, if you want to ensure it's empty or has specific content despite the error, you can assert as follows:
	assert.Empty(t, sensorDependencies, "Sensor dependencies should be empty for rules with invalid targets")
}

func TestCompileRuleWithCyclicDependencies(t *testing.T) {
	// Define rules that create a cyclic dependency
	rules := []rule.Rule{
		{
			Name: "RuleA",
			Conditions: rule.Conditions{
				All: []rule.Condition{
					{Fact: "factB", Operator: "equal", Value: true}, // Depends on a fact produced by RuleB
				},
			},
			Event: rule.Event{
				Actions: []rule.Action{
					{Type: "produceFact", Target: "factA", Value: true}, // Produces factA
				},
			},
		},
		{
			Name: "RuleB",
			Conditions: rule.Conditions{
				All: []rule.Condition{
					{Fact: "factA", Operator: "equal", Value: true}, // Depends on a fact produced by RuleA
				},
			},
			Event: rule.Event{
				Actions: []rule.Action{
					{Type: "produceFact", Target: "factB", Value: true}, // Produces factB
				},
			},
		},
	}

	// Attempt to compile the rules
	compiledRules, err := CompileRuleSet(rules)

	// Verify that the compiler correctly identifies and rejects the cyclic dependency
	assert.Nil(t, compiledRules, "Compiled rules should be nil due to cyclic dependency")
	assert.Error(t, err, "An error should be returned due to cyclic dependency")
}

func TestCompileRuleWithErrorHandling(t *testing.T) {
	// Define a rule with a condition that uses an unsupported operator
	r := rule.Rule{
		Name: "RuleWithError",
		Conditions: rule.Conditions{
			All: []rule.Condition{
				{Fact: "temperature", Operator: "unknownOperator", Value: 30},
			},
		},
		Event: rule.Event{
			Actions: []rule.Action{
				{Type: "notify", Target: "admin", Value: "Error encountered in rule evaluation"},
			},
		},
	}

	// Attempt to compile the rule
	_, _, err := CompileRule(&r)

	// Check if an error was returned due to the unsupported operator
	assert.Error(t, err, "Compilation should fail due to unsupported operator")
}
