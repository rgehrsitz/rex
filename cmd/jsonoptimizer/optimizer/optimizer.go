package optimizer

import (
	"encoding/json"
	"fmt"
	"rgehrsitz/rex/internal/rule"
	"sort"

	"github.com/schollz/progressbar/v3"
)

type Optimizer struct {
	Verbose bool
}

func New(verbose bool) *Optimizer {
	return &Optimizer{
		Verbose: verbose,
	}
}

func (o *Optimizer) OptimizeRules(rules []rule.Rule) ([]rule.Rule, error) {
	bar := progressbar.Default(int64(len(rules)), "Optimizing rules")
	for i := range rules {
		if o.Verbose {
			bar.Describe(fmt.Sprintf("Optimizing rule %d/%d: %s", i+1, len(rules), rules[i].Name))
		}

		o.OptimizeRule(&rules[i])
		bar.Add(1)
	}
	bar.Finish()

	return rules, nil
}

func (o *Optimizer) OptimizeRule(r *rule.Rule) {
	o.eliminateRedundantConditions(&r.Conditions.Any)
	o.eliminateRedundantConditions(&r.Conditions.All)
	// Directly optimize the conditions without attempting to flatten nested groups.
	o.optimizeConditionsGroup(&r.Conditions)
	// Finally, clear any empty conditions to ensure consistency
	clearConditionsIfEmpty(&r.Conditions)
}

// sortConditions sorts conditions based on an estimation function
func sortConditions(conditions []rule.Condition, estimateCostFunc func(rule.Condition) int) {
	sort.SliceStable(conditions, func(i, j int) bool {
		return estimateCostFunc(conditions[i]) < estimateCostFunc(conditions[j])
	})
}

// EstimateConditionCost calculates the cost of a condition in the optimizer.
//
// It takes a condition as a parameter and returns an integer.
func (o *Optimizer) EstimateConditionCost(cond rule.Condition) int {
	baseCost := 0 // Initialize baseCost to 0 for grouping conditions.

	// Only add base cost for conditions with an operator.
	if cond.Operator != "" {
		baseCost = o.getBaseCost(cond)
	}

	nestedCost := 0
	// Recursively add the cost of nested 'All' and 'Any' conditions.
	for _, nestedCond := range cond.All {
		nestedCost += o.EstimateConditionCost(nestedCond)
	}
	for _, nestedCond := range cond.Any {
		nestedCost += o.EstimateConditionCost(nestedCond)
	}

	probabilityAdjustment := o.getProbabilityAdjustment(cond)

	// The total cost is the sum of base cost and nested costs.
	return baseCost + nestedCost + probabilityAdjustment
}

// getBaseCost returns the cost of a given operation.
//
// It takes a condition as a parameter and returns an integer.
func (o *Optimizer) getBaseCost(cond rule.Condition) int {
	var operationCosts = map[string]int{
		"equal":              1,
		"notEqual":           1,
		"greaterThan":        2,
		"greaterThanOrEqual": 2,
		"lessThan":           2,
		"lessThanOrEqual":    2,
		"contains":           3,
		"notContains":        3,
	}

	cost, exists := operationCosts[cond.Operator]
	if !exists {
		cost = 5 // Assign a default cost for unspecified operations
	}
	return cost
}

func (o *Optimizer) getProbabilityAdjustment(cond rule.Condition) int {
	// If you have historical data or heuristics that can inform the likelihood of a condition
	// evaluating to true or false, you can use it to adjust the cost.
	// For example, if a condition is rarely true, it might be given a lower cost if part of an 'Any' block,
	// because it's likely to short-circuit the evaluation of subsequent conditions.
	// Conversely, if a condition is almost always true, it might be given a lower cost if part of an 'All' block.
	// This is a placeholder for such logic.
	return 0 // No adjustment by default
}

func (o *Optimizer) optimizeConditionsGroup(conds *rule.Conditions) {
	// Sort 'Any' and 'All' conditions at the current level
	sortConditions(conds.Any, o.EstimateConditionCost)
	sortConditions(conds.All, o.EstimateConditionCost)

	// Recursively optimize nested 'Any' and 'All' conditions
	for i := range conds.Any {
		// Check if there are nested conditions within 'Any'
		if len(conds.Any[i].All) > 0 || len(conds.Any[i].Any) > 0 {
			nestedConds := &rule.Conditions{All: conds.Any[i].All, Any: conds.Any[i].Any}
			o.optimizeConditionsGroup(nestedConds) // Recurse into nested conditions
			// Update the original condition with optimized nested conditions
			conds.Any[i].All = nestedConds.All
			conds.Any[i].Any = nestedConds.Any
		}
	}

	for i := range conds.All {
		// Check if there are nested conditions within 'All'
		if len(conds.All[i].All) > 0 || len(conds.All[i].Any) > 0 {
			nestedConds := &rule.Conditions{All: conds.All[i].All, Any: conds.All[i].Any}
			o.optimizeConditionsGroup(nestedConds) // Recurse into nested conditions
			// Update the original condition with optimized nested conditions
			conds.All[i].All = nestedConds.All
			conds.All[i].Any = nestedConds.Any
		}
	}
}

func (o *Optimizer) eliminateRedundantConditions(conditions *[]rule.Condition) {
	uniqueConditions := []rule.Condition{}
	seen := make(map[string]bool)

	for _, cond := range *conditions {
		// Serialize the condition to a string that uniquely represents it
		serializedCond, err := json.Marshal(cond)
		if err != nil {
			// Handle error, maybe log it or decide how to proceed
			continue // For simplicity, just skip this condition
		}
		key := string(serializedCond)

		// Check if we've already seen this condition
		if !seen[key] {
			uniqueConditions = append(uniqueConditions, cond)
			seen[key] = true
		}
	}

	*conditions = uniqueConditions
}

func clearConditionsIfEmpty(conds *rule.Conditions) {
	if len(conds.Any) == 0 {
		conds.Any = nil // Ensure an empty 'Any' slice is nil
	}
	if len(conds.All) == 0 {
		conds.All = nil // Ensure an empty 'All' slice is nil
	}
}
