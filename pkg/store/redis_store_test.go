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
	// Update expected results to only include the correct matching keys
	assert.ElementsMatch(t, []string{
		"special:char:1$",
		"special:char:2$",
	}, keys, "Expected to find keys that match the pattern 'special:char:*'")

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
	// Start a mock Redis server
	s, err := miniredis.Run()
	assert.NoError(t, err)
	defer s.Close() // Ensure server is properly closed after test

	// Create a Redis client connected to the mock server
	redisClient := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	store := &RedisStore{client: redisClient}

	// Simulate Redis server error by pointing to an unreachable address
	store.client = redis.NewClient(&redis.Options{
		Addr: "localhost:9999", // A non-existent Redis server address
	})

	// Test case: Attempt to scan facts with an unreachable Redis server
	pattern := "weather:*"
	keys, err := store.ScanFacts(pattern)
	assert.Error(t, err, "Expected an error when Redis server is unreachable")
	assert.Nil(t, keys, "Expected no keys to be returned when there is an error")
}

// TestScanFactsSingleCharacterWildcard checks if ScanFacts correctly handles patterns with the single-character wildcard.
func TestScanFactsSingleCharacterWildcard(t *testing.T) {
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
	s.Set("temperature:morn", "20")
	s.Set("temperature:mornin", "30")
	s.Set("temperature:morning", "25")
	s.Set("temperature:mooning", "15")

	// Adjusted Test case: Single-character wildcard "temperature:mornin?"
	// Pattern: "temperature:mornin?" - Expecting "temperature:morning"
	pattern := "temperature:mornin?"
	keys, err := store.ScanFacts(pattern)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"temperature:morning",
	}, keys, "Expected to find the key 'temperature:morning'")

	// Adjusted Test case: Single-character wildcard "temperature:moon?ng"
	// Pattern: "temperature:moon?ng" - Expecting "temperature:mooning"
	pattern = "temperature:moon?ng"
	keys, err = store.ScanFacts(pattern)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"temperature:mooning",
	}, keys, "Expected to find the key 'temperature:mooning'")

	// Adjusted Test case: Single-character wildcard "temperature:m????ng"
	// This test case checks if multiple keys match
	pattern = "temperature:m????ng"
	keys, err = store.ScanFacts(pattern)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"temperature:morning",
		"temperature:mooning",
	}, keys, "Expected to find both keys 'temperature:morning' and 'temperature:mooning'")

	// Adjusted Test case: No matching keys with wildcard "temperature:???"
	// This checks a pattern that should have no match.
	pattern = "temperature:???"
	keys, err = store.ScanFacts(pattern)
	assert.NoError(t, err)
	assert.Empty(t, keys, "Expected no keys to be found for the pattern 'temperature:???'")
}

//var ctx = context.Background()

func setupRedis() (*RedisStore, func(), error) {
	// Create a Redis client connected to your local Redis server
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379", // Adjust this if your Redis server runs on a different host or port
	})

	// Clear the Redis database before running each test
	err := redisClient.FlushDB(ctx).Err()
	if err != nil {
		return nil, nil, err
	}

	store := &RedisStore{client: redisClient}

	// Return the store and a teardown function to close the connection
	return store, func() {
		redisClient.Close()
	}, nil
}

func TestScanFactsMixedWildcards(t *testing.T) {
	store, teardown, err := setupRedis()
	assert.NoError(t, err)
	defer teardown()

	// Add test data to the real Redis server
	err = store.client.Set(ctx, "user:1234:active", "true", 0).Err()
	assert.NoError(t, err)
	err = store.client.Set(ctx, "user:1235:inactive", "false", 0).Err()
	assert.NoError(t, err)
	err = store.client.Set(ctx, "user:1236:active", "true", 0).Err()
	assert.NoError(t, err)

	// Test case: Mixed wildcards pattern "user:12*:?ctive"
	// Ensure we are testing the correct pattern interpretation by Redis
	pattern := "user:12*:?ctive"
	keys, err := store.ScanFacts(pattern)
	assert.NoError(t, err)

	// The expected result should only include the active entries
	assert.ElementsMatch(t, []string{
		"user:1234:active",
		"user:1236:active",
	}, keys, "Expected to find only 'user:1234:active' and 'user:1236:active'")

	// To confirm Redis interpretation, let's test additional explicit patterns
	// Test case: Pattern without wildcards
	pattern = "user:1234:active"
	keys, err = store.ScanFacts(pattern)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"user:1234:active",
	}, keys, "Expected to find only 'user:1234:active'")

	// Test case: Simple wildcard matching
	pattern = "user:12*"
	keys, err = store.ScanFacts(pattern)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"user:1234:active",
		"user:1235:inactive",
		"user:1236:active",
	}, keys, "Expected to find all three keys matching the pattern 'user:12*'")

	// Test case: Simple wildcard matching
	pattern = "user:123?:?ctive"
	keys, err = store.ScanFacts(pattern)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"user:1234:active",
		"user:1236:active",
	}, keys, "Expected to find only two keys matching the pattern 'user:12*'")
}
