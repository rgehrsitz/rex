// rex/pkg/compiler/store/redis_store.go

package store

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"rgehrsitz/rex/pkg/eventcontext"
	"rgehrsitz/rex/pkg/logging"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client      *redis.Client
	batchOnce   sync.Once
	batchClient *redis.Client
	durableMu   sync.Mutex
	durable     *RedisDurable
}

// RedisOptions defines connection settings without owning caller credentials or
// TLS configuration. TLSConfig is cloned by NewRedisStore.
type RedisOptions struct {
	Addr         string
	Username     string
	Password     string
	DB           int
	TLSConfig    *tls.Config
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// NewRedisStore establishes and verifies a Redis connection using the caller's
// cancellation and deadline. It never terminates the process.
func NewRedisStore(ctx context.Context, options RedisOptions) (*RedisStore, error) {
	if ctx == nil {
		return nil, fmt.Errorf("redis startup context is required")
	}
	if options.Addr == "" {
		return nil, fmt.Errorf("redis address is required")
	}
	if strings.Contains(options.Addr, "@") {
		return nil, fmt.Errorf("redis address must not contain credentials; use username and password options")
	}
	logAddress := redisLogAddress(options.Addr)
	logging.Logger.Info().Str("addr", logAddress).Int("db", options.DB).Bool("tls", options.TLSConfig != nil).Msg("Connecting to Redis")
	var tlsConfig *tls.Config
	if options.TLSConfig != nil {
		tlsConfig = options.TLSConfig.Clone()
	}

	client := redis.NewClient(&redis.Options{
		Addr:                  options.Addr,
		Username:              options.Username,
		Password:              options.Password,
		DB:                    options.DB,
		TLSConfig:             tlsConfig,
		DialTimeout:           options.DialTimeout,
		ReadTimeout:           options.ReadTimeout,
		WriteTimeout:          options.WriteTimeout,
		ContextTimeoutEnabled: true,
	})

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect to Redis at %s: %w", logAddress, err)
	}

	logging.Logger.Info().Str("addr", logAddress).Int("db", options.DB).Bool("tls", tlsConfig != nil).Msg("Successfully connected to Redis")

	return &RedisStore{client: client}, nil
}

func redisLogAddress(address string) string {
	if separator := strings.LastIndex(address, "@"); separator >= 0 {
		return address[separator+1:]
	}
	return address
}

// Ping checks current Redis connectivity with the caller's deadline.
func (s *RedisStore) Ping(ctx context.Context) error { return s.client.Ping(ctx).Err() }

// Close releases the Redis client resources held by the store.
func (s *RedisStore) Close() error {
	if s.batchClient != nil {
		_ = s.batchClient.Close()
	}
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
	if IsInternalKey(key) {
		return fmt.Errorf("fact key %q uses reserved internal state prefix", key)
	}
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
	if IsInternalKey(key) {
		return nil, fmt.Errorf("fact key %q uses reserved internal state prefix", key)
	}
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
	for _, key := range keys {
		if IsInternalKey(key) {
			return nil, fmt.Errorf("fact key %q uses reserved internal state prefix", key)
		}
	}
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
	if IsInternalKey(key) {
		return fmt.Errorf("fact key %q uses reserved internal state prefix", key)
	}
	group, _, _ := strings.Cut(key, ":")
	if strings.TrimSpace(group) == "" {
		return fmt.Errorf("fact key %q has no publish channel before ':'", key)
	}
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
		logging.Logger.Error().Err(err).Str("key", key).Msg("Failed to set fact in Redis")
		return err
	}

	// Need to break apart the key to get the group
	// Publish the value to a channel
	err = s.client.Publish(ctx, group, string(event)).Err()
	if err != nil {
		logging.Logger.Error().Err(err).Str("group", group).Str("key", key).Msg("Failed to publish fact update")
		return err
	}
	logging.Logger.Debug().Str("event", "fact_published").Str("channel", group).Str("fact_name", key).Msg("Published fact update")
	return nil
}
