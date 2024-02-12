package instructiongen

import (
	"fmt"
	"rgehrsitz/rex/internal/bytecode"
	"rgehrsitz/rex/internal/rule"
	"rgehrsitz/rex/internal/validation"
)

// CompileConditions compiles the given Conditions into a sequence of bytecode instructions,
// includes optimized jumps for 'All' and 'Any' conditions, and returns any sensor dependencies encountered.
func CompileConditions(conditions rule.Conditions) ([]bytecode.Instruction, []string, error) {
	var instructions []bytecode.Instruction
	var sensorDependencies []string

	// Handle optimized conditions first
	optimizedInstructions, optimizedSensorDeps := optimizeConditions(conditions)
	if len(optimizedInstructions) > 0 {
		instructions = optimizedInstructions
		sensorDependencies = optimizedSensorDeps
	} else {
		// Compile 'All' conditions if present
		if len(conditions.All) > 0 {
			allInstructions, allSensorDeps, err := compileAllConditions(conditions.All)
			if err != nil {
				return nil, nil, err
			}
			instructions = append(instructions, allInstructions...)
			sensorDependencies = append(sensorDependencies, allSensorDeps...)
		}

		// Compile 'Any' conditions if present
		if len(conditions.Any) > 0 {
			anyInstructions, anySensorDeps, err := compileAnyConditions(conditions.Any)
			if err != nil {
				return nil, nil, err
			}
			instructions = append(instructions, anyInstructions...)
			sensorDependencies = append(sensorDependencies, anySensorDeps...)
		}
	}

	return instructions, deduplicate(sensorDependencies), nil
}

