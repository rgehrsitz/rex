package optimizer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"rgehrsitz/rex/internal/rule"
	"sort"
)

// ProcessAndOptimizeRuleset takes a slice of rules and returns an optimized version of those rules.
func ProcessAndOptimizeRuleset(rules []rule.Rule) ([]rule.Rule, error) {
	// Validate all rules upfront.
	validatedRules, err := validateRules(rules)
	if err != nil {
		return nil, err // Return errors encountered during validation.
	}

	// Perform optimizations that may involve comparing rules against each other.
	optimizedRules, conflictReports, _ := ApplyOptimizationsAndDetectConflicts(validatedRules)
	if len(conflictReports) > 0 {
		fmt.Println("Conflict reports detected during optimization:")
		for _, report := range conflictReports {
			fmt.Println("- ", report)
		}
	} else {
		fmt.Println("No conflicts detected. Rules optimized successfully.")
	}

	return optimizedRules, nil
}

// validateRules checks all rules for validity and returns a slice of valid rules.
func validateRules(rules []rule.Rule) ([]rule.Rule, error) {
	var validatedRules []rule.Rule
	for _, r := range rules {
		if isValidRule(r) {
			validatedRules = append(validatedRules, r)
		} else {
			// Optionally, return detailed error information about which rule failed validation and why.
			return nil, fmt.Errorf("rule validation failed for rule: %s", r.Name)
		}
	}
	return validatedRules, nil
}

// isValidRule performs comprehensive validation on a single rule.
func isValidRule(r rule.Rule) bool {
	for _, condition := range append(r.Conditions.All, r.Conditions.Any...) {
		if !isValidOperator(condition.Operator) {
			fmt.Printf("Invalid operator '%s' in rule '%s'\n", condition.Operator, r.Name)
			return false
		}

		if !isValidConditionType(condition) {
			fmt.Printf("Invalid condition type for operator '%s' in rule '%s'\n", condition.Operator, r.Name)
			return false
		}
	}
	return true
}

// isValidOperator checks if the provided operator is among the allowed values.
func isValidOperator(op string) bool {
	switch op {
	case "equal", "notEqual", "greaterThan", "greaterThanOrEqual", "lessThan", "lessThanOrEqual", "contains", "notContains":
		return true
	}
	return false
}

// isValidConditionType checks if the condition's value type is appropriate for its operator.
func isValidConditionType(c rule.Condition) bool {
	// Expand this to check the type based on the operator and the ValueType.
	switch c.Operator {
	case "equal", "notEqual":
		// For equality operators, the value can be numerical, string, or boolean.
		_, isString := c.Value.(string)
		_, isFloat := c.Value.(float64)
		_, isInt := c.Value.(int)
		_, isBool := c.Value.(bool)
		return isString || isFloat || isInt || isBool
	case "greaterThan", "greaterThanOrEqual", "lessThan", "lessThanOrEqual":
		// For comparison operators, the value should be numerical.
		_, isFloat := c.Value.(float64)
		_, isInt := c.Value.(int)
		return isFloat || isInt
	case "contains", "notContains":
		// 'Contains' and 'NotContains' might primarily apply to strings or collections, adjust logic as needed.
		_, isString := c.Value.(string)
		return isString
	}
	return false
}

// ApplyOptimizationsAndDetectConflicts applies optimizations and identifies conflicts within the validated ruleset.
func ApplyOptimizationsAndDetectConflicts(rules []rule.Rule) ([]rule.Rule, []string, error) {
	var optimizedRules []rule.Rule
	var conflictReports []string // Collect conflict reports here.

	// Implement deduplication
	ruleMap := make(map[string]rule.Rule)
	for _, r := range rules {
		ruleKey := GenerateDeduplicationKey(r) // You need to implement generateRuleKey
		if _, exists := ruleMap[ruleKey]; !exists {
			ruleMap[ruleKey] = r
			optimizedRules = append(optimizedRules, r)
		} else {
			conflictReports = append(conflictReports, fmt.Sprintf("Duplicate rule detected: %s", r.Name))
		}
	}

	// Further optimization and conflict detection logic...

	return optimizedRules, conflictReports, nil
}

