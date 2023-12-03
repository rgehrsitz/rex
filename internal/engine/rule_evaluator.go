package engine

import (
	"fmt"
	"net"
	"reflect"
	"rgehrsitz/rex/internal/store"
	"rgehrsitz/rex/pkg/rule"
	"strconv"
	"strings"
)

// EvaluateRule evaluates a rule against a set of sensor data.
func EvaluateRule(r rule.Rule, sensorData map[string]interface{}) (bool, error) {
	return evaluateConditions(r.Conditions, sensorData)
}

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

// evaluateSingleCondition evaluates a single condition against sensor data.
func evaluateSingleCondition(cond rule.Condition, sensorData map[string]interface{}) (bool, error) {
	if cond.Fact != "" {
		factValue, ok := sensorData[cond.Fact]
		if !ok {
			// Fact not found in sensor data
			return false, nil
		}

		switch cond.Operator {
		case "equal":
			return reflect.DeepEqual(factValue, cond.Value), nil
		case "notEqual":
			return !reflect.DeepEqual(factValue, cond.Value), nil
		case "greaterThan":
			return compareNumbers(factValue, cond.Value, func(a, b float64) bool { return a > b })
		case "greaterThanOrEqual":
			return compareNumbers(factValue, cond.Value, func(a, b float64) bool { return a >= b })
		case "lessThan":
			return compareNumbers(factValue, cond.Value, func(a, b float64) bool { return a < b })
		case "lessThanOrEqual":
			return compareNumbers(factValue, cond.Value, func(a, b float64) bool { return a <= b })
		case "contains":
			factStr, factOk := factValue.(string)
			valueStr, valueOk := cond.Value.(string)
			return factOk && valueOk && strings.Contains(factStr, valueStr), nil
		case "notContains":
			factStr, factOk := factValue.(string)
			valueStr, valueOk := cond.Value.(string)
			return factOk && valueOk && !strings.Contains(factStr, valueStr), nil
		}

		// Handle nested conditions
		if len(cond.All) > 0 || len(cond.Any) > 0 {
			return evaluateConditions(rule.Conditions{All: cond.All, Any: cond.Any}, sensorData)
		}
	}

	return false, nil
}

// compareNumbers compares two numeric values based on a comparison function.
func compareNumbers(factValue, condValue interface{}, compFunc func(a, b float64) bool) (bool, error) {
	factFloat, factOk := toFloat64(factValue)
	condFloat, condOk := toFloat64(condValue)
	if !factOk || !condOk {
		return false, fmt.Errorf("comparison requires numeric types")
	}
	return compFunc(factFloat, condFloat), nil
}

// toFloat64 attempts to convert an interface{} to float64.
func toFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case string:
		if num, err := strconv.ParseFloat(v, 64); err == nil {
			return num, true
		}
	}
	return 0, false
}

// EvaluateRuleWithStore evaluates a rule using data from the specified store.
// It fetches additional sensor data as needed based on the rule's conditions.
func EvaluateRuleWithStore(r rule.Rule, triggeringSensor string, triggeringValue interface{}, s store.Store) error {
	sensorData := map[string]interface{}{triggeringSensor: triggeringValue}
	sensorsToFetch := uniqueSensors(r)

	// Fetch additional required sensor values
	for sensor := range sensorsToFetch {
		if sensor != triggeringSensor {
			data, err := s.GetValue(sensor)
			if err != nil {
				return fmt.Errorf("error fetching data for sensor %s: %w", sensor, err)
			}
			sensorData[sensor] = data
		}
	}

	// Evaluate the rule with the fetched sensor data
	satisfied, err := evaluateConditions(r.Conditions, sensorData)
	if err != nil {
		return fmt.Errorf("error evaluating conditions: %w", err)
	}

	if satisfied {
		switch r.Event.Action.Type {
		case "updateStore":
			if err := s.SetValue(r.Event.Action.Target, r.Event.Action.Value); err != nil {
				return fmt.Errorf("error updating store: %w", err)
			}
		case "sendMessage":
			message, ok := r.Event.Action.Value.(string)
			if !ok {
				return fmt.Errorf("error: action value is not a string")
			}
			if err := sendMessage(r.Event.Action.Target, message); err != nil {
				return fmt.Errorf("error sending message: %w", err)
			}
		default:
			fmt.Println("No action or unknown action type")
		}
	}

	return nil
}

// uniqueSensors returns a set of unique sensor names that need to be fetched.
func uniqueSensors(r rule.Rule) map[string]struct{} {
	sensors := make(map[string]struct{})
	for _, cond := range append(r.Conditions.All, r.Conditions.Any...) {
		sensors[cond.Fact] = struct{}{}
	}
	return sensors
}

func sendMessage(address, message string) error {
	conn, err := net.Dial("udp", address)
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = conn.Write([]byte(message))
	return err
}
