package main

import (
	"encoding/json"
	"log"
	"os"
	"rgehrsitz/rex/internal/compiler"
	"rgehrsitz/rex/pkg/rule"
)

func main() {
	rules := compileRules("../../data/basic_ruleset.json")
	runREX(rules)
}

func compileRules(filename string) []compiler.CompiledRule {
	// Read the JSON file
	data, err := os.ReadFile(filename)
	if err != nil {
		log.Fatalf("Error reading ruleset file: %v", err)
	}

	// Parse the JSON data into rules
	var rules []rule.Rule
	if err := json.Unmarshal(data, &rules); err != nil {
		log.Fatalf("Error parsing JSON ruleset: %v", err)
	}

	// Compile the rules
	compiledRules, err := compiler.CompileRulesWithDependencies(rules)
	if err != nil {
		log.Fatalf("Error compiling rules: %v", err)
	}

	return compiledRules
}

func runREX(rules []compiler.CompiledRule) {
	// TODO: Create a new instance of the rules engine, pass it the compiled rules, and start it
}
