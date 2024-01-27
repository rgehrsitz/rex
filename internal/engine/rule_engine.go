package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"reflect"
	"rgehrsitz/rex/internal/bytecode"
	"rgehrsitz/rex/internal/rule"
	"rgehrsitz/rex/internal/store"
	"strconv"
	"strings"
)

// Define the type for event handler functions
type EventHandlerFunc func(customProperty interface{}, store store.Store) error

// EventHandlers map to store the event handling logic for different event types
var EventHandlers = map[string]EventHandlerFunc{
	"updateSensor": handleUpdateSensorEvent,
	// Add other event types and their handlers here
}

// RulesEngine represents the rules engine instance.
type RulesEngine struct {
	CompiledRules []rule.Rule
	Store         store.Store
}

// NewRulesEngine creates and returns a new instance of the RulesEngine.
func NewRulesEngine(compiledRules []rule.Rule, store store.Store) *RulesEngine {
	return &RulesEngine{
		CompiledRules: compiledRules,
		Store:         store,
	}
}

// ProcessSensorData processes the incoming sensor data against the loaded rules.
func (re *RulesEngine) ProcessSensorData(sensorKey string, data interface{}) {
	// Convert data to a suitable format if necessary
	//sensorData := map[string]interface{}{sensorKey: data}

	for _, rule := range re.CompiledRules {
		// Evaluate the rule against the sensor data
		// This might involve checking if the sensorKey is relevant for the rule,
		// and if so, applying the rule logic to the data.
		// For example:
		satisfied, err := EvaluateRuleWithStore(rule, sensorKey, data, re.Store)
		if err != nil {
			// Handle error
			log.Printf("Error evaluating rule: %v", err)
			continue
		}

		if satisfied {
			// Handle the actions defined in the rule's event
			for _, action := range rule.Event.Actions {
				switch action.Type {
				case "updateStore":
					// Assuming action.Target is the key and action.Value is the value for the store
					if err := re.Store.SetValue(action.Target, action.Value); err != nil {
						log.Printf("Error updating store for key %s: %v", action.Target, err)
					}

				case "sendMessage":
					// Assuming action.Target is the address and action.Value is the message content
					if value, ok := action.Value.(string); ok {
						if err := sendMessage(action.Target, value); err != nil {
							log.Printf("Error sending message to %s: %v", action.Target, err)
						}
					} else {
						log.Printf("Error: action.Value is not a string")
					}

				// Add cases for other action types as needed

				default:
					log.Printf("Unknown action type: %s", action.Type)
				}
			}
		}
	}
}

func LoadRulesFromFile(filePath string) ([]rule.Rule, error) {
	var rules []rule.Rule
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(fileData, &rules)
	if err != nil {
		return nil, err
	}
	return rules, nil
}

// EvaluateRule evaluates a rule against a set of sensor data.
func EvaluateRule(r rule.Rule, sensorData map[string]interface{}) (bool, error) {
	return evaluateConditions(r.Conditions, sensorData)
}

// EvaluateRuleWithStore evaluates a rule using data from the specified store.
// It fetches additional sensor data as needed based on the rule's conditions.
func EvaluateRuleWithStore(r rule.Rule, triggeringSensor string, triggeringValue interface{}, s store.Store) (bool, error) {
	sensorData := map[string]interface{}{triggeringSensor: triggeringValue}
	sensorsToFetch := uniqueSensors(r)

	// Fetch additional required sensor values
	err := fetchSensorData(s, sensorsToFetch, sensorData)
	if err != nil {
		return false, err
	}

	// Evaluate the rule with the fetched sensor data
	satisfied, evalErr := evaluateConditions(r.Conditions, sensorData)
	if evalErr != nil {
		return false, fmt.Errorf("error evaluating conditions: %w", evalErr)
	}

	if satisfied {
		// Handle actions based on the rule's event
		// You might want to perform some action here if the rule is satisfied.
		// For now, we return true and no error.
		return true, nil
	}

	return false, nil
}

// fetchSensorData fetches and updates sensor data for given sensors from the store.
func fetchSensorData(s store.Store, sensors map[string]struct{}, sensorData map[string]interface{}) error {
	for sensor := range sensors {
		if _, exists := sensorData[sensor]; !exists {
			data, err := s.GetValue(sensor)
			if err != nil {
				return fmt.Errorf("error fetching data for sensor %s: %w", sensor, err)
			}
			sensorData[sensor] = data
		}
	}
	return nil
}

// handleRuleEvent handles the actions based on the rule's event.
// func handleRuleEvent(event rule.Event, s store.Store) error {
// 	for _, action := range event.Actions {
// 		switch action.Type {
// 		case "updateStore":
// 			if err := s.SetValue(action.Target, action.Value); err != nil {
// 				return fmt.Errorf("error updating store: %w", err)
// 			}
// 		case "sendMessage":
// 			message, ok := action.Value.(string)
// 			if !ok {
// 				return fmt.Errorf("error: action value is not a string")
// 			}
// 			if err := sendMessage(action.Target, message); err != nil {
// 				return fmt.Errorf("error sending message: %w", err)
// 			}
// 		default:
// 			fmt.Println("No action or unknown action type")
// 		}
// 	}
// 	return nil
// }

