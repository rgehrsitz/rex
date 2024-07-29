// rex/pkg/compiler/parser.go

package compiler

import (
	"encoding/json"
	"fmt"
	"strconv"

	"rgehrsitz/rex/pkg/logging"
)

// Parse parses the provided JSON data and returns a pointer to a Ruleset and an error.
func Parse(jsonData []byte) (*Ruleset, error) {
	var ruleset Ruleset
	err := json.Unmarshal(jsonData, &ruleset)
	if err != nil {
		return nil, logging.NewError(logging.ErrorTypeParse, "Failed to unmarshal JSON data", err, nil)
	}
	if len(ruleset.Rules) == 0 {
		return nil, logging.NewError(logging.ErrorTypeParse, "Missing rules field", nil, nil)
	}
	for i, rule := range ruleset.Rules {
		if err := validateRule(&rule); err != nil {
			return nil, logging.NewError(logging.ErrorTypeCompile, "Invalid rule", err, map[string]interface{}{"rule_name": rule.Name})
		}

		// Validate and compile custom scripts
		for scriptName, script := range rule.Scripts {
			if err := validateAndCompileScript(scriptName, script); err != nil {
				return nil, logging.NewError(logging.ErrorTypeCompile, "Invalid script", err, map[string]interface{}{"rule_name": rule.Name, "script_name": scriptName})
			}
		}

		ruleset.Rules[i] = rule

		for j, action := range rule.Actions {
			if err := validateAction(&action); err != nil {
				return nil, logging.NewError(logging.ErrorTypeCompile, "Invalid action", err, map[string]interface{}{"rule_name": rule.Name, "action_type": action.Type})
			}
			rule.Actions[j] = action
		}
	}

	logging.Logger.Debug().Interface("ruleset", ruleset).Msg("Parsed JSON data")
	return &ruleset, nil
}

// validateRule validates a rule and returns an error if any validation fails.
func validateRule(rule *Rule) error {
	logging.Logger.Debug().Str("rule", rule.Name).Msg("Validating rule")
	if rule.Name == "" {
		return logging.NewError(logging.ErrorTypeCompile, "Rule name is required", nil, nil)
	}
	if rule.Priority < 0 {
		return logging.NewError(logging.ErrorTypeCompile, "Rule priority must be non-negative", nil, map[string]interface{}{"rule_name": rule.Name})
	}
	if err := validateAndOrderConditionGroup(&rule.Conditions); err != nil {
		return logging.NewError(logging.ErrorTypeCompile, "Invalid condition group", err, map[string]interface{}{"rule_name": rule.Name})
	}
	if len(rule.Actions) == 0 {
		return logging.NewError(logging.ErrorTypeCompile, "At least one action is required", nil, map[string]interface{}{"rule_name": rule.Name})
	}
	return nil
}

func validateAndOrderConditionGroup(cg *ConditionGroup) error {
	logging.Logger.Debug().Interface("All", cg.All).Interface("Any", cg.Any).Msg("Validating and ordering condition group")
	if len(cg.All) == 0 && len(cg.Any) == 0 {
		logging.Logger.Error().Msg("Empty condition group detected")
		return logging.NewError(logging.ErrorTypeCompile, "Empty condition group", nil, nil)
	}

	var err error
	cg.All, err = orderConditionsAndGroups(cg.All)
	if err != nil {
		return err
	}

	cg.Any, err = orderConditionsAndGroups(cg.Any)
	if err != nil {
		return err
	}

	return nil
}

// orderConditionsAndGroups orders conditions and nested groups and returns an error if any condition or group is invalid.
// Conditions appear before nested groups.
func orderConditionsAndGroups(cogs []*ConditionOrGroup) ([]*ConditionOrGroup, error) {
	var conditions []*ConditionOrGroup
	var nestedGroups []*ConditionOrGroup

	for _, item := range cogs {
		if isCondition(item) {
			if err := validateConditionOrGroup(item); err != nil {
				return nil, err
			}
			conditions = append(conditions, item)
		} else {
			if err := validateConditionOrGroup(item); err != nil {
				return nil, err
			}
			nestedGroups = append(nestedGroups, item)
		}
	}

	return append(conditions, nestedGroups...), nil
}

// isCondition checks if the given ConditionOrGroup is a valid condition.
func isCondition(cog *ConditionOrGroup) bool {
	return cog.Fact != "" && cog.Operator != "" && cog.Value != nil
}

