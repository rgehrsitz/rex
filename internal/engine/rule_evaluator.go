// rex/internal/engine/rule_evaluator.go

package engine

import (
	"fmt"
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
func EvaluateRuleWithStore(r rule.Rule, s store.Store) error {
	// Example: Evaluate a rule that requires additional data from Redis
	for _, condition := range r.Conditions.All {
		if data, err := s.GetValue(condition.Fact); err == nil {
			// Evaluate condition with fetched data
			// Use your existing rule evaluation logic here
			fmt.Println(data)
		} else {
			return err // Handle error appropriately
		}
	}

	// Implement similar logic for 'Any' conditions and nested conditions
	// Implement event triggering if the rule conditions are met

	return nil
}
