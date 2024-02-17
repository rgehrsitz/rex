package instructiongen

import (
	"fmt"
	"rgehrsitz/rex/internal/bytecode"
	"rgehrsitz/rex/internal/rule"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompileEquals(t *testing.T) {
	cond := rule.Condition{
		Fact:     "temperature",
		Operator: "equal",
		Value:    25,
	}

	expectedInstructions := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpEqual, Operands: []interface{}{25}},
	}

	instructions, err := compileEquals(cond)

	assert.NoError(t, err)
	assert.Equal(t, expectedInstructions, instructions, "compileEquals did not generate the expected instructions")
}

func TestCompileConditions_SingleCondition(t *testing.T) {
	conditions := rule.Conditions{
		All: []rule.Condition{
			{Fact: "pressure", Operator: "greaterThan", Value: 100},
		},
	}

	// Expected bytecode instructions for "pressure" > 100 condition
	expectedInstructions := []bytecode.Instruction{
		{
			Opcode:   bytecode.OpLoadFact,
			Operands: []interface{}{"pressure"},
		},
		{
			Opcode:   bytecode.OpGreaterThan,
			Operands: []interface{}{100},
		},
		// Following the logic provided, we expect a jump instruction to skip over actions if the condition is false.
		// The placeholder for the jump target is 0, which we'll adjust based on the implementation details.
		// Note: In actual test, you'll need to adjust this based on how CompileConditions updates jump targets.
		{
			Opcode:   bytecode.OpJumpIfFalse,
			Operands: []interface{}{1}, // Assuming placeholder logic; actual test may adjust this.
		},
	}

	instructions, sensorDependencies, err := CompileConditions(conditions)

	// No error should occur during the compilation
	assert.NoError(t, err, "CompileConditions should not return an error for a valid condition")

	// The sensor dependencies should include "pressure"
	expectedSensorDependencies := []string{"pressure"}
	assert.Equal(t, expectedSensorDependencies, sensorDependencies, "Sensor dependencies did not match expected")

	// Instructions need careful comparison, especially if your logic includes placeholders for jumps.
	// For simplicity, this compares the length and specific opcodes. Adjust as needed for detailed verification.
	assert.Equal(t, len(expectedInstructions), len(instructions), "Number of generated instructions does not match expected")
	for i, inst := range instructions {
		assert.Equal(t, expectedInstructions[i].Opcode, inst.Opcode, "Opcode of instruction does not match expected at index %d", i)
		// Further comparisons can be made here, such as operands, especially for jumps.
	}
}

func TestCompileGreaterThan(t *testing.T) {
	cond := rule.Condition{
		Fact:     "speed",
		Operator: "greaterThan",
		Value:    60,
	}

	expectedInstructions := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"speed"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{60}},
	}

	instructions, err := compileGreaterThan(cond)

	assert.NoError(t, err)
	assert.Equal(t, expectedInstructions, instructions, "compileGreaterThan did not generate the expected instructions")
}

func TestCompileAction_UpdateStore(t *testing.T) {
	action := rule.Action{
		Type:   "updateStore",
		Target: "alarmState",
		Value:  "active",
	}

	expectedInstructions := []bytecode.Instruction{
		{Opcode: bytecode.OpUpdateStore, Operands: []interface{}{"alarmState", "active"}},
	}

	instructions, sensorDependencies, err := CompileAction(action)

	assert.NoError(t, err)
	assert.Equal(t, 0, len(sensorDependencies), "UpdateStore action should not have sensor dependencies")
	assert.Equal(t, expectedInstructions, instructions, "CompileAction did not generate the expected instructions for updateStore action")
}

func TestDeduplicate(t *testing.T) {
	input := []string{"temp", "pressure", "temp", "humidity", "pressure"}
	expected := []string{"temp", "pressure", "humidity"}

	result := deduplicate(input)

	assert.Equal(t, expected, result, "deduplicate function did not return the expected unique slice")
}

func TestCompileAllConditions(t *testing.T) {
	conditions := []rule.Condition{
		{Fact: "temp", Operator: "greaterThan", Value: 20},
		{Fact: "humidity", Operator: "lessThan", Value: 80},
	}

	expectedInstructions := []bytecode.Instruction{
		// Assuming compileGreaterThan generates these instructions for the first condition
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temp"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{20}},
		{Opcode: bytecode.OpJumpIfFalse, Operands: []interface{}{4}}, // Placeholder for jump; will need adjustment based on actual logic

		// Assuming compileLessThan generates these instructions for the second condition
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"humidity"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{80}},
		{Opcode: bytecode.OpJumpIfFalse, Operands: []interface{}{1}},
	}
	expectedSensorDependencies := []string{"temp", "humidity"}

	instructions, sensorDependencies, err := compileAllConditions(conditions)

	assert.NoError(t, err, "compileAllConditions should not return an error")
	assert.Equal(t, expectedInstructions, instructions, "Instructions generated by compileAllConditions do not match expected")
	assert.ElementsMatch(t, expectedSensorDependencies, sensorDependencies, "Sensor dependencies generated by compileAllConditions do not match expected")
}

