package redis

import (
	"context"
	"rgehrsitz/rex/internal/store"
	"sync"

	"github.com/redis/go-redis/v9" // Assuming use of go-redis package
)

type RedisStore struct {
	client   *redis.Client
	channels map[string]*redis.PubSub
	mu       sync.Mutex
}

// NewRedisStore now accepts *redis.Options as a parameter.
func NewRedisStore(opts *redis.Options) store.Store {
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
