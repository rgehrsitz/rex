package main

import (
	"fmt"
	"log"
	"os"
	"rgehrsitz/rex/internal/engine"
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
