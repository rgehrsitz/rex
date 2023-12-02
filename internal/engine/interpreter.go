package engine

import (
	"rgehrsitz/rex/pkg/bytecode"
	"strings"
)

// ExecuteBytecode executes a sequence of bytecode instructions.
func ExecuteBytecode(instructions []bytecode.Instruction, sensorData map[string]interface{}) (bool, error) {
	var lastLoadedFactValue interface{}
	var conditionMet bool

	for _, instr := range instructions {
		switch instr.Opcode {
		case bytecode.OpLoadFact:
			// Load the fact value from sensorData
			if fact, ok := instr.Operands[0].(string); ok {
				lastLoadedFactValue = sensorData[fact]
			}

		case bytecode.OpEqual:
			// Assume second operand is the value to compare
			conditionMet = lastLoadedFactValue == instr.Operands[0]

		case bytecode.OpContains:
			// Check if the loaded fact value contains the given value
			if value, ok := instr.Operands[0].(string); ok {
				if factValue, ok := lastLoadedFactValue.(string); ok {
					conditionMet = strings.Contains(factValue, value)
				}
			}

		case bytecode.OpNotContains:
			// Check if the loaded fact value does not contain the given value
			if value, ok := instr.Operands[0].(string); ok {
				if factValue, ok := lastLoadedFactValue.(string); ok {
					conditionMet = !strings.Contains(factValue, value)
				}
			}

			// Add other opcodes as needed
		}
	}

	return conditionMet, nil
}