func TestCompileEquals2(t *testing.T) {
	cond := rule.Condition{Fact: "temperature", Operator: "equal", Value: 25}
	expected := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpEqual, Operands: []interface{}{25}},
	}
	instructions, err := compileEquals(cond)
	assert.NoError(t, err)
	assert.Equal(t, expected, instructions)
}

func TestCompileCondition_UnsupportedOperator(t *testing.T) {
	cond := rule.Condition{Fact: "temp", Operator: "unknown", Value: 30}
	_, _, err := CompileCondition(cond)
	assert.Error(t, err)
}

func TestCompileCondition_NestedAll(t *testing.T) {
	nestedConditions := rule.Condition{
		All: []rule.Condition{
			{
				Fact:     "temp",
				Operator: "greaterThan",
				Value:    20,
			},
			{
				Fact:     "humidity",
				Operator: "lessThan",
				Value:    80,
			},
		},
	}

	// Expected bytecode instructions considering logical AND operation
	expectedInstructions := []bytecode.Instruction{
		// Assuming compileGreaterThan generates these instructions for the first condition "temp > 20"
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temp"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{20}},
		{Opcode: bytecode.OpJumpIfFalse, Operands: []interface{}{4}}, // Jump to skip next condition if false

		// Assuming compileLessThan generates these instructions for the second condition "humidity < 80"
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"humidity"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{80}},
		{Opcode: bytecode.OpJumpIfFalse, Operands: []interface{}{1}}, // Jump to skip the action if false
	}

	// Execute CompileCondition with the nested 'All' condition
	instructions, sensorDeps, err := CompileCondition(nestedConditions)

	// Assertions
	assert.NoError(t, err, "CompileCondition should not return an error for valid nested 'All' conditions")
	assert.Equal(t, expectedInstructions, instructions, "Compiled instructions for nested 'All' conditions do not match expected")
	assert.ElementsMatch(t, []string{"temp", "humidity"}, sensorDeps, "Sensor dependencies for nested 'All' conditions do not match expected")
}

func TestOptimizeConditions_EqualChecks(t *testing.T) {
	conditions := rule.Conditions{
		All: []rule.Condition{
			{Fact: "status", Operator: "equal", Value: "OK"},
			{Fact: "status", Operator: "equal", Value: "Warning"},
			{Fact: "status", Operator: "equal", Value: "Critical"},
		},
	}

	expectedInstructions := []bytecode.Instruction{
		{
			Opcode:   bytecode.OpLoadFact,
			Operands: []interface{}{"status"},
		},
		{
			Opcode:   bytecode.OpEqualAny,
			Operands: []interface{}{[]interface{}{"OK", "Warning", "Critical"}},
		},
	}

	optimizedInstructions, sensorDeps := optimizeConditions(conditions)

	assert.NotNil(t, optimizedInstructions, "Optimized instructions should not be nil")
	assert.Equal(t, expectedInstructions, optimizedInstructions, "The optimized instructions do not match the expected output")
	assert.Contains(t, sensorDeps, "status", "Sensor dependencies should contain 'status'")
	assert.Len(t, sensorDeps, 1, "There should be exactly one sensor dependency")
}

func TestCompileCondition_ComplexNested(t *testing.T) {
	cond := rule.Condition{
		All: []rule.Condition{
			{
				Any: []rule.Condition{
					{Fact: "temp", Operator: "lessThan", Value: 0},
					{Fact: "temp", Operator: "greaterThan", Value: 100},
				},
			},
			{Fact: "pressure", Operator: "greaterThan", Value: 1200},
		},
	}

	expectedInstructions := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temp"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{0}},
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{3}},
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temp"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{100}},
		{Opcode: bytecode.OpJumpIfFalse, Operands: []interface{}{4}},
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"pressure"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{1200}},
		{Opcode: bytecode.OpJumpIfFalse, Operands: []interface{}{1}},
	}

	instructions, _, err := CompileCondition(cond)

	assert.NoError(t, err, "CompileCondition should not return an error for valid nested conditions")
	assert.Equal(t, len(expectedInstructions), len(instructions), "Number of generated instructions does not match expected")
	for i, inst := range instructions {
		assert.Equal(t, expectedInstructions[i].Opcode, inst.Opcode, fmt.Sprintf("Opcode mismatch at instruction %d", i))
		if inst.Opcode == bytecode.OpJumpIfTrue {
			// Verify the jump distance is correctly calculated.
			assert.Equal(t, expectedInstructions[i].Operands[0], inst.Operands[0], fmt.Sprintf("Jump distance mismatch at instruction %d", i))
		} else {
			// For non-jump instructions, verify operands match.
			assert.Equal(t, expectedInstructions[i].Operands, inst.Operands, fmt.Sprintf("Operands mismatch at instruction %d", i))
		}
	}
}

