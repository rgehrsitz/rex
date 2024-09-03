// redis_store_test.go

package store

import (
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

// TestScanFacts checks if the ScanFacts method correctly retrieves keys matching a pattern.
func TestScanFacts(t *testing.T) {
	// Start a mock Redis server
	s, err := miniredis.Run()
	assert.NoError(t, err)
	defer s.Close()

	// Create a Redis client connected to the mock server
	redisClient := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	store := &RedisStore{client: redisClient}

	// Add test data to the mock Redis server
	s.Set("weather:temperature:morning", "20")
	s.Set("weather:temperature:afternoon", "30")
	s.Set("weather:temperature:evening", "25")
	s.Set("weather:humidity", "40")
	s.Set("system:status", "active")

	// Test case: Matching keys with wildcard "weather:temperature*"
	pattern := "weather:temperature*"
	keys, err := store.ScanFacts(pattern)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"weather:temperature:morning",
		"weather:temperature:afternoon",
		"weather:temperature:evening",
	}, keys, "Expected to find all temperature-related keys")

	// Test case: Matching keys with wildcard "system:*"
	pattern = "system:*"
	keys, err = store.ScanFacts(pattern)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"system:status",
	}, keys, "Expected to find the system status key")

	// Test case: No matching keys for "network:*"
	pattern = "network:*"
	keys, err = store.ScanFacts(pattern)
	assert.NoError(t, err)
	assert.Empty(t, keys, "Expected no keys to be found for the 'network:*' pattern")
}

// TestScanFactsNoKeys checks if ScanFacts correctly returns an empty result when no keys match the pattern.
func TestScanFactsNoKeys(t *testing.T) {
	// Start a mock Redis server
	s, err := miniredis.Run()
	assert.NoError(t, err)
	defer s.Close()

	// Create a Redis client connected to the mock server
	redisClient := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	store := &RedisStore{client: redisClient}

	// Test case: No matching keys for "nonexistent:*"
	pattern := "nonexistent:*"
	keys, err := store.ScanFacts(pattern)
	assert.NoError(t, err)
	assert.Empty(t, keys, "Expected no keys to be found for the 'nonexistent:*' pattern")
}

// TestScanFactsSpecialCharacters checks if ScanFacts correctly handles patterns with special characters.
func TestScanFactsSpecialCharacters(t *testing.T) {
	// Start a mock Redis server
	s, err := miniredis.Run()
	assert.NoError(t, err)
	defer s.Close()

	// Create a Redis client connected to the mock server
	redisClient := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	store := &RedisStore{client: redisClient}

	// Add test data to the mock Redis server
	s.Set("special:char:1$", "value1")
	s.Set("special:char:2$", "value2")
	s.Set("special:char#3", "value3")

	// Test case: Matching keys with wildcard "special:char:*"
	pattern := "special:char:*"
	keys, err := store.ScanFacts(pattern)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"special:char:1$",
		"special:char:2$",
		"special:char#3",
	}, keys, "Expected to find all keys with special characters")

	// Test case: Matching keys with wildcard "special:char:1*"
	pattern = "special:char:1*"
	keys, err = store.ScanFacts(pattern)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"special:char:1$",
	}, keys, "Expected to find the key 'special:char:1$'")
}

// TestScanFactsLargeNumberOfKeys checks the performance of ScanFacts with a large number of keys.
func TestScanFactsLargeNumberOfKeys(t *testing.T) {
	// Start a mock Redis server
	s, err := miniredis.Run()
	assert.NoError(t, err)
	defer s.Close()

	// Create a Redis client connected to the mock server
	redisClient := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	store := &RedisStore{client: redisClient}

	// Add a large number of test keys to the mock Redis server
	for i := 0; i < 1000; i++ {
		key := "weather:temperature:" + strconv.Itoa(i)
		s.Set(key, strconv.Itoa(i))
	}

	// Test case: Matching a large number of keys with "weather:temperature:*"
	pattern := "weather:temperature:*"
	keys, err := store.ScanFacts(pattern)
	assert.NoError(t, err)
	assert.Len(t, keys, 1000, "Expected to find 1000 keys for the 'weather:temperature:*' pattern")
}

// TestScanFactsRedisError checks if ScanFacts handles Redis errors correctly.
func TestScanFactsRedisError(t *testing.T) {
	// Start a mock Redis server and then shut it down to simulate an error
	s, err := miniredis.Run()
	assert.NoError(t, err)
	s.Close() // Simulate Redis server going down

	// Create a Redis client connected to the mock server
	redisClient := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	store := &RedisStore{client: redisClient}

	// Test case: Attempt to scan facts with the mock server down
	pattern := "weather:*"
	keys, err := store.ScanFacts(pattern)
	assert.Error(t, err, "Expected an error when Redis server is down")
	assert.Nil(t, keys, "Expected no keys to be returned when there is an error")
}
