package store

import (
	"context"
	"sync"
	"testing"
	"time"

	// A mock Redis server
	"github.com/redis/go-redis/v9"
)

func TestRedisStoreSubscription(t *testing.T) {
	// Replace with your real Redis server address and options
	opts := &redis.Options{
		Addr: "localhost:6379",
		// Add other options like password, DB, etc., as needed
	}

	// Create a new RedisStore instance with the real Redis server options
	redisStore := NewRedisStore(opts).(*RedisStore)

	testKey := "testKey"
	receivedMessage := ""

	// Subscribe using the RedisStore
	err := redisStore.Subscribe(testKey, func(data interface{}) {
		receivedMessage = data.(string)
	})
	if err != nil {
		t.Fatalf("Subscribe returned an error: %v", err)
	}

	// Use a real Redis client to publish a message
	client := redis.NewClient(opts)
	defer client.Close()

	client.Publish(context.Background(), testKey, "testValue")

	// Adjust the sleep time as needed for real Redis behavior
	time.Sleep(1 * time.Second)

	// Check if the message was received
	if receivedMessage != "testValue" {
		t.Errorf("Expected message 'testValue', got '%s'", receivedMessage)
	}

	// Unsubscribe
	err = redisStore.Unsubscribe(testKey)
	if err != nil {
		t.Errorf("Unsubscribe returned an error: %v", err)
	}
}

func TestRedisStoreGetValue(t *testing.T) {
	// Replace with your real Redis server address and options
	opts := &redis.Options{
		Addr: "localhost:6379",
		// Add other options like password, DB, etc., as needed
	}

	// Create a new RedisStore instance with the real Redis server options
	redisStore := NewRedisStore(opts).(*RedisStore)

	testKey, testValue := "getValueKey", "someValue"

	// Use a real Redis client to set a value
	client := redis.NewClient(opts)
	defer client.Close()

	// Set the value for the test key
	err := client.Set(context.Background(), testKey, testValue, 0).Err()
	if err != nil {
		t.Fatalf("Error setting value in Redis: %v", err)
	}

	// Retrieve the value using RedisStore
	gotValue, err := redisStore.GetValue(testKey)
	if err != nil {
		t.Fatalf("GetValue returned an error: %v", err)
	}
	if gotValue != testValue {
		t.Errorf("Expected value '%s', got '%s'", testValue, gotValue)
	}

	// Clean up: remove the test key from Redis
	err = client.Del(context.Background(), testKey).Err()
	if err != nil {
		t.Errorf("Error cleaning up test key in Redis: %v", err)
	}
}

func TestRedisStoreErrorHandling(t *testing.T) {
	opts := &redis.Options{
		Addr: "localhost:6379",
		// Include other options like password, DB, etc., if needed
	}

	redisStore := NewRedisStore(opts).(*RedisStore)

	// Use a unique key to avoid conflicts with other tests or processes
	nonExistentKey := "uniqueNonExistentKeyForTesting"

	// Test fetching a value for a key that doesn't exist
	_, err := redisStore.GetValue(nonExistentKey)
	if err == nil {
		t.Errorf("Expected an error when getting value for a non-existent key, but got none")
	}

	// Test subscribing to a non-existent key
	receivedMessage := ""
	err = redisStore.Subscribe(nonExistentKey, func(data interface{}) {
		receivedMessage = data.(string)
	})
	if err != nil {
		t.Errorf("Subscribe returned an unexpected error: %v", err)
	}

	// Wait to ensure no messages are received for the non-existent key
	time.Sleep(500 * time.Millisecond)

	// Check that no message was received
	if receivedMessage != "" {
		t.Errorf("Received a message for a non-existent key subscription")
	}

	// Unsubscribe
	err = redisStore.Unsubscribe(nonExistentKey)
	if err != nil {
		t.Errorf("Unsubscribe returned an error: %v", err)
	}
}

func TestRedisStoreMultipleSubscriptions(t *testing.T) {
	opts := &redis.Options{
		Addr: "localhost:6379",
		// Include other options like password, DB, etc., if needed
	}

	redisStore := NewRedisStore(opts).(*RedisStore)

	keys := []string{"testKey1", "testKey2", "testKey3"}
	values := []string{"value1", "value2", "value3"}
	messagesReceived := make(map[string]string)
	var mu sync.Mutex // For synchronizing access to messagesReceived

	for _, key := range keys {
		localKey := key // Local variable to avoid closure capture issues
		err := redisStore.Subscribe(localKey, func(data interface{}) {
			mu.Lock()
			messagesReceived[localKey] = data.(string)
			mu.Unlock()
		})
		if err != nil {
			t.Errorf("Subscribe returned an error for key %s: %v", key, err)
		}
	}

	// Use a real Redis client to publish messages
	client := redis.NewClient(opts)
	defer client.Close()

	for i, key := range keys {
		err := client.Publish(context.Background(), key, values[i]).Err()
		if err != nil {
			t.Errorf("Error publishing to key %s: %v", key, err)
		}
	}

	// Allow some time for messages to be processed
	time.Sleep(1 * time.Second)

	mu.Lock()
	for i, key := range keys {
		if messagesReceived[key] != values[i] {
			t.Errorf("Expected message '%s' for key '%s', got '%s'", values[i], key, messagesReceived[key])
		}
	}
	mu.Unlock()

	// Unsubscribe from all keys
	for _, key := range keys {
		err := redisStore.Unsubscribe(key)
		if err != nil {
			t.Errorf("Unsubscribe returned an error for key %s: %v", key, err)
		}
	}
}