// evaluateConditions evaluates a set of conditions (All and Any) against sensor data.
func evaluateConditions(conditions rule.Conditions, sensorData map[string]interface{}) (bool, error) {
	// Evaluate 'All' conditions (logical AND)
	for _, cond := range conditions.All {
		satisfied, err := evaluateSingleCondition(cond, sensorData)
		if err != nil {
			return false, err
		}
		if !satisfied {
			return false, nil
		}
	}

	// If there are no 'Any' conditions, and all 'All' conditions are satisfied, return true.
	if len(conditions.Any) == 0 {
		return true, nil
	}

	// Evaluate 'Any' conditions (logical OR)
	anySatisfied := false
	for _, cond := range conditions.Any {
		satisfied, err := evaluateSingleCondition(cond, sensorData)
		if err != nil {
			return false, err
		}
		if satisfied {
			anySatisfied = true
			break
		}
	}

	return anySatisfied, nil
}

func evaluateSingleCondition(cond rule.Condition, sensorData map[string]interface{}) (bool, error) {
	if cond.Fact != "" {
		factValue, exists := sensorData[cond.Fact]
		if !exists {
			return false, nil // Fact not found in sensor data
		}

		switch cond.Operator {
		case "equal":
			return reflect.DeepEqual(factValue, cond.Value), nil
		case "notEqual":
			return !reflect.DeepEqual(factValue, cond.Value), nil
		case "greaterThan":
			return isGreaterThan(factValue, cond.Value)
		case "greaterThanOrEqual":
			return isGreaterThanOrEqual(factValue, cond.Value)
		case "lessThan":
			return isLessThan(factValue, cond.Value)
		case "lessThanOrEqual":
			return isLessThanOrEqual(factValue, cond.Value)
		case "contains":
			return contains(factValue, cond.Value)
		case "notContains":
			return notContains(factValue, cond.Value)
		default:
			return false, fmt.Errorf("unsupported operator: %s", cond.Operator)
		}
	} else if len(cond.All) > 0 || len(cond.Any) > 0 {
		return evaluateConditions(rule.Conditions{All: cond.All, Any: cond.Any}, sensorData)
	}

	return false, nil
}

func isGreaterThan(factValue, condValue interface{}) (bool, error) {
	return compareNumbers(factValue, condValue, func(a, b float64) bool { return a > b })
}

func isGreaterThanOrEqual(factValue, condValue interface{}) (bool, error) {
	return compareNumbers(factValue, condValue, func(a, b float64) bool { return a >= b })
}

func isLessThan(factValue, condValue interface{}) (bool, error) {
	return compareNumbers(factValue, condValue, func(a, b float64) bool { return a < b })
}

func isLessThanOrEqual(factValue, condValue interface{}) (bool, error) {
	return compareNumbers(factValue, condValue, func(a, b float64) bool { return a <= b })
}

func contains(factValue, condValue interface{}) (bool, error) {
	factStr, factOk := factValue.(string)
	valueStr, valueOk := condValue.(string)
	if !factOk || !valueOk {
		return false, fmt.Errorf("both fact and condition value must be strings for 'contains'")
	}
	return strings.Contains(factStr, valueStr), nil
}

func notContains(factValue, condValue interface{}) (bool, error) {
	factStr, factOk := factValue.(string)
	valueStr, valueOk := condValue.(string)
	if !factOk || !valueOk {
		return false, fmt.Errorf("both fact and condition value must be strings for 'notContains'")
	}
	return !strings.Contains(factStr, valueStr), nil
}

func compareNumbers(factValue, condValue interface{}, compFunc func(a, b float64) bool) (bool, error) {
	factFloat, err := toFloat64(factValue)
	if err != nil {
		return false, fmt.Errorf("error converting fact value to float64: %w", err)
	}

	condFloat, err := toFloat64(condValue)
	if err != nil {
		return false, fmt.Errorf("error converting condition value to float64: %w", err)
	}

	return compFunc(factFloat, condFloat), nil
}

func toFloat64(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		num, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to convert string '%s' to float64: %w", v, err)
		}
		return num, nil
	default:
		return 0, fmt.Errorf("unsupported type %T for conversion to float64", value)
	}
}

