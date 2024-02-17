package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"rgehrsitz/rex/cmd/jsonoptimizer/optimizer"
	"rgehrsitz/rex/internal/compiler"
	"rgehrsitz/rex/internal/rule"
	// other imports
)

func main() {
	rulesFilePath, outputFilePath, verbose := parseFlags()

	// Create an instance of the optimizer with the desired verbosity level.

	opt := optimizer.New(*verbose)

	// Read rules from the file.
	rules, err := readRulesFromFile(*rulesFilePath)
	if err != nil {
		exitWithError(fmt.Errorf("error reading rules file: %w", err))
	}

	// Optimize the rules using the optimizer instance.
	optimizedRules, err := opt.OptimizeRules(rules)
	if err != nil {
		exitWithError(fmt.Errorf("error optimizing rules: %w", err))
	}

	// If an output file is specified, save the optimized rules.
	if *outputFilePath != "" {
		err = saveOptimizedRules(optimizedRules, *outputFilePath)
		if err != nil {
			exitWithError(fmt.Errorf("error saving optimized rules: %w", err))
		}
	}

	// Additional logic...
}
func exitWithError(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

// Other functions like parseFlags, readRulesFromFile, saveOptimizedRules, exitWithError...
// And new functions for optimizing rules...
func parseFlags() (*string, *string, *bool) {
	rulesFilePath := flag.String("rules", "", "Path to the JSON file containing the rules")
	outputFilePath := flag.String("output", "", "Path to save the compiled rules (optional)")
	verbose := flag.Bool("verbose", false, "Enable verbose output")
	flag.Parse()

	if *rulesFilePath == "" {
		fmt.Println("Please specify a rules file path using the -rules flag.")
		os.Exit(1)
	}

	return rulesFilePath, outputFilePath, verbose
}

func readRulesFromFile(filePath string) ([]rule.Rule, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read the file '%s': %w", filePath, err)
	}

	var rules []rule.Rule
	if err = compiler.ParseRules(data, &rules); err != nil {
		return nil, fmt.Errorf("failed to parse rules: %w", err)
	}

	return rules, nil
}

func saveOptimizedRules(rules []rule.Rule, filePath string) error {
	data, err := json.Marshal(rules)
	if err != nil {
		return fmt.Errorf("failed to marshal optimized rules: %w", err)
	}

	err = os.WriteFile(filePath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to save optimized rules to file: %w", err)
	}

	return nil
}
