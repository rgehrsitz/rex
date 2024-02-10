package rulesengine

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"reflect"
	"rgehrsitz/rex/internal/bytecode"
	"rgehrsitz/rex/internal/compiler"
	"rgehrsitz/rex/internal/rule"
	"rgehrsitz/rex/internal/store"
	"strconv"
	"strings"
)

// RulesEngine represents the rules engine instance.
type RulesEngine struct {
	CompiledRules []compiler.CompiledRule
	Store         store.Store
}

// NewRulesEngine creates and returns a new instance of the RulesEngine.
func NewRulesEngine(compiledRules []compiler.CompiledRule, store store.Store) *RulesEngine {
	return &RulesEngine{
		CompiledRules: compiledRules,
		Store:         store,
	}
}

func (re *RulesEngine) StartEvaluationCycle() {
	for _, compiledRule := range re.CompiledRules {
		sensorValues, err := re.Store.GetValues(compiledRule.SensorDependencies)
		if err != nil {
			fmt.Printf("Error fetching sensor values for rule '%s': %v\n", compiledRule.Name, err)
			continue
		}

		satisfied, err := re.EvaluateCompiledRule(compiledRule, sensorValues)
		if err != nil {
			fmt.Printf("Error evaluating compiled rule '%s': %v\n", compiledRule.Name, err)
			continue
		}

		if satisfied {
			re.ExecuteActions(compiledRule.Event.Actions)
		}
	}
}

func (re *RulesEngine) EvaluateCompiledRule(compiledRule compiler.CompiledRule, sensorValues map[string]interface{}) (bool, error) {
	return ExecuteBytecode(compiledRule.Instructions, sensorValues, re.Store)
}

// LoadCompiledRulesFromFile reads compiled rules from a JSON file.
func LoadCompiledRulesFromFile(filePath string) ([]compiler.CompiledRule, error) {
	var compiledRules []compiler.CompiledRule
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(fileData, &compiledRules)
	if err != nil {
		return nil, err
	}
	return compiledRules, nil
}

// ExecuteActions executes the actions associated with a rule's event.
func (re *RulesEngine) ExecuteActions(actions []rule.Action) {
	for _, action := range actions {
		// Execute each action, similar to the existing switch-case logic
		// in the ProcessSensorData method.
	}
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

// ExecuteBytecode evaluates a series of bytecode instructions against pre-fetched sensor values.
func ExecuteBytecode(instructions []bytecode.Instruction, sensorValues map[string]interface{}, store store.Store) (bool, error) {
	var stack []interface{}

	for _, instr := range instructions {
		switch instr.Opcode {
		case bytecode.OpLoadFact:
			factName, ok := instr.Operands[0].(string)
			if !ok {
				return false, fmt.Errorf("OpLoadFact expects a string operand, got %T", instr.Operands[0])
			}
			value, exists := sensorValues[factName]
			if !exists {
				return false, fmt.Errorf("fact %s not found in pre-fetched sensor values", factName)
			}
			stack = append(stack, value)

		case bytecode.OpEqual, bytecode.OpNotEqual, bytecode.OpGreaterThan, bytecode.OpLessThan, bytecode.OpGreaterThanOrEqual, bytecode.OpLessThanOrEqual:
			// Ensure there are enough values in the stack for comparison
			if len(stack) < 2 {
				return false, fmt.Errorf("not enough values in stack for comparison")
			}
			b, a := stack[len(stack)-1], stack[len(stack)-2] // Pop two values from stack
			stack = stack[:len(stack)-2]                     // Reduce stack size
			result, err := executeComparison(instr.Opcode, a, b)
			if err != nil {
				return false, err
			}
			stack = append(stack, result)

		// Handle other opcodes, such as actions and logical operators (AND, OR, NOT)

		default:
			return false, fmt.Errorf("unsupported opcode: %v", instr.Opcode)
		}
	}

	// After executing all instructions, expect a boolean result on the stack
	if len(stack) != 1 {
		return false, fmt.Errorf("expected single boolean result, got stack: %v", stack)
	}
	result, ok := stack[0].(bool)
	if !ok {
		return false, fmt.Errorf("final value in stack is not boolean: %v", stack[0])
	}

	return result, nil
}

func executeComparison(opcode bytecode.Opcode, a, b interface{}) (bool, error) {
	switch opcode {
	case bytecode.OpEqual:
		return reflect.DeepEqual(a, b), nil
	case bytecode.OpNotEqual:
		return !reflect.DeepEqual(a, b), nil
	case bytecode.OpGreaterThan, bytecode.OpLessThan, bytecode.OpGreaterThanOrEqual, bytecode.OpLessThanOrEqual:
		// Convert a and b to float64 for numeric comparison
		aFloat, aErr := toFloat64(a)
		bFloat, bErr := toFloat64(b)
		if aErr != nil || bErr != nil {
			return false, fmt.Errorf("comparison error: %v, %v", aErr, bErr)
		}
		return compareNumeric(opcode, aFloat, bFloat), nil
	// Add other comparison types
	default:
		return false, fmt.Errorf("unsupported comparison opcode: %v", opcode)
	}
}

// compareNumeric performs the numeric comparison based on the opcode.
func compareNumeric(opcode bytecode.Opcode, a, b float64) bool {
	switch opcode {
	case bytecode.OpGreaterThan:
		return a > b
	case bytecode.OpLessThan:
		return a < b
	case bytecode.OpGreaterThanOrEqual:
		return a >= b
	case bytecode.OpLessThanOrEqual:
		return a <= b
	default:
		// This should not happen as unsupported opcodes should be caught earlier.
		return false
	}
}

func handleOpLoadFact(instr bytecode.Instruction, sensorValues map[string]interface{}) (interface{}, error) {
	fact, ok := instr.Operands[0].(string)
	if !ok {
		return nil, fmt.Errorf("invalid operand for OpLoadFact: %v", instr.Operands[0])
	}
	value, exists := sensorValues[fact]
	if !exists {
		return nil, fmt.Errorf("fact %s not found in pre-fetched sensor values", fact)
	}
	return value, nil
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
