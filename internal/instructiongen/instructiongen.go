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

// All Conditions (Logical AND)
func compileAllConditions(conditions []rule.Condition) ([]bytecode.Instruction, []string, error) {
	var instructions []bytecode.Instruction
	var jumpToEndIndexes []int // To keep track of where to update the jump target later.
	var allSensorDeps []string

	for _, cond := range conditions {
		condInstructions, condDeps, err := CompileCondition(cond)
		if err != nil {
			return nil, nil, err
		}
		instructions = append(instructions, condInstructions...)
		allSensorDeps = append(allSensorDeps, condDeps...)

		// Add a jump instruction if this condition is false. The exact jump target will be set later.
		jumpToEnd := bytecode.Instruction{
			Opcode:   bytecode.OpJumpIfFalse,
			Operands: []interface{}{0}, // Placeholder for the jump target, to be updated.
		}
		instructions = append(instructions, jumpToEnd)
		jumpToEndIndexes = append(jumpToEndIndexes, len(instructions)-1)
	}

	// Update jump targets now that we know the final length of instructions
	for _, index := range jumpToEndIndexes {
		instructions[index].Operands[0] = len(instructions) - index
	}

	return instructions, allSensorDeps, nil
}

// Any Conditions (Logical OR)
func compileAnyConditions(conditions []rule.Condition) ([]bytecode.Instruction, []string, error) {
	var instructions []bytecode.Instruction
	var jumpToEndIndexes []int // To keep track of where to update the jump target later.
	var anySensorDeps []string

	for i, cond := range conditions {
		condInstructions, condDeps, err := CompileCondition(cond)
		if err != nil {
			return nil, nil, err
		}
		instructions = append(instructions, condInstructions...)
		anySensorDeps = append(anySensorDeps, condDeps...)

		// Add jump instruction logic as in the original code
		if i < len(conditions)-1 {
			jumpToEnd := bytecode.Instruction{
				Opcode:   bytecode.OpJumpIfTrue,
				Operands: []interface{}{0}, // Placeholder to be updated
			}
			instructions = append(instructions, jumpToEnd)
			jumpToEndIndexes = append(jumpToEndIndexes, len(instructions)-1)
		}
	}

	// Update jump targets for 'Any' conditions
	finalPos := len(instructions)
	for _, index := range jumpToEndIndexes {
		instructions[index].Operands[0] = finalPos - index
	}

	return instructions, anySensorDeps, nil
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

	// Identify equality checks on the same fact that can be combined
	for _, cond := range conditions.All {
		if cond.Operator == "equal" {
			equalityChecks[cond.Fact] = append(equalityChecks[cond.Fact], cond.Value)
		}
	}

	// Generate optimized instructions for combined equality checks
	for fact, values := range equalityChecks {
		if len(values) > 1 {
			optimizedInstructions = append(optimizedInstructions, bytecode.Instruction{
				Opcode:   bytecode.OpLoadFact,
				Operands: []interface{}{fact},
			})
			optimizedInstructions = append(optimizedInstructions, bytecode.Instruction{
				Opcode:   bytecode.OpEqualAny,
				Operands: values,
			})
		}
	}

	// Return optimized instructions if we have any, otherwise return the original instructions
	if len(optimizedInstructions) > 0 {
		return optimizedInstructions, extractSensorDependencies(equalityChecks)
	}
	return nil, nil
}

// extractSensorDependencies extracts sensor dependencies from the optimized conditions
func extractSensorDependencies(equalityChecks map[string][]interface{}) []string {
	var sensors []string
	for sensor := range equalityChecks {
		sensors = append(sensors, sensor)
	}
	return deduplicate(sensors)
}
