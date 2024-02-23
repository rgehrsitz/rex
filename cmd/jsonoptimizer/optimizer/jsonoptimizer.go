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
