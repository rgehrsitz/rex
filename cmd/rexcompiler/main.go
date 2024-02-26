package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"rgehrsitz/rex/internal/compiler"
	"rgehrsitz/rex/internal/rule"
)

func main() {
	// Parse command-line flags
	rulesFilePath, outputFilePath, verbose := parseFlags()

	// Read rules from the specified JSON file
	rules, err := readRulesFromFile(*rulesFilePath)
	if err != nil {
		exitWithError(fmt.Errorf("error reading rules file: %w", err))
	}

	// Compile the rules using the compiler package
	compiledRules, err := compiler.CompileRuleSet(rules)
	if err != nil {
		exitWithError(fmt.Errorf("error compiling rules: %w", err))
	}

	// If an output file is specified, save the compiled rules
	if *outputFilePath != "" {
		// err := saveCompiledRules(compiledRules, *outputFilePath)
		if err != nil {
			exitWithError(fmt.Errorf("error saving compiled rules: %w", err))
		}
	}

	// If verbose mode is enabled, print the compiled rules
	if *verbose {
		fmt.Printf("Compiled rules: %+v\n", compiledRules)
	}
}

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
	if err = json.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("failed to parse rules: %w", err)
	}

	return rules, nil
}

// func saveCompiledRules(compiledRules []compiler.CompiledRule, filePath string) error {
// 	if err := compiler.SaveCompiledRules(compiledRules, filePath); err != nil {
// 		return fmt.Errorf("failed to save compiled rules: %w", err)
// 	}
// 	return nil
// }

func exitWithError(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
