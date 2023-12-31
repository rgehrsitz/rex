// File: internal/store/mock/mock_store.go
package mock

import "fmt"

type MockStore struct {
	data map[string]interface{}
}

func NewMockStore() *MockStore {
	return &MockStore{data: make(map[string]interface{})}
}

func (m *MockStore) GetValue(key string) (interface{}, error) {
	value, exists := m.data[key]
	if !exists {
		return nil, fmt.Errorf("key not found")
	}
	return value, nil
}

// Updated SetValue method to return an error
func (m *MockStore) SetValue(key string, value interface{}) error {
	m.data[key] = value
	return nil // Assuming setting a value in the mock store always succeeds
}

// Subscribe matches the signature expected by the store.Store interface.
func (m *MockStore) Subscribe(channel string, callback func(interface{})) error {
	// Mock implementation or leave empty if not relevant for testing
	return nil
}

// Unsubscribe is a placeholder for the Unsubscribe method required by the store.Store interface.
// Implement this method based on the actual interface definition.
func (m *MockStore) Unsubscribe(channel string) error {
	// Mock implementation or leave empty if not relevant for testing
	return nil
}
