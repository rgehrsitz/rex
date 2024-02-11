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

	// First, attempt to optimize the conditions before compiling them
	optimizedInstructions, optimizedSensorDeps := optimizeConditions(conditions)

	// If optimizations were applied, use those instructions and dependencies
	if len(optimizedInstructions) > 0 {
		instructions = optimizedInstructions
		sensorDependencies = optimizedSensorDeps
	} else {
		// Otherwise, compile conditions as before
		// Compile 'All' conditions with an early exit jump if any condition is false.
		if len(conditions.All) > 0 {
			var endOfAllJumpIndexes []int
			for _, cond := range conditions.All {
				condInstructions, condDeps, err := CompileCondition(cond)
				if err != nil {
					return nil, nil, err
				}
				instructions = append(instructions, condInstructions...)
				sensorDependencies = append(sensorDependencies, condDeps...)
				// Add jump instruction placeholder for false evaluation to skip remaining conditions and actions.
				jumpInst := bytecode.Instruction{Opcode: bytecode.OpJumpIfFalse, Operands: []interface{}{0}} // Placeholder
				instructions = append(instructions, jumpInst)
				endOfAllJumpIndexes = append(endOfAllJumpIndexes, len(instructions)-1)
			}
			// Update the jump targets for 'All' conditions.
			for _, index := range endOfAllJumpIndexes {
				instructions[index].Operands[0] = len(instructions) - index
			}
		}

		// Compile 'Any' conditions with an optimization to skip remaining conditions if one is true.
		if len(conditions.Any) > 0 {
			var jumpToEndIfTrueIndexes []int
			for _, cond := range conditions.Any {
				condInstructions, condDeps, err := CompileCondition(cond)
				if err != nil {
					return nil, nil, err
				}
				instructions = append(instructions, condInstructions...)
				sensorDependencies = append(sensorDependencies, condDeps...)
				// Add jump instruction to skip the rest of the 'Any' conditions if this condition is true.
				jumpInst := bytecode.Instruction{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{0}} // Placeholder
				instructions = append(instructions, jumpInst)
				jumpToEndIfTrueIndexes = append(jumpToEndIfTrueIndexes, len(instructions)-1)
			}
			// Update jump targets for 'Any' conditions to jump past the 'Any' block.
			for _, index := range jumpToEndIfTrueIndexes {
				instructions[index].Operands[0] = len(instructions) - index
			}
		}
	}

	// Deduplicate sensor dependencies
	sensorDependencies = deduplicate(sensorDependencies)

	return instructions, sensorDependencies, nil
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

	for i, cond := range conditions {
		condInstructions, condDeps, err := CompileCondition(cond)
		if err != nil {
			return nil, nil, err
		}
		instructions = append(instructions, condInstructions...)
		allSensorDeps = append(allSensorDeps, condDeps...)

		// For all conditions except the last, add a jump instruction to skip to the end if the condition is false.
		if i < len(conditions)-1 {
			jumpToEnd := bytecode.Instruction{
				Opcode:   bytecode.OpJumpIfFalse,
				Operands: []interface{}{0}, // Placeholder for the jump target, to be updated later.
			}
			instructions = append(instructions, jumpToEnd)
		}
	}

	// Update jump targets for all but the last condition.
	// The final position after all instructions for conditions and actions are compiled is not known here,
	// so the placeholder '0' is used and should be updated outside this function based on the overall rule compilation logic.
	for i := range instructions {
		if instructions[i].Opcode == bytecode.OpJumpIfFalse && i < len(instructions)-1 { // Avoid adjusting the last condition's non-existent jump
			// Assuming the actions immediately follow the conditions, and knowing the final instruction set size,
			// the jump target would be calculated to skip over all remaining conditions and actions.
			// This placeholder adjustment logic will need to be refined based on the complete compilation process.
			instructions[i].Operands[0] = len(instructions) - i
		}
	}

	return instructions, deduplicate(allSensorDeps), nil
}

// Any Conditions (Logical OR)
func compileAnyConditions(conditions []rule.Condition) ([]bytecode.Instruction, []string, error) {
	var instructions []bytecode.Instruction
	var anySensorDeps []string
	var jumpToEndIndexes []int // Track jump instruction positions for later update.

	for _, cond := range conditions {
		condInstructions, condDeps, err := CompileCondition(cond)
		if err != nil {
			return nil, nil, err
		}
		instructions = append(instructions, condInstructions...)
		anySensorDeps = append(anySensorDeps, condDeps...)

		// Append a jump instruction after each condition, to be updated later.
		jumpToEnd := bytecode.Instruction{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{0}}
		instructions = append(instructions, jumpToEnd)
		jumpToEndIndexes = append(jumpToEndIndexes, len(instructions)-1)
	}

	// Adjust the jump targets based on the final instruction count.
	for _, index := range jumpToEndIndexes {
		// Calculate distance to end of conditions block, assuming actions follow immediately.
		// This might need adjustment to account for any intermediate instructions.
		instructions[index].Operands[0] = len(instructions) - index + additionalOffset // Define additionalOffset based on actual layout.
	}

	return instructions, deduplicate(anySensorDeps), nil
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