// GenerateDeduplicationKey creates a unique key for each rule based on its content.
func GenerateDeduplicationKey(r rule.Rule) string {
	// Normalize conditions to ensure consistent ordering.
	normalizedConditions := normalizeConditions(r.Conditions)

	// Serialize the rule with normalized conditions for hashing.
	serialized, err := json.Marshal(struct {
		Conditions rule.Conditions `json:"conditions"`
		Actions    []rule.Action   `json:"actions"`
	}{
		Conditions: normalizedConditions,
		Actions:    r.Event.Actions,
	})
	if err != nil {
		// Handle error appropriately.
		return ""
	}

	hash := sha256.New()
	hash.Write(serialized)
	return hex.EncodeToString(hash.Sum(nil))
}

// normalizeConditions ensures consistent ordering of conditions for deduplication.
func normalizeConditions(conds rule.Conditions) rule.Conditions {
	// Normalize and sort the top-level All and Any slices.
	conds.All = normalizeAndSortConditionSlice(conds.All)
	conds.Any = normalizeAndSortConditionSlice(conds.Any)

	return conds
}

// normalizeAndSortConditionSlice normalizes and sorts a slice of Conditions, including any nested conditions.
func normalizeAndSortConditionSlice(conds []rule.Condition) []rule.Condition {
	for i := range conds {
		// Recursively normalize nested conditions.
		if len(conds[i].All) > 0 || len(conds[i].Any) > 0 {
			conds[i].All = normalizeAndSortConditionSlice(conds[i].All)
			conds[i].Any = normalizeAndSortConditionSlice(conds[i].Any)
		}
	}

	// Sort the conditions slice to ensure consistent ordering.
	sort.Slice(conds, func(i, j int) bool {
		// Define sorting logic. For simplicity, use the condition's fact and operator.
		// Adjust the criteria as needed to fit the rule engine's specifics.
		return conds[i].Fact < conds[j].Fact || (conds[i].Fact == conds[j].Fact && conds[i].Operator < conds[j].Operator)
	})

	return conds
}

// MergeAndSimplifyRules optimizes a set of rules by merging similar rules and simplifying conditions.
func MergeAndSimplifyRules(rules []rule.Rule) ([]rule.Rule, error) {
	var optimizedRules []rule.Rule

	// Step 1: Group rules by actions to identify merge candidates.
	actionGroups := groupRulesByActions(rules)

	// Step 2: For each group, attempt to merge rules based on their conditions.
	for _, group := range actionGroups {
		mergedRules := mergeRulesWithinGroup(group)
		optimizedRules = append(optimizedRules, mergedRules...)
	}

	// Step 3: Simplify conditions for each rule.
	for i, r := range optimizedRules {
		optimizedRules[i].Conditions = simplifyConditions(r.Conditions)
	}

	return optimizedRules, nil
}

// groupRulesByActions groups rules that have identical actions.
func groupRulesByActions(rules []rule.Rule) map[string][]rule.Rule {
	groups := make(map[string][]rule.Rule)
	for _, r := range rules {
		actionKey := generateActionKey(r.Event.Actions)
		groups[actionKey] = append(groups[actionKey], r)
	}
	return groups
}

// mergeRulesWithinGroup takes a slice of rules that are candidates for merging (based on identical actions)
// and attempts to merge them into a smaller number of rules by intelligently combining their conditions.
func mergeRulesWithinGroup(group []rule.Rule) []rule.Rule {
	if len(group) <= 1 {
		return group // No merging necessary for a single rule.
	}

	// Initial approach: start with the first rule as the base for merging.
	// This is a simplified strategy; more complex scenarios may require a different approach.
	mergedRule := group[0]

	for _, r := range group[1:] {
		mergedRule.Conditions = mergeConditions(mergedRule.Conditions, r.Conditions)
	}

	return []rule.Rule{mergedRule} // Return a slice containing the single, merged rule.
}

