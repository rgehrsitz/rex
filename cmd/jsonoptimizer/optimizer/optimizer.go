package optimizer

import (
	"fmt"
	"rgehrsitz/rex/internal/rule"
	"sort"

	"github.com/schollz/progressbar/v3"
)

// Optimizer contains the settings and state for the optimization process
type Optimizer struct {
	Verbose bool
}

// New creates a new Optimizer instance
func New(verbose bool) *Optimizer {
	return &Optimizer{
		Verbose: verbose,
	}
}

// OptimizeRules iterates over the rules and applies optimizations.
func (o *Optimizer) OptimizeRules(rules []rule.Rule) ([]rule.Rule, error) {
	bar := progressbar.Default(int64(len(rules)), "Optimizing rules")

	for i, r := range rules {
		bar.Describe(fmt.Sprintf("Optimizing rule %d/%d: %s", i+1, len(rules), r.Name))
		optimizedRule := o.optimizeRule(r)
		rules[i] = optimizedRule
		bar.Add(1)
	}

	bar.Finish()
	return rules, nil
}

// optimizeRule handles optimizations at the rule level, potentially calling other optimization functions.
func (o *Optimizer) optimizeRule(r rule.Rule) rule.Rule {
	// Rule-level optimizations could go here, such as simplifying rule structures or removing redundant facts.
	// After that, optimize the conditions within the rule.
	r.Conditions = o.optimizeConditions(r.Conditions)
	return r
}

// optimizeConditions handles optimizations specific to the conditions.
func (o *Optimizer) optimizeConditions(conds rule.Conditions) rule.Conditions {
	// Optimization for 'Any' conditions
	conds.Any = optimizeConditionOrder(conds.Any)
	// Optimization for 'All' conditions
	conds.All = optimizeConditionOrder(conds.All)

	// Recursively optimize nested conditions
	for i, cond := range conds.All {
		cond.All = optimizeConditionOrder(cond.All)
		cond.Any = optimizeConditionOrder(cond.Any)
		conds.All[i] = cond
	}
	for i, cond := range conds.Any {
		cond.All = optimizeConditionOrder(cond.All)
		cond.Any = optimizeConditionOrder(cond.Any)
		conds.Any[i] = cond
	}

	return conds
}

// optimizeConditionOrder reorders conditions based on complexity.
func optimizeConditionOrder(conditions []rule.Condition) []rule.Condition {
	sort.SliceStable(conditions, func(i, j int) bool {
		return conditionComplexity(conditions[i]) < conditionComplexity(conditions[j])
	})
	return conditions
}

// conditionComplexity calculates the complexity of a condition.
func conditionComplexity(cond rule.Condition) int {
	// Insert complexity calculation logic here.
}
