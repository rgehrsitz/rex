package main

import (
	"flag"
	"fmt"
	"os"
	"rgehrsitz/rex/cmd/jsonoptimizer/optimizer"
	"rgehrsitz/rex/internal/compiler"
	"rgehrsitz/rex/internal/rule"

	"github.com/schollz/progressbar/v3"
	// other imports
)

func main() {
	inputFilePath, outputFilePath, verbose := parseFlags()

	bar := progressbar.Default(-1, "Starting JSON optimization")
	bar.RenderBlank()

	// Read JSON from the input file
	bar.Add(1)
	rules, err := readRulesFromFile(*inputFilePath)
	// Check for errors and handle them...

	// Update progress bar description and increment for each step
	bar.Describe("Validating JSON")
	bar.Add(1)
	// Perform validation...

	// Optimization steps with progress display
	bar.Describe("Optimizing rules")
	bar.Add(1)
	optimizedRules, err := optimizer.OptimizeRules(rules, *verbose)
	// Check for errors and handle them...

	// Save the optimized JSON to the output file
	bar.Describe("Saving optimized JSON")
	bar.Add(1)
	err = saveOptimizedRules(optimizedRules, *outputFilePath)
	// Check for errors and handle them...

	bar.Finish()
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
