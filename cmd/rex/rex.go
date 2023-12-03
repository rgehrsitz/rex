package main

import (
	"log"
	"rgehrsitz/rex/internal/engine"
)

func main() {
	// Example usage
	ruleFilePath := "../../data/rules.json"
	rules, err := engine.LoadRulesFromFile(ruleFilePath)
	if err != nil {
		log.Fatalf("Failed to read or parse rules: %v", err)
	}

	log.Printf("Successfully read and parsed %d rules", len(rules))
	// Enhanced AddRule or a similar compiler function
	for _, rule := range rules {
		//	bytecode, err := compiler.CompileRule(rule)
		if err != nil {
			log.Printf("Failed to compile rule %s: %v", rule.Name, err)
			continue
		}

		// Store or use bytecode...
	}

	// Create a new Redis store instance
	/* 	redisStore := redis.NewRedisStore(&redis.Options{
	   		Addr:     "localhost:6379", // Production Redis server address
	   		Password: "",               // Password, if any
	   		DB:       0,                // DB number
	   	})

	   	// Define a sample rule (adjust as per your rule format)
	   	sampleRule := rule.Rule{
	   		// Rule definition here
	   	}

	   	// Evaluate the rule with Redis store
	   	if err := engine.EvaluateRuleWithStore(sampleRule, redisStore); err != nil {
	   		// Handle error
	   	} */
}
