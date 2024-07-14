package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	redisAddr  string
	updateRate int
	factTypes  []string
)

func init() {
	flag.StringVar(&redisAddr, "redis", "localhost:6379", "Redis address")
	flag.IntVar(&updateRate, "rate", 10, "Number of fact updates per second")
	flag.Parse()

	factTypes = []string{"temperature", "humidity", "pressure", "wind_speed"}
}

func main() {
	ctx := context.Background()

	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		panic(fmt.Sprintf("Failed to connect to Redis: %v", err))
	}

	fmt.Printf("Connected to Redis at %s\n", redisAddr)
	fmt.Printf("Updating facts at a rate of %d per second\n", updateRate)

	// Start updating facts
	ticker := time.NewTicker(time.Second / time.Duration(updateRate))
	defer ticker.Stop()

	for range ticker.C {
		factType := factTypes[rand.Intn(len(factTypes))]
		factValue := rand.Float64() * 100 // Random value between 0 and 100
		factKey := fmt.Sprintf("weather:%s", factType)

		err := rdb.Set(ctx, factKey, factValue, 0).Err()
		if err != nil {
			fmt.Printf("Error setting fact: %v\n", err)
			continue
		}

		// Publish the fact update
		message := fmt.Sprintf("%s=%f", factKey, factValue)
		err = rdb.Publish(ctx, "weather", message).Err()
		if err != nil {
			fmt.Printf("Error publishing fact update: %v\n", err)
			continue
		}

		fmt.Printf("Updated and published fact: %s\n", message)
	}
}