func validateConditionOrGroup(cog *ConditionOrGroup) error {
	logging.Logger.Debug().Interface("ConditionOrGroup", cog).Msg("Validating condition or group")
	if cog == nil {
		return logging.NewError(logging.ErrorTypeCompile, "Nil condition or group received", nil, nil)
	}

	if len(cog.All) == 0 && len(cog.Any) == 0 {
		if cog.Fact == "" {
			return logging.NewError(logging.ErrorTypeCompile, "Empty or missing fact field", nil, nil)
		} else if !isFactValid(cog.Fact) {
			return logging.NewError(logging.ErrorTypeCompile, "Invalid condition fact", nil, map[string]interface{}{"fact": cog.Fact})
		}

		if cog.Operator == "" {
			return logging.NewError(logging.ErrorTypeCompile, "Empty or missing operator field", nil, nil)
		} else if !isOperatorValid(cog.Operator) {
			return logging.NewError(logging.ErrorTypeCompile, "Invalid condition operator", nil, map[string]interface{}{"operator": cog.Operator})
		}

		if cog.Value == "" {
			return logging.NewError(logging.ErrorTypeCompile, "Invalid condition value", nil, map[string]interface{}{"value": cog.Value})
		} else if !isValueValid(cog.Operator, cog.Value) {
			return logging.NewError(logging.ErrorTypeCompile, "Invalid condition value for operator", nil, map[string]interface{}{"value": cog.Value, "operator": cog.Operator})
		}
	}

	for _, subgroup := range cog.All {
		if err := validateConditionOrGroup(subgroup); err != nil {
			return err
		}
	}

	for _, subgroup := range cog.Any {
		if err := validateConditionOrGroup(subgroup); err != nil {
			return err
		}
	}

	return nil
}

func validateAction(action *Action) error {
	if action != nil {
		logging.Logger.Debug().Str("action", action.Type).Msg("Validating action")
	}
	if action == nil {
		return logging.NewError(logging.ErrorTypeCompile, "Nil action received", nil, nil)
	}
	if action.Type == "" {
		return logging.NewError(logging.ErrorTypeCompile, "Empty or missing type field", nil, nil)
	}
	if action.Target == "" {
		return logging.NewError(logging.ErrorTypeCompile, "Empty or missing target field", nil, nil)
	}
	if !isActionValueValid(action.Type, action.Value) {
		return logging.NewError(logging.ErrorTypeCompile, "Invalid action value for action type", nil, map[string]interface{}{"value": action.Value, "action_type": action.Type})
	}
	return nil
}

func isFactValid(fact string) bool {
	// Placeholder implementation
	// Update this function when the listing of facts is available
	// from spec
	return fact != ""
}

// isOperatorValid checks if the given operator is valid.
func isOperatorValid(operator string) bool {
	validOperators := []string{
		"EQ", "NEQ", "LT", "LTE", "GT", "GTE", "CONTAINS", "NOT_CONTAINS",
	}
	for _, op := range validOperators {
		if op == operator {
			return true
		}
	}
	return false
}

func isValueValid(operator string, value interface{}) bool {
	switch operator {
	case "EQ", "NEQ":
		// EQ and NEQ can have any value type
		return true
	case "LT", "LTE", "GT", "GTE":
		// For these operators, the value must be a number
		return isNumeric(value)
	case "CONTAINS", "NOT_CONTAINS":
		// For these operators, the value must be a string or list
		return isStringOrList(value)
	default:
		return false
	}
}

func isNumeric(value interface{}) bool {
	switch v := value.(type) {
	case float64, float32, int, int64, int32:
		return true
	case string:
		_, err := strconv.ParseFloat(v, 64)
		return err == nil
	default:
		return false
	}
}

func isStringOrList(value interface{}) bool {
	switch value.(type) {
	case string:
		return true
	case []interface{}:
		return true
	default:
		return false
	}
}

func isActionValueValid(actionType string, value interface{}) bool {
	// Placeholder for more complex validation logic based on action type
	switch actionType {
	case "sendMessage", "updateStore":
		switch value.(type) {
		case float64, float32, int, int64, int32:
			return true
		case string:
			return true
		case bool:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func validateAndCompileScript(name string, script Script) error {
	// TODO: Implement script validation and compilation
	// This could involve checking for syntax errors, disallowed operations, etc.
	// For now, we'll just do a basic check on the script body
	if script.Body == "" {
		return fmt.Errorf("script body cannot be empty")
	}
	if name == "" {
		return fmt.Errorf("script name cannot be empty")
	}
	// You might want to add more validation logic here
	return nil
}