// deduplicate removes duplicate strings from a slice.
func deduplicate(items []string) []string {
	seen := make(map[string]struct{})
	var result []string

	for _, item := range items {
		if _, ok := seen[item]; !ok {
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}

	return result
}

// CompileCondition compiles a single condition into bytecode instructions
// and returns any sensor dependencies encountered.func CompileCondition(cond rule.Condition) ([]bytecode.Instruction, []string, error) {
func CompileCondition(cond rule.Condition) ([]bytecode.Instruction, []string, error) {
	switch {
	case cond.Operator != "":
		instructions, sensorDeps, err := compileComparisonCondition(cond)
		if err != nil {
			return nil, nil, err
		}
		// Sensor dependency is determined within the comparison condition function.
		return instructions, sensorDeps, nil

	case len(cond.All) > 0:
		instructions, sensorDeps, err := compileAllConditions(cond.All)
		if err != nil {
			return nil, nil, err
		}
		return instructions, sensorDeps, nil

	case len(cond.Any) > 0:
		instructions, sensorDeps, err := compileAnyConditions(cond.Any)
		if err != nil {
			return nil, nil, err
		}
		return instructions, sensorDeps, nil

	default:
		return nil, nil, fmt.Errorf("invalid condition: %+v", cond)
	}
}

func compileComparisonCondition(cond rule.Condition) ([]bytecode.Instruction, []string, error) {
	var sensorDeps []string
	if cond.Fact != "" {
		sensorDeps = append(sensorDeps, cond.Fact) // Assuming cond.Fact is a sensor.
	}

	switch cond.Operator {
	case "equal":
		instructions, err := compileEquals(cond)
		return instructions, sensorDeps, err
	case "notEqual":
		instructions, err := compileNotEqual(cond)
		return instructions, sensorDeps, err
	case "greaterThan":
		instructions, err := compileGreaterThan(cond)
		return instructions, sensorDeps, err
	case "lessThan":
		instructions, err := compileLessThan(cond)
		return instructions, sensorDeps, err
	case "greaterThanOrEqual":
		instructions, err := compileGreaterThanOrEqual(cond)
		return instructions, sensorDeps, err
	case "lessThanOrEqual":
		instructions, err := compileLessThanOrEqual(cond)
		return instructions, sensorDeps, err
	default:
		return nil, nil, fmt.Errorf("unknown or unsupported comparison operator: %s", cond.Operator)
	}
}

// compileAllConditions compiles all conditions with logical AND semantics.
// It adds jump instructions to skip actions if any condition is false.
func compileAllConditions(conditions []rule.Condition) ([]bytecode.Instruction, []string, error) {
	var instructions []bytecode.Instruction
	var allSensorDeps []string
	var jumpToEndIndexes []int // Track positions of jump instructions for later adjustment

	for _, cond := range conditions {
		condInstructions, condDeps, err := CompileCondition(cond)
		if err != nil {
			return nil, nil, err
		}
		instructions = append(instructions, condInstructions...)
		allSensorDeps = append(allSensorDeps, condDeps...)

		// Append a jump instruction to skip the rest of the conditions and actions if the current condition is false.
		jumpToEnd := bytecode.Instruction{
			Opcode:   bytecode.OpJumpIfFalse,
			Operands: []interface{}{0}, // Placeholder for jump distance; to be calculated later.
		}
		instructions = append(instructions, jumpToEnd)
		jumpToEndIndexes = append(jumpToEndIndexes, len(instructions)-1)
	}

	// Calculate and update the jump distances for the jump instructions
	// Assuming here that actions or end of rule instructions follow immediately after conditions.
	finalInstructionCount := len(instructions) // This might need adjustment if actions are appended later
	for _, index := range jumpToEndIndexes {
		instructions[index].Operands[0] = finalInstructionCount - index
	}

	return instructions, deduplicate(allSensorDeps), nil
}

// Revised compileAnyConditions function implementation
func compileAnyConditions(conditions []rule.Condition) ([]bytecode.Instruction, []string, error) {
	var instructions []bytecode.Instruction
	var sensorDeps []string
	var jumpToEndIndexes []int // To keep track of jump instruction positions for later adjustment.

	for i, cond := range conditions {
		condInstructions, condDeps, err := CompileCondition(cond)
		if err != nil {
			return nil, nil, err
		}
		instructions = append(instructions, condInstructions...)
		sensorDeps = append(sensorDeps, condDeps...)

		// For each condition, append a jump instruction to skip the rest of the conditions if the current one evaluates to true.
		// This jump instruction will be appended after all but the last condition.
		if i < len(conditions)-1 {
			jumpToEnd := bytecode.Instruction{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{0}} // Placeholder for jump distance.
			instructions = append(instructions, jumpToEnd)
			jumpToEndIndexes = append(jumpToEndIndexes, len(instructions)-1)
		}
	}

	// Assuming 'instructions' is a slice of all compiled instructions for the conditions
	// and 'jumpToEndIndexes' contains the indexes of jump instructions within this slice.
	for _, index := range jumpToEndIndexes {
		// The distance to jump is the total length of 'instructions' minus the current index.
		// This calculation assumes that you are at the position of the jump instruction in 'instructions'
		// and need to jump over the remaining part of the instructions list.
		jumpDistance := len(instructions) - index // Subtract 1 to account for the current position being on the jump instruction itself.

		// Update the jump instruction to reflect the calculated distance.
		instructions[index].Operands[0] = jumpDistance
	}

	return instructions, deduplicate(sensorDeps), nil
}

func compileEquals(cond rule.Condition) ([]bytecode.Instruction, error) {
	// Assuming cond.Value is already validated and is the value to compare against the fact.
	instructions := []bytecode.Instruction{
		{
			Opcode:   bytecode.OpLoadFact,
			Operands: []interface{}{cond.Fact},
		},
		{
			Opcode:   bytecode.OpEqual,
			Operands: []interface{}{cond.Value},
		},
	}
	return instructions, nil
}

// Greater Than Condition
func compileGreaterThan(cond rule.Condition) ([]bytecode.Instruction, error) {
	return []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{cond.Fact}},
		{Opcode: bytecode.OpGreaterThan, Operands: []interface{}{cond.Value}},
	}, nil
}