func ExecuteBytecode(instructions []bytecode.Instruction, sensorData map[string]interface{}, store store.Store) (bool, error) {
	var lastLoadedFactValue interface{}
	var conditionMet bool

	for _, instr := range instructions {
		var err error
		switch instr.Opcode {
		case bytecode.OpLoadFact:
			lastLoadedFactValue, err = handleOpLoadFact(instr, sensorData)
		case bytecode.OpEqual:
			conditionMet, err = handleOpEqual(instr, lastLoadedFactValue)
		case bytecode.OpContains:
			conditionMet, err = handleOpContains(instr, lastLoadedFactValue)
		case bytecode.OpNotContains:
			conditionMet, err = handleOpNotContains(instr, lastLoadedFactValue)
		case bytecode.OpUpdateStore:
			err = handleOpUpdateStore(instr, store)
		case bytecode.OpSendMessage:
			err = handleOpSendMessage(instr)
		}

		if err != nil {
			return false, err
		}
	}

	return conditionMet, nil
}

func handleOpLoadFact(instr bytecode.Instruction, sensorData map[string]interface{}) (interface{}, error) {
	if fact, ok := instr.Operands[0].(string); ok {
		return sensorData[fact], nil
	}
	return nil, fmt.Errorf("invalid operand for OpLoadFact")
}

func handleOpEqual(instr bytecode.Instruction, lastLoadedFactValue interface{}) (bool, error) {
	// Assume second operand is the value to compare
	return lastLoadedFactValue == instr.Operands[0], nil
}

func handleOpContains(instr bytecode.Instruction, lastLoadedFactValue interface{}) (bool, error) {
	if value, ok := instr.Operands[0].(string); ok {
		if factValue, ok := lastLoadedFactValue.(string); ok {
			return strings.Contains(factValue, value), nil
		}
	}
	return false, fmt.Errorf("invalid operands for OpContains")
}

func handleOpNotContains(instr bytecode.Instruction, lastLoadedFactValue interface{}) (bool, error) {
	if value, ok := instr.Operands[0].(string); ok {
		if factValue, ok := lastLoadedFactValue.(string); ok {
			return !strings.Contains(factValue, value), nil
		}
	}
	return false, fmt.Errorf("invalid operands for OpNotContains")
}

func handleOpUpdateStore(instr bytecode.Instruction, store store.Store) error {
	if len(instr.Operands) != 2 {
		return fmt.Errorf("invalid number of operands for OpUpdateStore")
	}
	target, ok := instr.Operands[0].(string)
	if !ok {
		return fmt.Errorf("invalid target operand for OpUpdateStore")
	}
	value := instr.Operands[1] // Value is already interface{}
	return store.SetValue(target, value)
}

func handleOpSendMessage(instr bytecode.Instruction) error {
	if len(instr.Operands) != 2 {
		return fmt.Errorf("invalid number of operands for OpSendMessage")
	}
	address, ok1 := instr.Operands[0].(string)
	message, ok2 := instr.Operands[1].(string)
	if !ok1 || !ok2 {
		return fmt.Errorf("invalid operands for OpSendMessage")
	}
	return sendMessage(address, message)
}

// handleUpdateSensorEvent handles the "updateSensor" event type
func handleUpdateSensorEvent(customProperty interface{}, store store.Store) error {
	if sensorID, ok := customProperty.(string); ok {
		newValue := calculateNewSensorValue(sensorID)
		return store.SetValue(sensorID, newValue)
	}
	return fmt.Errorf("invalid custom property for updateSensor event")
}

// calculateNewSensorValue - Placeholder function for calculating a new sensor value
// Replace this with actual logic
func calculateNewSensorValue(sensorID string) interface{} {
	// Example logic
	return "new value"
}

func sendMessage(address, message string) error {
	conn, err := net.Dial("udp", address)
	if err != nil {
		return fmt.Errorf("error dialing UDP address %s: %w", address, err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte(message))
	if err != nil {
		return fmt.Errorf("error sending message to %s: %w", address, err)
	}
	return nil
}

func uniqueSensors(r rule.Rule) map[string]struct{} {
	sensors := make(map[string]struct{})

	// Function to add a sensor to the map if it's not already present
	addSensor := func(sensor string) {
		if _, exists := sensors[sensor]; !exists {
			sensors[sensor] = struct{}{}
		}
	}

	// Loop through both 'All' and 'Any' conditions
	for _, cond := range append(r.Conditions.All, r.Conditions.Any...) {
		addSensor(cond.Fact)
	}

	return sensors
}

// ExtractSensorKeys returns a slice of unique sensor keys required by the rules
func (re *RulesEngine) ExtractSensorKeys() []string {
	keyMap := make(map[string]struct{})
	for _, r := range re.CompiledRules {
		extractKeysFromConditions(r.Conditions.All, keyMap)
		extractKeysFromConditions(r.Conditions.Any, keyMap)
	}

	var keys []string
	for key := range keyMap {
		keys = append(keys, key)
	}
	return keys
}

// extractKeysFromConditions extracts sensor keys from a slice of Condition and adds them to the provided map.
func extractKeysFromConditions(conditions []rule.Condition, keyMap map[string]struct{}) {
	for _, cond := range conditions {
		if cond.Fact != "" {
			keyMap[cond.Fact] = struct{}{}
		}
		extractKeysFromConditions(cond.All, keyMap)
		extractKeysFromConditions(cond.Any, keyMap)
	}
}
