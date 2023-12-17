package main

import (
	"encoding/json"
	"log"
	"os"
	"rgehrsitz/rex/internal/compiler"
	"rgehrsitz/rex/pkg/rule"
)

func main() {
	store := NewKeyValueStore("your_redis_config")

	// Compile rules (assuming a function for this)
	compiledRules := CompileRules("path_to_rules.json")

	// Subscribe to sensor updates
	SubscribeToSensorUpdates(store, compiledRules)

	// Setup REST interface
	SetupRESTInterface(store, compiledRules)

	// Other application initialization...

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


func SubscribeToSensorUpdates(store store.Store, compiledRules []bytecode.Instruction) {
    // Define the list of sensor keys you want to subscribe to
    sensorKeys := []string{"sensor1", "sensor2", ...}

    for _, key := range sensorKeys {
        err := store.Subscribe(key, func(data interface{}) {
            // Convert or assert data to the required format
            sensorData := data.(map[string]interface{})

            // Execute the rules against this sensor data
            ExecuteBytecode(compiledRules, sensorData, store)
        })

        if err != nil {
            // Handle subscription error
            log.Printf("Error subscribing to %s: %v", key, err)
        }
    }
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
