package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"rgehrsitz/rex/internal/compiler"
	"rgehrsitz/rex/internal/rule"
)

type Rule rule.Rule

func main() {
	// Define command-line arguments
	rulesFilePath := flag.String("rules", "", "Path to the JSON file containing the rules")
	outputFilePath := flag.String("output", "", "Path to save the compiled rules (optional)")
	verbose := flag.Bool("verbose", false, "Enable verbose output")
	flag.Parse()

	// Validate the input
	if *rulesFilePath == "" {
		fmt.Println("Please specify a rules file path")
		os.Exit(1)
	}

	// Read and parse the rules file
	rules, err := ReadAndParseRules(*rulesFilePath)
	if err != nil {
		fmt.Printf("Error reading or parsing rules file: %s\n", err)
		os.Exit(1)
	}

	// Compile the rules with dependencies
	compiledRules, err := compiler.CompileRuleSet(rules)
	if err != nil {
		fmt.Printf("Error compiling rules: %s\n", err)
		os.Exit(1)
	}

	// Optionally save the compiled rules
	if *outputFilePath != "" {
		err := saveCompiledRules(compiledRules, *outputFilePath)
		if err != nil {
			fmt.Printf("Error saving compiled rules: %s\n", err)
			os.Exit(1)
		}
	}

	if *verbose {
		fmt.Printf("Compiled rules: %+v\n", compiledRules)
	}
}

// ReadAndParseRules reads and parses the rules from a JSON file.
// It returns a slice of rules and a map of UUIDs to rule names for debugging.
func ReadAndParseRules(filePath string) ([]rule.Rule, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read rules file '%s': %w", filePath, err)
	}

	var rules []rule.Rule
	err = json.Unmarshal(data, &rules)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON in file '%s': %w", filePath, err)
	}

	return rules, nil
}

// CompileRules compiles the provided rules into a set of compiled rules.
func CompileRules(rules []rule.Rule) ([]compiler.CompiledRule, error) {
	usedNames := make(map[string]struct{})
	var compiled []compiler.CompiledRule
	dependencyGraph := compiler.NewDependencyGraph()

	// Ensure unique rule names
	for _, r := range rules {
		if _, exists := usedNames[r.Name]; exists {
			return nil, fmt.Errorf("duplicate rule name detected: '%s'", r.Name)
		}
		usedNames[r.Name] = struct{}{}
	}

	// Compile each rule
	for _, r := range rules {
		// Adjusted to capture sensorDependencies from the updated CompileRule signature
		instructions, sensorDependencies, err := compiler.CompileRule(&r)
		if err != nil {
			return nil, fmt.Errorf("error compiling rule '%s': %w", r.Name, err)
		}
		// Include sensorDependencies in the compiled rule
		compiledRule := compiler.CompiledRule{
			Name:               r.Name,
			Instructions:       instructions,
			RuleDependencies:   dependencyGraph.Dependencies(r.Name), // Now immediately setting dependencies
			SensorDependencies: sensorDependencies,                   // Include sensor dependencies
		}
		compiled = append(compiled, compiledRule)
	}

	return compiled, nil
}

// saveCompiledRules saves the compiled rules to a file in JSON format
func saveCompiledRules(compiledRules []compiler.CompiledRule, filePath string) error {
	data, err := json.Marshal(compiledRules)
	if err != nil {
		return fmt.Errorf("error marshaling compiled rules to JSON: %w", err)
	}

	err = os.WriteFile(filePath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write compiled rules to file '%s': %w", filePath, err)
	}

	return nil
}
