// rex/pkg/compiler/store/redis_store.go

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"rgehrsitz/rex/pkg/logging"
	"strings"

	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

type RedisStore struct {
	client *redis.Client
}

// NewRedisStore creates a new instance of RedisStore with the given address, password, and database number.
// It establishes a connection to the Redis server and returns a pointer to the RedisStore.
func NewRedisStore(addr, password string, db int) *RedisStore {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	_, err := client.Ping(ctx).Result()
	if err != nil {
		logging.Logger.Fatal().Err(err).Msg("Failed to connect to Redis: %v")
	}

	return &RedisStore{client: client}
}

// SetFact sets a fact in the Redis store with the specified key and value.
// The value is serialized to JSON before being stored.
// Returns an error if there was a problem serializing the value or setting it in the store.
func (s *RedisStore) SetFact(key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, key, data, 0).Err()
}

func (s *RedisStore) GetFact(key string) (interface{}, error) {
	data, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	var value interface{}
	if err := json.Unmarshal([]byte(data), &value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *RedisStore) MGetFacts(keys ...string) (map[string]interface{}, error) {
	results, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	facts := make(map[string]interface{})
	for i, result := range results {
		if result == nil {
			facts[keys[i]] = nil
			continue
		}

		var value interface{}
		switch v := result.(type) {
		case string:
			if err := json.Unmarshal([]byte(v), &value); err != nil {
				return nil, err
			}
		case []byte:
			if err := json.Unmarshal(v, &value); err != nil {
				return nil, err
			}
		default:
			value = v
		}
		facts[keys[i]] = value
	}
	return facts, nil
}

func (s *RedisStore) Subscribe(channels ...string) *redis.PubSub {
	pubsub := s.client.Subscribe(ctx, channels...)

	// Verify the subscription was successful
	_, err := pubsub.Receive(ctx)
	if err != nil {
		logging.Logger.Error().Err(err).Msg("Failed to subscribe to Redis channels")
		return nil
	}

	logging.Logger.Info().Strs("channels", channels).Msg("Successfully subscribed to Redis channels")
	return pubsub
}

func (s *RedisStore) ReceiveFacts() <-chan *redis.Message {
	return s.client.Subscribe(ctx).Channel()
}

func (s *RedisStore) SetAndPublishFact(key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	// Set the value in Redis
	err = s.client.Set(ctx, key, data, 0).Err()
	if err != nil {
		return err
	}

	// Need to break apart the key to get the group
	group := strings.Split(key, ":")[0]
	// Publish the value to a channel
	err = s.client.Publish(ctx, group, fmt.Sprintf("%s=%s", key, data)).Err()
	log.Printf("Published update to group %s: %s=%s", group, key, data)
	log.Printf("context: %v", ctx)
	return err
}
