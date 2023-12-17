package engine

import (
	"log"
	"rgehrsitz/rex/internal/store"
	"rgehrsitz/rex/pkg/bytecode"
	"strings"
)

// ExecuteBytecode executes a sequence of bytecode instructions.
func ExecuteBytecode(instructions []bytecode.Instruction, sensorData map[string]interface{}, store store.Store) (bool, error) {
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
			handleEvent(eventType, customProperty, store)
		}

		// Add other opcodes as needed

	}

	return conditionMet, nil
}

// handleEvent processes the event triggered by the rule
func handleEvent(eventType string, customProperty interface{}, store store.Store) {
	// Example: Log the event
	log.Printf("Event Triggered: %s, Property: %v\n", eventType, customProperty)

	// Implement additional logic based on eventType
	// Example: Update a value in the key/value store
	if eventType == "updateSensor" {
		if sensorID, ok := customProperty.(string); ok {
			// Update sensor value logic
			newValue := calculateNewSensorValue(sensorID)
			store.SetValue(sensorID, newValue)
		}
	}

	// Add other event types and their handling logic here
}

// Placeholder function for calculating a new sensor value
// Replace this with actual logic
func calculateNewSensorValue(sensorID string) interface{} {
	// Example logic
	return "new value"
}
