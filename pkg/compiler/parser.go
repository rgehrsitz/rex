// rex/pkg/compiler/parser.go

package compiler

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"
)

func Parse(jsonData []byte) (*Ruleset, error) {
	log.Debug().Str("jsonData", string(jsonData)).Msg("Starting to parse JSON data")
	var ruleset Ruleset
	err := json.Unmarshal(jsonData, &ruleset)
	if err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal JSON data")
		return nil, fmt.Errorf("invalid JSON format: %v", err)
	}
	if len(ruleset.Rules) == 0 {
		return nil, errors.New("missing rules field")
	}
	for i, rule := range ruleset.Rules {
		if err := validateRule(&rule); err != nil {
			log.Error().Err(err).Str("rule", rule.Name).Msg("Invalid rule")
			return nil, fmt.Errorf("invalid rule '%s': %v", rule.Name, err)
		}
		ruleset.Rules[i] = rule

		// Add validation for actions
		for j, action := range rule.Actions {
			if err := validateAction(&action); err != nil {
				log.Error().Err(err).Str("rule", rule.Name).Str("action", action.Type).Msg("Invalid action")
				return nil, fmt.Errorf("invalid action '%s' in rule '%s': %v", action.Type, rule.Name, err)
			}
			rule.Actions[j] = action
		}
	}

	//print the parsed ruleset
	log.Debug().Interface("ruleset", ruleset).Msg("Parsed JSON data")
	return &ruleset, nil
}

func validateRule(rule *Rule) error {
	// Basic rule validations
	log.Debug().Str("rule", rule.Name).Msg("Validating rule")
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

func validateAndOrderConditionGroup(cg *ConditionGroup) error {
	// Log for debugging
	log.Debug().Interface("All", cg.All).Interface("Any", cg.Any).Msg("Validating and ordering condition group")
	// Check if the entire group is logically empty
	if len(cg.All) == 0 && len(cg.Any) == 0 {
		log.Error().Msg("Empty condition group detected")
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

func isCondition(cog *ConditionOrGroup) bool {
	return cog.Fact != "" && cog.Operator != "" && cog.Value != nil
}

func validateConditionOrGroup(cog *ConditionOrGroup) error {
	log.Debug().Interface("ConditionOrGroup", cog).Msg("Validating condition or group")
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

func validateAction(action *Action) error {
	if action != nil {
		log.Debug().Str("action", action.Type).Msg("Validating action")
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
	// Return true for now, as there are no specific criteria defined
	return value != nil
}
