// rex/tools/redis_setup_cli/redis_setup.go

//  This program initializes the Redis database with default values
//  for the rex system. This includes groups, keys, and values for
//  each group. It also starts a CLI for modifying values when debugging.

package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

func main() {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	// Initialize pub/sub groups and default keys
	initializeRedis(rdb)

	// Start CLI for modifying values
	startCLI(rdb)
}

func initializeRedis(rdb *redis.Client) {
	groups := map[string]map[string]string{
		"weather": {
			"temperature": "25.0",
			"humidity":    "60",
			"pressure":    "1013.25",
		},
		"system": {
			"flow_rate": "30",
			"velocity":  "45",
		},
	}

	for group, keys := range groups {
		for key, value := range keys {
			fullKey := fmt.Sprintf("%s:%s", group, key)
			err := rdb.Set(ctx, fullKey, value, 0).Err()
			if err != nil {
				fmt.Printf("Error setting %s: %v\n", fullKey, err)
			} else {
				fmt.Printf("Set %s to %s\n", fullKey, value)
			}
		}
	}
}

func startCLI(rdb *redis.Client) {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("Enter command (set <group:key> <value> or exit): ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "exit" {
			break
		}

		parts := strings.Split(input, " ")
		if len(parts) != 3 || parts[0] != "set" {
			fmt.Println("Invalid command. Use 'set <group:key> <value>'")
			continue
		}

		key := parts[1]
		value := parts[2]

		err := rdb.Set(ctx, key, value, 0).Err()
		if err != nil {
			fmt.Printf("Error setting %s: %v\n", key, err)
		} else {
			fmt.Printf("Set %s to %s\n", key, value)
			// Publish update to the group channel
			group := strings.Split(key, ":")[0]
			rdb.Publish(ctx, group, fmt.Sprintf("%s=%s", key, value))
			log.Printf("Published update to group %s: %s=%s", group, key, value)
			log.Printf("context: %v", ctx)
		}
	}
}