// mergeConditions combines two sets of conditions (each potentially having 'All' and 'Any' sub-conditions)
// into a single set of conditions that preserves the logical intent of both.
func mergeConditions(conds1, conds2 rule.Conditions) rule.Conditions {
	// Merge 'All' conditions by concatenating them.
	mergedAll := append(conds1.All, conds2.All...)

	// Merge 'Any' conditions by concatenating them.
	mergedAny := append(conds1.Any, conds2.Any...)

	// Create a temporary Conditions struct for each to pass to optimizeConditions.
	tempAllConds := rule.Conditions{All: mergedAll}
	tempAnyConds := rule.Conditions{Any: mergedAny}

	// Optimize merged conditions for 'All' and 'Any' separately.
	optimizedAllConds := optimizeConditions(tempAllConds) // Optimizes the 'All' conditions.
	optimizedAnyConds := optimizeConditions(tempAnyConds) // Optimizes the 'Any' conditions.

	// Construct the final Conditions struct with optimized 'All' and 'Any' conditions.
	return rule.Conditions{
		All: optimizedAllConds.All,
		Any: optimizedAnyConds.Any,
	}
}

// optimizeConditions simplifies the conditions of a rule by removing redundancies and optimizing logical structures.
func optimizeConditions(conds rule.Conditions) rule.Conditions {
	optimizedConds := rule.Conditions{
		All: removeRedundantConditions(conds.All),
		Any: removeRedundantConditions(conds.Any),
	}

	// Further optimization could involve flattening nested conditions, combining conditions, etc.
	// This example focuses on removing redundancies.
	return optimizedConds
}

// removeRedundantConditions removes duplicate conditions from a slice of conditions.
func removeRedundantConditions(conditions []rule.Condition) []rule.Condition {
	unique := make([]rule.Condition, 0, len(conditions))
	seen := make(map[string]struct{})

	for _, cond := range conditions {
		// Serialize condition to use as a key for detecting duplicates.
		// This simple serialization assumes that identical conditions will produce identical JSON strings.
		// Adjust serialization as needed to ensure accurate comparison.
		serialized, _ := json.Marshal(cond) // Simplification, handle errors in production code.
		if _, exists := seen[string(serialized)]; !exists {
			seen[string(serialized)] = struct{}{}
			unique = append(unique, cond)
		}
	}

	return unique
}

// Simplifies the conditions of a rule by removing duplicates.
func simplifyConditions(conds rule.Conditions) rule.Conditions {
	simplifiedAll := removeDuplicateConditions(conds.All)
	simplifiedAny := removeDuplicateConditions(conds.Any)

	return rule.Conditions{All: simplifiedAll, Any: simplifiedAny}
}

// Removes duplicate conditions from a slice of conditions.
func removeDuplicateConditions(conditions []rule.Condition) []rule.Condition {
	var unique []rule.Condition
	seen := make(map[string]struct{})

	for _, cond := range conditions {
		key := conditionKey(cond)
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			unique = append(unique, cond)
		}
	}

	return unique
}

// Generates a unique key for a condition based on its Fact, Operator, and Value.
// This key is used to identify duplicate conditions.
func conditionKey(cond rule.Condition) string {
	// Use JSON serialization or a similar method to generate a unique key for the condition.
	// This simplistic approach assumes that identical conditions will produce identical strings.
	serialized, _ := json.Marshal(cond) // Simplification: handle errors in production code.
	return string(serialized)
}

// generateActionKey generates a unique key for a slice of actions to facilitate grouping.
func generateActionKey(actions []rule.Action) string {
	// Simple approach: Concatenate action types and targets. Consider hashing for complex scenarios.
	var key string
	for _, action := range actions {
		key += action.Type + ":" + action.Target + ";"
	}
	return key
}

func DetectConflicts(rules []rule.Rule) ([]string, error) {
	var conflictReports []string

	// Logic to analyze the rule set for potential conflicts.
	// This involves comparing the conditions and actions of rules to identify contradictions or logical conflicts.

	return conflictReports, nil
}

func ValidateRuleSet(rules []rule.Rule) (bool, []string) {
	var validationErrors []string

	// Extended validation logic here.
	// This could involve checking for logical inconsistencies within rules,
	// ensuring that all referenced facts exist, and validating data types and values are appropriate for their usage in conditions and actions.

	isValid := len(validationErrors) == 0
	return isValid, validationErrors
}
