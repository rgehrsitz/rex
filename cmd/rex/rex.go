package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"rgehrsitz/rex/internal/compiler"
	"rgehrsitz/rex/internal/engine"
	"rgehrsitz/rex/internal/rule"
	"rgehrsitz/rex/internal/store"

	"github.com/redis/go-redis/v9"
)

func main() {
	// Load compiled rules from a file
	rulesFilePath := "path_to_compiled_rules.json" // Replace with the actual path
	rules, err := engine.LoadRulesFromFile(rulesFilePath)
	if err != nil {
		fmt.Printf("Error loading rules: %v\n", err)
		os.Exit(1)
	}

	// Initialize Redis store
	redisOptions := &redis.Options{
		Addr: "localhost:6379", // Replace with actual Redis server address
		// Other options as needed
	}
	redisStore := store.NewRedisStore(redisOptions)

	// Initialize the rules engine with the loaded rules
	rulesEngine := engine.NewRulesEngine(rules, redisStore)

	// Subscribe to sensor updates
	subscribeToSensorUpdates(redisStore, rulesEngine)

	// The application remains running to process incoming sensor data
	select {} // Prevents the application from exiting
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

func subscribeToSensorUpdates(redisStore store.Store, rulesEngine *engine.RulesEngine) {
	// Assuming the RulesEngine has a method to extract the necessary sensor keys
	sensorKeys := rulesEngine.ExtractSensorKeys()

	// Subscribe to each sensor key
	for _, key := range sensorKeys {
		err := redisStore.Subscribe(key, func(data interface{}) {
			// Process the incoming sensor data with the rules engine
			rulesEngine.ProcessSensorData(key, data)
		})

		if err != nil {
			log.Printf("Error subscribing to %s: %v", key, err)
		}
	}
}

// Extract sensor keys from the compiled rules
// func extractSensorKeys(compiledRules []compiler.CompiledRule) []string {
// 	var keys []string
// 	keySet := make(map[string]bool)

// 	for _, rule := range compiledRules {
// 		for _, instr := range rule.Instructions {
// 			if instr.Opcode == bytecode.OpLoadFact { // Assuming this opcode loads a sensor fact
// 				key := instr.Operands[0].(string)
// 				if _, exists := keySet[key]; !exists {
// 					keys = append(keys, key)
// 					keySet[key] = true
// 				}
// 			}
// 		}
// 	}

// 	return keys
// }

// func SetupRESTInterface(store store.Store, compiledRules []bytecode.Instruction) {
// 	http.HandleFunc("/updateSensorData", func(w http.ResponseWriter, r *http.Request) {
// 		// Parse the incoming data
// 		var sensorData map[string]interface{}
// 		err := json.NewDecoder(r.Body).Decode(&sensorData)
// 		if err != nil {
// 			// Handle error
// 			http.Error(w, err.Error(), http.StatusBadRequest)
// 			return
// 		}

// 		// Execute the rules against this sensor data
// 		ExecuteBytecode(compiledRules, sensorData, store)

// 		// Respond to the API call
// 		fmt.Fprintf(w, "Processed sensor data")
// 	})

// 	log.Fatal(http.ListenAndServe(":8080", nil))
// }
