// rex/pkg/compiler/store/redis_store.go

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"rgehrsitz/rex/pkg/eventcontext"
	"rgehrsitz/rex/pkg/logging"
	"strings"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client *redis.Client
}

// NewRedisStore creates a new instance of RedisStore with the given address, password, and database number.
// It establishes a connection to the Redis server and returns a pointer to the RedisStore.
func NewRedisStore(addr, password string, db int) *RedisStore {
	logging.Logger.Info().Str("addr", addr).Int("db", db).Msg("Connecting to Redis")

	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	_, err := client.Ping(context.Background()).Result()
	if err != nil {
		logging.Logger.Fatal().Err(err).Msg("Failed to connect to Redis")
	}

	logging.Logger.Info().Msg("Successfully connected to Redis")

	return &RedisStore{client: client}
}

// Close releases the Redis client resources held by the store.
func (s *RedisStore) Close() error {
	return s.client.Close()
}

// SetFact sets a fact in the Redis store with the specified key and value.
// The value is serialized to JSON before being stored.
// Returns an error if there was a problem serializing the value or setting it in the store.
func (s *RedisStore) SetFact(key string, value interface{}) error {
	return s.SetFactContext(context.Background(), key, value)
}

// SetFactContext sets a fact using the caller's context.
func (s *RedisStore) SetFactContext(ctx context.Context, key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, key, data, 0).Err()
}

func (s *RedisStore) GetFact(key string) (interface{}, error) {
	return s.GetFactContext(context.Background(), key)
}

// GetFactContext retrieves a fact using the caller's context.
func (s *RedisStore) GetFactContext(ctx context.Context, key string) (interface{}, error) {
	data, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		logging.Logger.Debug().Str("key", key).Msg("Fact not found in Redis")
		return nil, nil
	} else if err != nil {
		logging.Logger.Error().Err(err).Str("key", key).Msg("Failed to get fact from Redis")
		return nil, err
	}

	var value interface{}
	if err := json.Unmarshal([]byte(data), &value); err != nil {
		logging.Logger.Error().Err(err).Str("key", key).Str("data", data).Msg("Failed to unmarshal fact data")
		return nil, err
	}
	logging.Logger.Debug().Str("key", key).Interface("value", value).Msg("Retrieved fact from Redis")
	return value, nil
}

func (s *RedisStore) MGetFacts(keys ...string) (map[string]interface{}, error) {
	return s.MGetFactsContext(context.Background(), keys...)
}

// MGetFactsContext retrieves facts using the caller's context.
func (s *RedisStore) MGetFactsContext(ctx context.Context, keys ...string) (map[string]interface{}, error) {
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

func (s *RedisStore) Subscribe(ctx context.Context, channels ...string) (*redis.PubSub, error) {
	logging.Logger.Info().Strs("channels", channels).Msg("Subscribing to Redis channels")

	pubsub := s.client.Subscribe(ctx, channels...)

	// Verify the subscription was successful
	_, err := pubsub.Receive(ctx)
	if err != nil {
		logging.Logger.Error().Err(err).Msg("Failed to subscribe to Redis channels")
		_ = pubsub.Close()
		return nil, fmt.Errorf("subscribe to Redis channels: %w", err)
	}

	logging.Logger.Info().Strs("channels", channels).Msg("Successfully subscribed to Redis channels")
	return pubsub, nil
}

func (s *RedisStore) SetAndPublishFact(key string, value interface{}) error {
	return s.SetAndPublishFactContext(context.Background(), key, value)
}

// SetAndPublishFactContext updates and publishes a fact using the caller's context.
func (s *RedisStore) SetAndPublishFactContext(ctx context.Context, key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		logging.Logger.Error().Err(err).Str("key", key).Interface("value", value).Msg("Failed to marshal fact value")
		return err
	}
	event, err := eventcontext.EncodeFactUpdate(ctx, key, value)
	if err != nil {
		logging.Logger.Error().Err(err).Str("key", key).Interface("value", value).Msg("Failed to marshal fact event")
		return err
	}
	// Set the value in Redis
	err = s.client.Set(ctx, key, data, 0).Err()
	if err != nil {
		logging.Logger.Error().Err(err).Str("key", key).Str("data", string(data)).Msg("Failed to set fact in Redis")
		return err
	}

	// Need to break apart the key to get the group
	group := strings.Split(key, ":")[0]
	// Publish the value to a channel
	err = s.client.Publish(ctx, group, string(event)).Err()
	if err != nil {
		logging.Logger.Error().Err(err).Str("group", group).Str("key", key).Str("event", string(event)).Msg("Failed to publish fact update")
		return err
	}
	log.Printf("Published update to group %s: %s", group, string(event))
	return nil
}