// Less Than Condition
func compileLessThan(cond rule.Condition) ([]bytecode.Instruction, error) {
	return []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{cond.Fact}},
		{Opcode: bytecode.OpLessThan, Operands: []interface{}{cond.Value}},
	}, nil
}

// Greater Than or Equal Condition
func compileGreaterThanOrEqual(cond rule.Condition) ([]bytecode.Instruction, error) {
	return []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{cond.Fact}},
		{Opcode: bytecode.OpGreaterThanOrEqual, Operands: []interface{}{cond.Value}},
	}, nil
}

// Less Than or Equal Condition
func compileLessThanOrEqual(cond rule.Condition) ([]bytecode.Instruction, error) {
	return []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{cond.Fact}},
		{Opcode: bytecode.OpLessThanOrEqual, Operands: []interface{}{cond.Value}},
	}, nil
}

// Not Equal Condition
func compileNotEqual(cond rule.Condition) ([]bytecode.Instruction, error) {
	return []bytecode.Instruction{
		{Opcode: bytecode.OpLoadFact, Operands: []interface{}{cond.Fact}},
		{Opcode: bytecode.OpNotEqual, Operands: []interface{}{cond.Value}},
	}, nil
}

// CompileAction compiles a given action into bytecode instructions and returns any sensor dependencies.
func CompileAction(action rule.Action) ([]bytecode.Instruction, []string, error) {
	var instructions []bytecode.Instruction
	var sensorDependencies []string // For actions, sensor dependencies might be less common but are tracked similarly.

	switch action.Type {
	case "updateStore":
		if !validation.IsValidStoreKey(action.Target) {
			return nil, nil, fmt.Errorf("invalid store key: %s", action.Target)
		}
		instructions = append(instructions, bytecode.Instruction{
			Opcode:   bytecode.OpUpdateStore,
			Operands: []interface{}{action.Target, action.Value},
		})

	case "sendMessage":
		if !validation.IsValidAddress(action.Target) {
			return nil, nil, fmt.Errorf("invalid address: %s", action.Target)
		}
		instructions = append(instructions, bytecode.Instruction{
			Opcode:   bytecode.OpSendMessage,
			Operands: []interface{}{action.Target, action.Value},
		})

	default:
		return nil, nil, fmt.Errorf("unsupported action type: %s", action.Type)
	}

	// If actions can affect or depend on sensor values, add logic here to capture those dependencies.
	// Example: If an action reads a sensor value before deciding on an update, track that sensor as a dependency.

	return instructions, sensorDependencies, nil
}

func optimizeConditions(conditions rule.Conditions) ([]bytecode.Instruction, []string) {
	var optimizedInstructions []bytecode.Instruction
	equalityChecks := make(map[string][]interface{})

	// Loop through all conditions to find 'equal' conditions on the same fact
	for _, cond := range conditions.All {
		if cond.Operator == "equal" {
			equalityChecks[cond.Fact] = append(equalityChecks[cond.Fact], cond.Value)
		}
	}

	// For each fact with multiple 'equal' conditions, generate an OpEqualAny instruction
	for fact, values := range equalityChecks {
		if len(values) > 1 { // Only optimize if there are multiple equal checks on the same fact
			optimizedInstructions = append(optimizedInstructions, bytecode.Instruction{
				Opcode:   bytecode.OpLoadFact,
				Operands: []interface{}{fact},
			})
			optimizedInstructions = append(optimizedInstructions, bytecode.Instruction{
				Opcode:   bytecode.OpEqualAny,
				Operands: []interface{}{values},
			})
		}
	}

	// If we have generated any optimized instructions, return them along with the sensor dependencies
	if len(optimizedInstructions) > 0 {
		sensorDeps := make([]string, 0, len(equalityChecks))
		for fact := range equalityChecks {
			sensorDeps = append(sensorDeps, fact)
		}
		return optimizedInstructions, deduplicate(sensorDeps)
	}

	// If no optimizations were applicable, return nil to indicate no optimizations were done
	return nil, nil
}
