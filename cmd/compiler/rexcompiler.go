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
	rules, err := readAndParseRules(*rulesFilePath)
	if err != nil {
		fmt.Printf("Error reading or parsing rules file: %s\\n", err)
		os.Exit(1)
	}

	// Compile the rules
	compiledRules, err := compileRules(rules)
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

func readAndParseRules(filePath string) ([]Rule, error) {
	// Read the file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Parse the JSON data
	var rules []Rule
	err = json.Unmarshal(data, &rules)
	if err != nil {
		return nil, err
	}

	return rules, nil
}

// compileRules uses the CompileRule function from the compiler package
func compileRules(rules []Rule) ([]compiler.CompiledRule, error) {
	var compiled []compiler.CompiledRule
	for _, r := range rules {
		// Convert 'Rule' (alias) to 'rule.Rule' (original type) before passing it to CompileRule
		originalRule := rule.Rule(r)
		instructions, err := compiler.CompileRule(originalRule)
		if err != nil {
			return nil, err
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
	// Convert the compiled rules into JSON
	data, err := json.Marshal(compiledRules)
	if err != nil {
		return fmt.Errorf("error marshaling compiled rules to JSON: %s", err)
	}

	// Write the JSON data to the specified file
	err = os.WriteFile(filePath, data, 0644) // 0644 permissions allow read/write for user and read for others
	if err != nil {
		return fmt.Errorf("error writing compiled rules to file: %s", err)
	}

	return nil
}
