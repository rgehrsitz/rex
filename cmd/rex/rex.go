package main

import (
	"encoding/json"
	"log"
	"os"
	"rgehrsitz/rex/internal/compiler"
	"rgehrsitz/rex/pkg/rule"
)

func main() {
	// Read the JSON file
	data, err := os.ReadFile("ruleset.json")
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

	// TODO: Pass compiledRules to the rules engine and start it
}
