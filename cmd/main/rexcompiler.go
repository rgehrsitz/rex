package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"rgehrsitz/rex/internal/compiler"
	"rgehrsitz/rex/internal/rule"

	"github.com/google/uuid"
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
	rules, _, err := ReadAndParseRules(*rulesFilePath)
	if err != nil {
		fmt.Printf("Error reading or parsing rules file: %s\\n", err)
		os.Exit(1)
	}

	// Compile the rules
	compiledRules, err := CompileRules(rules)
	if err != nil {
		fmt.Printf("Error compiling rules: %s\\n", err)
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

func ReadAndParseRules(filePath string) ([]Rule, map[uuid.UUID]string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read rules file '%s': %w", filePath, err)
	}

	var rules []Rule
	err = json.Unmarshal(data, &rules)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse JSON in file '%s': %w", filePath, err)
	}

	rulesMap := make(map[uuid.UUID]string)
	for _, r := range rules {
		r.UUID = uuid.New() // Assign a new UUID
		rulesMap[r.UUID] = r.Name
	}

	return rules, rulesMap, nil
}

// compileRules compiles the provided Rex rules into a set of compiled rules with
// executable instructions that can be interpreted at runtime. It iterates through
// each rule, calls the compiler to generate instructions, and builds up the
// compiled rule set. Any compilation errors are returned.
func CompileRules(rules []Rule) ([]compiler.CompiledRule, error) {
	var compiled []compiler.CompiledRule
	for i, r := range rules {
		originalRule := rule.Rule(r)
		instructions, err := compiler.CompileRule(originalRule)
		if err != nil {
			return nil, fmt.Errorf("error compiling rule %d ('%s'): %w", i+1, originalRule.Name, err)
		}
		compiled = append(compiled, compiler.CompiledRule{
			Instructions: instructions,
			Dependencies: []string{}, // Dependencies can be populated if needed
		})
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
