package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedisStoreSetAndGetFact(t *testing.T) {
	s := NewRedisStore("localhost:6379", "", 0)

	// Set fact
	err := s.SetFact("test_fact", 123.45)
	assert.NoError(t, err)

	// Get fact
	value, err := s.GetFact("test_fact")
	assert.NoError(t, err)
	assert.Equal(t, 123.45, value.(float64))
}

func TestRedisStoreSetAndGetStringFact(t *testing.T) {
	s := NewRedisStore("localhost:6379", "", 0)

	// Set fact
	err := s.SetFact("test_string_fact", "hello world")
	assert.NoError(t, err)

	// Get fact
	value, err := s.GetFact("test_string_fact")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", value.(string))
}

func TestRedisStoreSetAndGetBooleanFact(t *testing.T) {
	s := NewRedisStore("localhost:6379", "", 0)

	// Set fact
	err := s.SetFact("test_bool_fact", true)
	assert.NoError(t, err)

	// Get fact
	value, err := s.GetFact("test_bool_fact")
	assert.NoError(t, err)
	assert.Equal(t, true, value.(bool))
}

func TestRedisStoreGetNonExistentFact(t *testing.T) {
	s := NewRedisStore("localhost:6379", "", 0)

	// Get non-existent fact
	value, _ := s.GetFact("non_existent_fact")
	assert.Nil(t, value)
}

func TestRedisStoreSetAndGetMultipleFacts(t *testing.T) {
	s := NewRedisStore("localhost:6379", "", 0)

	// Set multiple facts
	err := s.SetFact("fact1", 42.0)
	assert.NoError(t, err)

	err = s.SetFact("fact2", "example")
	assert.NoError(t, err)

	err = s.SetFact("fact3", false)
	assert.NoError(t, err)

	// Get multiple facts
	value1, err := s.GetFact("fact1")
	assert.NoError(t, err)
	assert.Equal(t, 42.0, value1.(float64))

	value2, err := s.GetFact("fact2")
	assert.NoError(t, err)
	assert.Equal(t, "example", value2.(string))

	value3, err := s.GetFact("fact3")
	assert.NoError(t, err)
	assert.Equal(t, false, value3.(bool))
}

func TestRedisStoreMGetFacts(t *testing.T) {
	s := NewRedisStore("localhost:6379", "", 0)

	// Set multiple facts
	err := s.SetFact("fact1", 42.0)
	assert.NoError(t, err)

	err = s.SetFact("fact2", "example")
	assert.NoError(t, err)

	err = s.SetFact("fact3", true)
	assert.NoError(t, err)

	// MGet facts
	facts, err := s.MGetFacts("fact1", "fact2", "fact3", "non_existent_fact")
	assert.NoError(t, err)
	assert.Equal(t, 42.0, facts["fact1"].(float64))
	assert.Equal(t, "example", facts["fact2"].(string))
	assert.Equal(t, true, facts["fact3"].(bool))
	assert.Nil(t, facts["non_existent_fact"])
}
