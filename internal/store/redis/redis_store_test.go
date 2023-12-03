package redis

import (
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2" // A mock Redis server
	"github.com/redis/go-redis/v9"
)

func TestRedisStoreSubscription(t *testing.T) {
	// Start miniredis
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("an error '%s' occurred when starting miniredis", err)
	}
	defer mr.Close()

	// Create Redis options to connect to miniredis
	opts := &redis.Options{
		Addr: mr.Addr(),
	}

	// Create a new RedisStore instance with the options
	redisStore := NewRedisStore(opts).(*RedisStore)

	// Define a key to subscribe to
	testKey := "testKey"
	receivedMessage := ""

	// Subscribe using the RedisStore
	err = redisStore.Subscribe(testKey, func(data interface{}) {
		receivedMessage = data.(string)
	})
	if err != nil {
		t.Fatalf("Subscribe returned an error: %v", err)
	}

	// Wait a bit for the subscription to be active
	time.Sleep(100 * time.Millisecond)

	// Publish a message using miniredis
	mr.Publish(testKey, "testValue")

	// Wait for the message to be received
	time.Sleep(500 * time.Millisecond) // Increase this time to ensure the message is received

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
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Error occurred when starting miniredis: %s", err)
	}
	defer mr.Close()

	testKey, testValue := "getValueKey", "someValue"
	mr.Set(testKey, testValue)

	redisStore := NewRedisStore(&redis.Options{Addr: mr.Addr()}).(*RedisStore)

	gotValue, err := redisStore.GetValue(testKey)
	if err != nil {
		t.Fatalf("GetValue returned an error: %v", err)
	}
	if gotValue != testValue {
		t.Errorf("Expected value '%s', got '%s'", testValue, gotValue)
	}
}

func TestRedisStoreUnsubscribe(t *testing.T) {
	// Start miniredis
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("an error '%s' occurred when starting miniredis", err)
	}
	defer mr.Close()

	// Create Redis options to connect to miniredis
	opts := &redis.Options{
		Addr: mr.Addr(),
	}

	// Create a new RedisStore instance with the options
	redisStore := NewRedisStore(opts).(*RedisStore)

	testKey := "unsubscribeKey"
	callbackCalled := false
	callback := func(data interface{}) {
		fmt.Println("Callback executed") // Temporary debugging statement
		callbackCalled = true
	}

	redisStore.Subscribe(testKey, callback)
	mr.Publish(testKey, "testValue")

	// Wait for the message to be received
	time.Sleep(500 * time.Millisecond)

	if !callbackCalled {
		t.Fatalf("Callback was not called before unsubscribe")
	}

	// Unsubscribe and test
	err = redisStore.Unsubscribe(testKey)
	if err != nil {
		t.Fatalf("Unsubscribe returned an error: %v", err)
	}

	// Reset flag and try publishing again
	callbackCalled = false
	mr.Publish(testKey, "newValue")

	time.Sleep(500 * time.Millisecond)
	if callbackCalled {
		t.Errorf("Callback was called after unsubscribe")
	}
}
