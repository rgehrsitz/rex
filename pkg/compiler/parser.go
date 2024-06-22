// rex/pkg/compiler/parser.go

package compiler

import (
	"encoding/json"
	"errors"
	"fmt"

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
// It checks if the entire group is logically empty and separates conditions
// and nested groups. It recursively validates and orders nested groups.
// Finally, it reassigns the ordered conditions and nested groups to the
// original condition group.
func validateAndOrderConditionGroup(cg *ConditionGroup) error {
	// Log for debugging
	logging.Logger.Debug().Interface("All", cg.All).Interface("Any", cg.Any).Msg("Validating and ordering condition group")
	// Check if the entire group is logically empty
	if len(cg.All) == 0 && len(cg.Any) == 0 {
		logging.Logger.Error().Msg("Empty condition group detected")
		return errors.New("empty condition group")
	}

	// Separate conditions and nested groups
	var conditions []*ConditionOrGroup
	var nestedGroups []*ConditionOrGroup

	for _, item := range cg.All {
		if isCondition(item) {
			if err := validateConditionOrGroup(item); err != nil {
				return err
			}
			conditions = append(conditions, item)
		} else {
			nestedGroups = append(nestedGroups, item)
		}
	}

	// Recursively validate and order nested groups
	for _, subgroup := range nestedGroups {
		if err := validateConditionOrGroup(subgroup); err != nil {
			return err
		}
	}

	// Reassign ordered conditions and nested groups
	cg.All = append(conditions, nestedGroups...)

	return nil
}

// isCondition checks if the given ConditionOrGroup is a valid condition.
// This is just a plceholder for additional validation logic.
func isCondition(cog *ConditionOrGroup) bool {
	return cog.Fact != "" && cog.Operator != "" && cog.Value != nil
}

// validateConditionOrGroup validates a ConditionOrGroup object.
// It checks if the object is nil, and if not, it validates the fact, operator, and value fields.
// If the fact, operator, or value fields are missing or invalid, an error is returned.
// If the object contains nested groups, it recursively validates each subgroup.
// Returns nil if the object is valid, otherwise returns an error.
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
		} else if !isValueValid(cog.Value) {
			return fmt.Errorf("invalid condition value '%v'", cog.Value)
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
// It checks if the action is nil, if the type field is empty or missing,
// if the target field is empty or missing, and if the value field is empty or missing.
// If any of these conditions are met, it returns an error.
// Otherwise, it returns nil.
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
	if action.Value == "" {
		return errors.New("empty or missing value field")
	}
	return nil
}

func isFactValid(fact string) bool {
	// Placeholder implementation
	// Update this function when the listing of facts is available
	return fact != ""
}

// isOperatorValid checks if the given operator is valid.
// It returns true if the operator is valid, otherwise false.
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

func isValueValid(value interface{}) bool {
	// Placeholder implementation
	// Return true for now, as there are no specific criteria defined yet
	return value != nil
}
