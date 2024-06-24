// rex/pkg/compiler/parser.go

package compiler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"rgehrsitz/rex/pkg/logging"
)

// Parse parses the provided JSON data and returns a pointer to a Ruleset and an error.
func Parse(jsonData []byte) (*Ruleset, error) {
	logging.Logger.Debug().Str("jsonData", string(jsonData)).Msg("Starting to parse JSON data")
	var ruleset Ruleset
	err := json.Unmarshal(jsonData, &ruleset)
	if err != nil {
		logging.Logger.Error().Err(err).Msg("Failed to unmarshal JSON data")
		return nil, fmt.Errorf("invalid JSON format: %v", err)
	}
	if len(ruleset.Rules) == 0 {
		return nil, errors.New("missing rules field")
	}
	for i, rule := range ruleset.Rules {
		if err := validateRule(&rule); err != nil {
			logging.Logger.Error().Err(err).Str("rule", rule.Name).Msg("Invalid rule")
			return nil, fmt.Errorf("invalid rule '%s': %v", rule.Name, err)
		}
		ruleset.Rules[i] = rule

		// Add validation for actions
		for j, action := range rule.Actions {
			if err := validateAction(&action); err != nil {
				logging.Logger.Error().Err(err).Str("rule", rule.Name).Str("action", action.Type).Msg("Invalid action")
				return nil, fmt.Errorf("invalid action '%s' in rule '%s': %v", action.Type, rule.Name, err)
			}
			rule.Actions[j] = action
		}
	}

	//print the parsed ruleset
	logging.Logger.Debug().Interface("ruleset", ruleset).Msg("Parsed JSON data")
	return &ruleset, nil
}

// validateRule validates a rule and returns an error if any validation fails.
func validateRule(rule *Rule) error {
	// Basic rule validations
	logging.Logger.Debug().Str("rule", rule.Name).Msg("Validating rule")
	if rule.Name == "" {
		return errors.New("rule name is required")
	}
	if rule.Priority < 0 {
		return errors.New("rule priority must be non-negative")
	}
	// Start group validation and reordering
	if err := validateAndOrderConditionGroup(&rule.Conditions); err != nil {
		return fmt.Errorf("invalid condition group: %v", err)
	}
	// Actions validation remains the same
	if len(rule.Actions) == 0 {
		return errors.New("at least one action is required")
	}
	// Fact validations remain the same
	return nil
}

// validateAndOrderConditionGroup validates and orders a condition group.
func validateAndOrderConditionGroup(cg *ConditionGroup) error {
	// Log for debugging
	logging.Logger.Debug().Interface("All", cg.All).Interface("Any", cg.Any).Msg("Validating and ordering condition group")
	// Check if the entire group is logically empty
	if len(cg.All) == 0 && len(cg.Any) == 0 {
		logging.Logger.Error().Msg("Empty condition group detected")
		return errors.New("empty condition group")
	}

	// Validate and order the conditions and nested groups
	orderedAll, err := orderConditionsAndGroups(cg.All)
	if err != nil {
		return err
	}
	cg.All = orderedAll

	orderedAny, err := orderConditionsAndGroups(cg.Any)
	if err != nil {
		return err
	}
	cg.Any = orderedAny

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

	// Return ordered list with conditions first, followed by nested groups
	return append(conditions, nestedGroups...), nil
}

// isCondition checks if the given ConditionOrGroup is a valid condition.
func isCondition(cog *ConditionOrGroup) bool {
	return cog.Fact != "" && cog.Operator != "" && cog.Value != nil
}

// validateConditionOrGroup validates a ConditionOrGroup object.
func validateConditionOrGroup(cog *ConditionOrGroup) error {
	logging.Logger.Debug().Interface("ConditionOrGroup", cog).Msg("Validating condition or group")
	if cog == nil {
		return errors.New("nil condition or group received")
	}

	if len(cog.All) == 0 && len(cog.Any) == 0 {
		if cog.Fact == "" {
			return errors.New("empty or missing fact field")
		} else if !isFactValid(cog.Fact) {
			return fmt.Errorf("invalid condition fact '%s'", cog.Fact)
		}

		if cog.Operator == "" {
			return errors.New("empty or missing operator field")
		} else if !isOperatorValid(cog.Operator) {
			return fmt.Errorf("invalid condition operator '%s'", cog.Operator)
		}

		if cog.Value == "" {
			return fmt.Errorf("invalid condition value '%v'", cog.Value)
		} else if !isValueValid(cog.Operator, cog.Value) {
			return fmt.Errorf("invalid condition value '%v' for operator '%s'", cog.Value, cog.Operator)
		}
	}

	// Otherwise, validate as a group
	// Validate 'All' nested groups
	for _, subgroup := range cog.All {
		if err := validateConditionOrGroup(subgroup); err != nil {
			return err
		}
	}

	// Validate 'Any' nested groups
	for _, subgroup := range cog.Any {
		if err := validateConditionOrGroup(subgroup); err != nil {
			return err
		}
	}

	return nil
}

// validateAction validates the given action.
func validateAction(action *Action) error {
	if action != nil {
		logging.Logger.Debug().Str("action", action.Type).Msg("Validating action")
	}
	if action == nil {
		return errors.New("nil action received")
	}
	if action.Type == "" {
		return errors.New("empty or missing type field")
	}
	// Add checks for valid action types, targets, and values
	if action.Target == "" {
		return errors.New("empty or missing target field")
	}
	if !isActionValueValid(action.Type, action.Value) {
		return fmt.Errorf("invalid action value '%v' for action type '%s'", action.Value, action.Type)
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
