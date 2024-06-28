// rex/pkg/store/store_test.go

package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
)

func setupMiniredis(t *testing.T) (*miniredis.Miniredis, *RedisStore) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}

	store := NewRedisStore(s.Addr(), "", 0)
	return s, store
}

func TestRedisStoreSetAndGetFact(t *testing.T) {
	s, store := setupMiniredis(t)
	defer s.Close()

	// Set fact
	err := store.SetFact("test_fact", 123.45)
	assert.NoError(t, err)

	// Get fact
	value, err := store.GetFact("test_fact")
	assert.NoError(t, err)
	assert.Equal(t, 123.45, value.(float64))
}

func TestRedisStoreSetAndGetStringFact(t *testing.T) {
	s, store := setupMiniredis(t)
	defer s.Close()

	// Set fact
	err := store.SetFact("test_string_fact", "hello world")
	assert.NoError(t, err)

	// Get fact
	value, err := store.GetFact("test_string_fact")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", value.(string))
}

func TestRedisStoreSetAndGetBooleanFact(t *testing.T) {
	s, store := setupMiniredis(t)
	defer s.Close()

	// Set fact
	err := store.SetFact("test_bool_fact", true)
	assert.NoError(t, err)

	// Get fact
	value, err := store.GetFact("test_bool_fact")
	assert.NoError(t, err)
	assert.Equal(t, true, value.(bool))
}

func TestRedisStoreGetNonExistentFact(t *testing.T) {
	s, store := setupMiniredis(t)
	defer s.Close()

	// Get non-existent fact
	value, _ := store.GetFact("non_existent_fact")
	assert.Nil(t, value)
}

func TestRedisStoreSetAndGetMultipleFacts(t *testing.T) {
	s, store := setupMiniredis(t)
	defer s.Close()

	// Set multiple facts
	err := store.SetFact("fact1", 42.0)
	assert.NoError(t, err)

	err = store.SetFact("fact2", "example")
	assert.NoError(t, err)

	err = store.SetFact("fact3", false)
	assert.NoError(t, err)

	// Get multiple facts
	value1, err := store.GetFact("fact1")
	assert.NoError(t, err)
	assert.Equal(t, 42.0, value1.(float64))

	value2, err := store.GetFact("fact2")
	assert.NoError(t, err)
	assert.Equal(t, "example", value2.(string))

	value3, err := store.GetFact("fact3")
	assert.NoError(t, err)
	assert.Equal(t, false, value3.(bool))
}

func TestRedisStoreMGetFacts(t *testing.T) {
	s, store := setupMiniredis(t)
	defer s.Close()

	// Set multiple facts
	err := store.SetFact("fact1", 42.0)
	assert.NoError(t, err)

	err = store.SetFact("fact2", "example")
	assert.NoError(t, err)

	err = store.SetFact("fact3", true)
	assert.NoError(t, err)

	// MGet facts
	facts, err := store.MGetFacts("fact1", "fact2", "fact3", "non_existent_fact")
	assert.NoError(t, err)
	assert.Equal(t, 42.0, facts["fact1"].(float64))
	assert.Equal(t, "example", facts["fact2"].(string))
	assert.Equal(t, true, facts["fact3"].(bool))
	assert.Nil(t, facts["non_existent_fact"])
}

func TestSubscribe(t *testing.T) {
	s, store := setupMiniredis(t)
	defer s.Close()

	pubsub := store.Subscribe("test_channel")
	assert.NotNil(t, pubsub)

	// Clean up
	pubsub.Close()
}

func TestReceiveFacts(t *testing.T) {
	s, store := setupMiniredis(t)
	defer s.Close()

	ch := store.ReceiveFacts()
	assert.NotNil(t, ch)

	// Clean up
	store.client.Close()
}

func TestSetAndPublishFact(t *testing.T) {
	s, store := setupMiniredis(t)
	defer s.Close()

	key := "test:key"
	value := "test_value"

	// Subscribe to the channel before publishing
	pubsub := store.Subscribe("test")
	defer pubsub.Close()

	err := store.SetAndPublishFact(key, value)
	assert.NoError(t, err)

	// Verify the fact was set
	result, err := store.GetFact(key)
	assert.NoError(t, err)
	assert.Equal(t, value, result)

	// Verify the message was published
	msg, err := pubsub.ReceiveMessage(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "test", msg.Channel)
	expectedPayload := fmt.Sprintf("%s=\"%s\"", key, value) // Account for JSON encoding of string
	assert.Equal(t, expectedPayload, msg.Payload)

	// Verify the value in miniredis
	storedValue, err := s.Get(key)
	assert.NoError(t, err)
	assert.Equal(t, `"`+value+`"`, storedValue) // miniredis stores strings with quotes
}

func TestSetAndPublishFactWithDifferentTypes(t *testing.T) {
	s, store := setupMiniredis(t)
	defer s.Close()

	testCases := []struct {
		name  string
		key   string
		value interface{}
	}{
		{"String", "test:string", "hello"},
		{"Float", "test:float", 3.14},
		{"Boolean", "test:bool", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Subscribe to the channel before publishing
			pubsub := store.Subscribe("test")
			defer pubsub.Close()

			err := store.SetAndPublishFact(tc.key, tc.value)
			assert.NoError(t, err)

			// Verify the fact was set
			result, err := store.GetFact(tc.key)
			assert.NoError(t, err)
			assert.Equal(t, tc.value, result)

			// Verify the message was published
			msg, err := pubsub.ReceiveMessage(context.Background())
			assert.NoError(t, err)
			assert.Equal(t, "test", msg.Channel)

			var expectedPayload string
			switch v := tc.value.(type) {
			case string:
				expectedPayload = fmt.Sprintf("%s=\"%s\"", tc.key, v)
			default:
				expectedPayload = fmt.Sprintf("%s=%v", tc.key, v)
			}
			assert.Equal(t, expectedPayload, msg.Payload)

			// Verify the value in miniredis
			storedValue, err := s.Get(tc.key)
			assert.NoError(t, err)
			expectedStoredValue := fmt.Sprintf("%v", tc.value)
			if tc.name == "String" {
				expectedStoredValue = `"` + expectedStoredValue + `"`
			}
			assert.Equal(t, expectedStoredValue, storedValue)
		})
	}
}
