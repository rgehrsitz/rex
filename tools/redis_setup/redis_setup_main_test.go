// rex/tools/redis_setup_cli/redis_setup_test.go

package main

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func TestConnectToRedis(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	defer s.Close()

	rdb := connectToRedis(s.Addr())
	assert.NotNil(t, rdb)

	// Test connection
	pong, err := rdb.Ping(context.Background()).Result()
	assert.NoError(t, err)
	assert.Equal(t, "PONG", pong)
}

func TestInitializeRedis(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})

	err = initializeRedis(rdb)
	assert.NoError(t, err)

	// Check if keys were set correctly
	val, err := rdb.Get(context.Background(), "weather:temperature").Result()
	assert.NoError(t, err)
	assert.Equal(t, "25.0", val)

	val, err = rdb.Get(context.Background(), "system:flow_rate").Result()
	assert.NoError(t, err)
	assert.Equal(t, "30", val)
}

func TestProcessCommand(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})

	// Test valid command
	err = processCommand(rdb, "set test:key1 value1")
	assert.NoError(t, err)

	val, err := rdb.Get(context.Background(), "test:key1").Result()
	assert.NoError(t, err)
	assert.Equal(t, "value1", val)

	// Test invalid command
	err = processCommand(rdb, "invalid command")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid command")

	// Test publish
	pubsub := rdb.Subscribe(context.Background(), "test")
	defer pubsub.Close()

	err = processCommand(rdb, "set test:key2 value2")
	assert.NoError(t, err)

	msg, err := pubsub.ReceiveMessage(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "test", msg.Channel)
	assert.Equal(t, "test:key2=value2", msg.Payload)
}
