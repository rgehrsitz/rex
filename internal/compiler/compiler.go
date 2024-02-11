package compiler

import (
	"encoding/json"
	"os"
	"rgehrsitz/rex/internal/bytecode"
	"rgehrsitz/rex/internal/dependencygraph"
	"rgehrsitz/rex/internal/instructiongen"
	"rgehrsitz/rex/internal/rule"
)

type CompiledRule struct {
	Name               string
	Instructions       []bytecode.Instruction
	RuleDependencies   []string // Names of dependent rules
	SensorDependencies []string // Names of sensors (facts) required for the rule
	Actions            []Action
}

type Action struct {
	Type   string
	Target string
	Value  interface{}
}

// Removed DependencyGraph struct and associated methods.
// This should be moved to an internal package specifically for dependency analysis.
// The rest of the compiler code should use this package to create and work with dependency graphs.

// Removed NewDependencyGraph, AddDependency, and CheckCircularDependency.
// These will now be part of the new internal dependency graph package.

// Removed CompileRule and related condition compiling functions.
// Instead, these functions should be reorganized to focus specifically on compiling individual components of a rule
// such as conditions and actions. The orchestration of compiling a whole rule set will be handled in CompileRuleSet.

// Removed compileCondition, compileAnyConditions, compileNestedCondition, and compileAction.
// These will be restructured into more focused and reusable components within this package.

// Removed getOpcodeForComparison and compileContainsCondition.
// These should be part of a separate internal package for instruction generation.

// Refactored CompileRuleSet to use the new internal packages for dependency analysis and instruction generation.
func CompileRuleSet(rules []rule.Rule) ([]CompiledRule, error) {
	// Build the dependency graph upfront
	graph := dependencygraph.BuildDependencyGraph(rules)

	// Sort rules based on dependency order.
	sortedRuleNames, err := graph.TopologicalSort()
	if err != nil {
		return nil, err // Handle circular dependency error or other sorting issues.
	}

	// Map rule names to rule objects for easier access.
	ruleMap := make(map[string]rule.Rule)
	for _, r := range rules {
		ruleMap[r.Name] = r
	}

	var compiledRules []CompiledRule

	for _, ruleName := range sortedRuleNames {
		r := ruleMap[ruleName]

		var allInstructions []bytecode.Instruction
		var sensorDeps []string

		// Compile the rule's conditions and capture sensor dependencies
		conditionInstructions, condSensorDeps, err := instructiongen.CompileConditions(r.Conditions)
		if err != nil {
			return nil, err
		}
		allInstructions = append(allInstructions, conditionInstructions...)
		sensorDeps = append(sensorDeps, condSensorDeps...)

		// Compile actions and capture action-related dependencies
		for _, action := range r.Event.Actions {
			actionInstructions, actionSensorDeps, err := instructiongen.CompileAction(action)
			if err != nil {
				return nil, err
			}
			allInstructions = append(allInstructions, actionInstructions...)
			sensorDeps = append(sensorDeps, actionSensorDeps...)
		}

		// Deduplicate sensor dependencies
		sensorDeps = deduplicate(sensorDeps)

		compiledRules = append(compiledRules, CompiledRule{
			Name:               r.Name,
			Instructions:       allInstructions,
			RuleDependencies:   graph.DependenciesOf(r.Name), // Directly use the graph to populate rule dependencies.
			SensorDependencies: sensorDeps,                   // Populate sensor dependencies captured during compilation.
		})
	}

	return compiledRules, nil
}

// deduplicate removes duplicate strings from a slice.
func deduplicate(items []string) []string {
	seen := make(map[string]struct{})
	var result []string

	for _, item := range items {
		if _, ok := seen[item]; !ok {
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}

	return result
}

// Removed isValidStoreKey and isValidAddress.
// These should be part of a separate internal package for validation.

// Removed getConsumedFacts and getProducedFacts.
// These should be part of the rule processing logic, possibly within the rule package itself.

// Removed PreprocessRulesFacts and PreprocessDependencies.
// The preprocessing of rules for facts and dependencies should be part of the new dependency analysis package.

// New functions for reading and saving compiled rules.
func ParseRules(data []byte, rules *[]rule.Rule) error {
	return json.Unmarshal(data, rules)
}

func SaveCompiledRules(compiledRules []CompiledRule, filePath string) error {
	data, err := json.Marshal(compiledRules)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}
