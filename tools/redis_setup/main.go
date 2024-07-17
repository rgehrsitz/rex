// rex/tools/redis_setup_cli/redis_setup.go

package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

func main() {
	rdb := connectToRedis("localhost:6379")
	initializeRedis(rdb)
	startCLI(rdb)
}

func connectToRedis(addr string) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return rdb
}

func initializeRedis(rdb *redis.Client) error {
	groups := map[string]map[string]string{
		"weather": {
			"temperature": "25.0",
			"humidity":    "60",
			"pressure":    "1013.25",
			"flow_rate":   "30",
			"velocity":    "45",
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
				return err
			}
			fmt.Printf("Set %s to %s\n", fullKey, value)
		}
	}
	return nil
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

		err := processCommand(rdb, input)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		}
	}
}

func processCommand(rdb *redis.Client, input string) error {
	parts := strings.Split(input, " ")
	if len(parts) != 3 || parts[0] != "set" {
		return fmt.Errorf("invalid command. Use 'set <group:key> <value>'")
	}

	key := parts[1]
	value := parts[2]

	err := rdb.Set(ctx, key, value, 0).Err()
	if err != nil {
		return fmt.Errorf("error setting %s: %v", key, err)
	}

	fmt.Printf("Set %s to %s\n", key, value)

	// Publish update to the group channel
	group := strings.Split(key, ":")[0]
	err = rdb.Publish(ctx, group, fmt.Sprintf("%s=%s", key, value)).Err()
	if err != nil {
		return fmt.Errorf("error publishing update: %v", err)
	}

	fmt.Printf("Published update to group %s: %s=%s\n", group, key, value)
	return nil
}
