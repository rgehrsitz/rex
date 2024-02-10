package store

// Store is an interface for key/value stores with pub/sub capabilities.
type Store interface {
	// GetValue retrieves a value for a given key from the store.
	GetValue(sensorName string) (interface{}, error)

	GetValues(sensorNames []string) (map[string]interface{}, error)

	// Subscribe sets up a subscription for changes to a specific key.
	Subscribe(key string, callback func(interface{})) error

	// Unsubscribe removes the subscription for changes to a specific key.
	Unsubscribe(key string) error

	SetValue(key string, value interface{}) error

	// ... Add any other necessary methods for your use case
}

// DataCallback is a function type for handling data updates from the store.
type DataCallback func(data interface{})