func TestCompileConditions_AnyConditions(t *testing.T) {
	conditions := rule.Conditions{
		Any: []rule.Condition{
			{Fact: "humidity", Operator: "lessThan", Value: 30},
			{Fact: "humidity", Operator: "greaterThan", Value: 70},
			{Fact: "temperature", Operator: "greaterThan", Value: 25}, // Additional condition
		},
	}

	// Corrected bytecode sequence expectations based on the discussion:
	// - Each condition compiles to two instructions: load fact and comparison.
	// - For 'Any' conditions, we want to jump over the next conditions if the current one evaluates to true.
	expectedInstructions := []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"humidity"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{30}},
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{6}}, // Jump to skip two conditions if true.
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"humidity"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{70}},
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{3}}, // Jump to skip one condition if true.
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{25}},
		// No jump after the last condition since we proceed to actions if it's true.
	}

	instructions, sensorDependencies, err := CompileConditions(conditions)

	assert.NoError(t, err, "CompileConditions should not return an error for valid 'Any' conditions")
	assert.Equal(t, expectedInstructions, instructions, "Instructions generated by CompileConditions do not match expected for 'Any' conditions")
	assert.Contains(t, sensorDependencies, "humidity", "Sensor dependencies should contain 'humidity'")
	assert.Contains(t, sensorDependencies, "temperature", "Sensor dependencies should also contain 'temperature'")
	// Ensuring the correct number of sensor dependencies is identified.
	assert.Len(t, sensorDependencies, 2, "There should be exactly two sensor dependencies identified")
}

func TestCompileAnyConditions(t *testing.T) {
	// Define conditions that simulate realistic scenarios where 'Any' logic is applied.
	conditions := []rule.Condition{
		{Fact: "temperature", Operator: "greaterThan", Value: 25},
		{Fact: "rainChance", Operator: "greaterThan", Value: 50},
	}

	// Expected bytecode instructions.
	// These instructions assume a direct translation of the conditions into bytecode,
	// with jump instructions to bypass all remaining conditions if one evaluates to true.
	expectedInstructions := []bytecode.Instruction{
		// First condition: If temperature > 25
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"temperature"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{25}},
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{3}}, // Jump past next condition check if true

		// Second condition: If rainChance > 50 (no jump needed after the last condition)
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"rainChance"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{50}},
	}

	actualInstructions, sensorDependencies, err := compileAnyConditions(conditions)

	assert.NoError(t, err, "compileAnyConditions should not produce an error")
	assert.Equal(t, expectedInstructions, actualInstructions, "The compiled instructions did not match the expected output")
	assert.ElementsMatch(t, []string{"temperature", "rainChance"}, sensorDependencies, "Sensor dependencies should accurately reflect the conditions' facts")
}

func TestCompileCondition_DeepNestedAllWithinAny(t *testing.T) {
	cond := rule.Condition{
		Any: []rule.Condition{
			{
				All: []rule.Condition{
					{Fact: "windSpeed", Operator: "greaterThan", Value: 25},
					{Fact: "visibility", Operator: "lessThan", Value: 1000},
				},
			},
			{Fact: "weather", Operator: "equal", Value: "rainy"},
		},
	}

	expectedInstructions := []bytecode.Instruction{
		// Evaluate the first condition in the 'All' block
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"windSpeed"}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{25}},
		// If windSpeed > 25 is false, jump to the next condition (weather == "rainy")
		{Opcode: bytecode.OpJumpIfFalse, Operands: []interface{}{3}}, // Adjusted jump distance if necessary
		// Evaluate the second condition in the 'All' block
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"visibility"}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{1000}},
		// If both conditions in 'All' are true, jump over the evaluation of "weather" == "rainy"
		{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{3}}, // Jump past the next condition
		// Evaluate the second (standalone) condition in the 'Any' block
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{"weather"}},
		{Opcode: bytecode.OpEqual, Operands: []interface{}{"rainy"}},
	}

	instructions, _, err := CompileCondition(cond)

	assert.NoError(t, err, "CompileCondition should not return an error for valid deeply nested conditions")
	assert.Equal(t, len(expectedInstructions), len(instructions), "Number of generated instructions does not match expected")
	for i, inst := range instructions {
		assert.Equal(t, expectedInstructions[i].Opcode, inst.Opcode, fmt.Sprintf("Opcode mismatch at instruction %d", i))
		assert.Equal(t, expectedInstructions[i].Operands, inst.Operands, fmt.Sprintf("Operands mismatch at instruction %d", i))
	}
}
