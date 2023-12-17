package store

import (
	"context"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client   *redis.Client
	channels map[string]*redis.PubSub
	mu       sync.Mutex
}

// NewRedisStore now accepts *redis.Options as a parameter.
func NewRedisStore(opts *redis.Options) Store {
	rdb := redis.NewClient(opts)
	return &RedisStore{
		client:   rdb,
		channels: make(map[string]*redis.PubSub),
	}
}

func (r *RedisStore) GetValue(key string) (interface{}, error) {
	ctx := context.Background()
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	return val, nil
}

func (r *RedisStore) Subscribe(key string, callback func(interface{})) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.channels[key]; exists {
		// Already subscribed to this key
		return nil
	}

	pubsub := r.client.Subscribe(context.Background(), key)
	r.channels[key] = pubsub

	go func() {
		ch := pubsub.Channel()
		for msg := range ch {
			callback(msg.Payload)
		}
	}()

	return nil
}

func (r *RedisStore) Unsubscribe(key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if pubsub, exists := r.channels[key]; exists {
		if err := pubsub.Close(); err != nil {
			return err
		}
		delete(r.channels, key)
	}

	return nil
}

func (r *RedisStore) SetValue(key string, value interface{}) error {
	ctx := context.Background()
	// Assuming value is a string or can be converted to a string.
	// You might need to handle different types or marshal to a string.
	valStr, ok := value.(string)
	if !ok {
		// Handle error or convert value to string as needed
		return fmt.Errorf("value for key %s is not a string", key)
	}
	return r.client.Set(ctx, key, valStr, 0).Err() // 0 means no expiration
}
