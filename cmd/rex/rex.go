package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"rgehrsitz/rex/api"
	"rgehrsitz/rex/internal/bytecode"
	"rgehrsitz/rex/internal/compiler"
	"rgehrsitz/rex/internal/rule"
	"rgehrsitz/rex/internal/store"

	"github.com/redis/go-redis/v9"
)

func main() {

	// Start the REST API server
	api.StartServer("8080")

	// Initialize the Redis store
	redisOpts := &redis.Options{
		Addr: "localhost:6379",
		// ... other Redis options as needed ...
	}
	store := store.NewRedisStore(redisOpts)

	// Compile rules (assuming a function for this)
	compiledRules := compileRules("path_to_rules.json")

	// Subscribe to sensor updates
	SubscribeToSensorUpdates(store, compiledRules)

	// Setup REST interface
	SetupRESTInterface(store, compiledRules)

	// Other application initialization...

	runREX(compiledRules)
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

func SubscribeToSensorUpdates(store store.Store, compiledRules []compiler.CompiledRule) {
	sensorKeys := extractSensorKeys(compiledRules)

	for _, key := range sensorKeys {
		err := store.Subscribe(key, func(data interface{}) {
			// Convert or assert data to the required format
			sensorData, ok := data.(map[string]interface{})
			if !ok {
				log.Printf("Error: received data is not in expected format for key %s", key)
				return
			}

			// Execute the rules against this sensor data
			ExecuteBytecode(compiledRules, sensorData, store)
		})

		if err != nil {
			// Handle subscription error
			log.Printf("Error subscribing to %s: %v", key, err)
		}
	}
}

// Extract sensor keys from the compiled rules
func extractSensorKeys(compiledRules []compiler.CompiledRule) []string {
	var keys []string
	keySet := make(map[string]bool)

	for _, rule := range compiledRules {
		for _, instr := range rule.Instructions {
			if instr.Opcode == bytecode.OpLoadFact { // Assuming this opcode loads a sensor fact
				key := instr.Operands[0].(string)
				if _, exists := keySet[key]; !exists {
					keys = append(keys, key)
					keySet[key] = true
				}
			}
		}
	}

	return keys
}

func SetupRESTInterface(store store.Store, compiledRules []bytecode.Instruction) {
	http.HandleFunc("/updateSensorData", func(w http.ResponseWriter, r *http.Request) {
		// Parse the incoming data
		var sensorData map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&sensorData)
		if err != nil {
			// Handle error
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Execute the rules against this sensor data
		ExecuteBytecode(compiledRules, sensorData, store)

		// Respond to the API call
		fmt.Fprintf(w, "Processed sensor data")
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
