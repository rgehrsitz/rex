// rex/pkg/store/store_test.go

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rgehrsitz/rex/pkg/eventcontext"
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
	defer store.Close()

	pubsub, err := store.Subscribe(context.Background(), "test_channel")
	assert.NoError(t, err)
	assert.NotNil(t, pubsub)

	// Clean up
	pubsub.Close()
}

func TestSubscribeHonorsContext(t *testing.T) {
	s, store := setupMiniredis(t)
	defer s.Close()
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pubsub, err := store.Subscribe(ctx, "test_channel")
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, pubsub)
}

func TestCloseReleasesClient(t *testing.T) {
	s, store := setupMiniredis(t)
	defer s.Close()

	assert.NoError(t, store.Close())
	assert.Error(t, store.SetFact("test_fact", "value"))
}

func TestFactOperationsHonorContext(t *testing.T) {
	s, store := setupMiniredis(t)
	defer s.Close()
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.ErrorIs(t, store.SetFactContext(ctx, "test_fact", "value"), context.Canceled)
	_, err := store.GetFactContext(ctx, "test_fact")
	assert.ErrorIs(t, err, context.Canceled)
	_, err = store.MGetFactsContext(ctx, "test_fact")
	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorIs(t, store.SetAndPublishFactContext(ctx, "test:fact", "value"), context.Canceled)
}

func TestSetAndPublishFact(t *testing.T) {
	s, store := setupMiniredis(t)
	defer s.Close()

	key := "test:key"
	value := "test_value"

	// Subscribe to the channel before publishing
	pubsub, err := store.Subscribe(context.Background(), "test")
	require.NoError(t, err)
	defer pubsub.Close()

	err = store.SetAndPublishFact(key, value)
	assert.NoError(t, err)

	// Verify the fact was set
	result, err := store.GetFact(key)
	assert.NoError(t, err)
	assert.Equal(t, value, result)

	// Verify the message was published
	msg, err := pubsub.ReceiveMessage(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "test", msg.Channel)
	var event map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(msg.Payload), &event))
	assert.Equal(t, value, event[key])

	// Verify the value in miniredis
	storedValue, err := s.Get(key)
	assert.NoError(t, err)
	assert.Equal(t, `"`+value+`"`, storedValue) // miniredis stores strings with quotes
}

func TestSetAndPublishFactContextPreservesEventMetadata(t *testing.T) {
	_, store := setupMiniredis(t)
	defer store.Close()

	pubsub, err := store.Subscribe(context.Background(), "test")
	require.NoError(t, err)
	defer pubsub.Close()

	ctx := eventcontext.WithMetadata(context.Background(), eventcontext.Metadata{TraceID: "event-12", Hop: 4})
	require.NoError(t, store.SetAndPublishFactContext(ctx, "test:key", "value"))

	msg, err := pubsub.ReceiveMessage(context.Background())
	require.NoError(t, err)
	facts, metadata, enveloped, err := eventcontext.DecodeFactEvent([]byte(msg.Payload))
	require.NoError(t, err)
	assert.True(t, enveloped)
	assert.Equal(t, eventcontext.Metadata{TraceID: "event-12", Hop: 5}, metadata)
	assert.Equal(t, "value", facts["test:key"])
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
			pubsub, err := store.Subscribe(context.Background(), "test")
			require.NoError(t, err)
			defer pubsub.Close()

			err = store.SetAndPublishFact(tc.key, tc.value)
			assert.NoError(t, err)

			// Verify the fact was set
			result, err := store.GetFact(tc.key)
			assert.NoError(t, err)
			assert.Equal(t, tc.value, result)

			// Verify the message was published
			msg, err := pubsub.ReceiveMessage(context.Background())
			assert.NoError(t, err)
			assert.Equal(t, "test", msg.Channel)

			var event map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(msg.Payload), &event))
			assert.Equal(t, tc.value, event[tc.key])

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
