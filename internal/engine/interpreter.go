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

		case bytecode.OpTriggerEvent:
			// Logic to handle the event triggering
			// This could involve calling a function to process the event
			eventType := instr.Operands[0].(string)
			customProperty := instr.Operands[1]
			handleEvent(eventType, customProperty)
		}

		// Add other opcodes as needed

	}

	return conditionMet, nil
}

// handleEvent processes the event triggered by the rule
func handleEvent(eventType string, customProperty interface{}) {
	// Implementation depends on how your system handles events
	// This could involve logging, notifications, etc.
}
