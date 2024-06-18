// rex/pkg/compiler/store/redis_store.go

package store

import (
	"context"
	"encoding/json"
	"log"

	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(addr, password string, db int) *RedisStore {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	_, err := client.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	return &RedisStore{client: client}
}

func (s *RedisStore) SetFact(key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, key, data, 0).Err()
}

func (s *RedisStore) GetFact(key string) (interface{}, error) {
	data, err := s.client.Get(ctx, key).Result()
	if err != nil {
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
			continue
		}
		var value interface{}
		if err := json.Unmarshal([]byte(result.(string)), &value); err != nil {
			return nil, err
		}
		facts[keys[i]] = value
	}
	return facts, nil
}
