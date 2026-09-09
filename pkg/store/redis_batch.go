package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const MaxSnapshotBytes = 4 << 20
const ResultsChannel = "rex_results"

func (s *RedisStore) batchWriter() *redis.Client {
	s.batchOnce.Do(func() {
		options := *s.client.Options()
		options.PushNotificationProcessor = nil
		if options.MaintNotificationsConfig != nil {
			copyConfig := *options.MaintNotificationsConfig
			options.MaintNotificationsConfig = &copyConfig
		}
		options.MaxRetries = -1
		options.ContextTimeoutEnabled = true
		s.batchClient = redis.NewClient(&options)
	})
	return s.batchClient
}
func (s *RedisStore) ReadSnapshot(ctx context.Context, keys []string) (map[string]Fact, error) {
	if metadata, ok := durableEventFromContext(ctx); ok {
		if s.durable == nil {
			return nil, fmt.Errorf("durable event context requires an open durable adapter")
		}
		return s.durable.readSnapshot(ctx, metadata, keys)
	}
	return s.readRedisSnapshot(ctx, keys)
}

func (s *RedisStore) readRedisSnapshot(ctx context.Context, keys []string) (map[string]Fact, error) {
	out := make(map[string]Fact, len(keys))
	if len(keys) == 0 {
		return out, ctx.Err()
	}
	values, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	size := 0
	for i, v := range values {
		if v == nil {
			out[keys[i]] = Fact{State: Missing}
			continue
		}
		raw, ok := v.(string)
		if !ok {
			out[keys[i]] = Fact{State: Invalid}
			continue
		}
		size += len(raw)
		if size > MaxSnapshotBytes {
			return nil, fmt.Errorf("snapshot exceeds byte limit")
		}
		out[keys[i]] = DecodeFact([]byte(raw))
	}
	return out, nil
}
func (s *RedisStore) Commit(ctx context.Context, request CommitRequest) (CommitResult, error) {
	result := CommitResult{Outcome: NotCommitted}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := validateWrites(request.Writes); err != nil {
		return result, err
	}
	if metadata, ok := durableEventFromContext(ctx); ok {
		if s.durable == nil {
			return result, fmt.Errorf("durable event context requires an open durable adapter")
		}
		return s.durable.commit(ctx, metadata, request)
	}
	encoded := make([][]byte, len(request.Writes))
	facts := map[string]interface{}{}
	for i, w := range request.Writes {
		var err error
		encoded[i], err = json.Marshal(w.Value)
		if err != nil {
			return result, err
		}
		facts[w.Key] = w.Value
	}
	notification, err := json.Marshal(struct {
		Metadata map[string]interface{} `json:"_rex"`
		Facts    map[string]interface{} `json:"facts"`
	}{map[string]interface{}{"trace_id": request.ChainID, "hop": request.Round + 1, "kind": "committed_output"}, facts})
	if err != nil {
		return result, err
	}
	if len(notification) > MaxEventBytes {
		return result, fmt.Errorf("output notification exceeds %d bytes", MaxEventBytes)
	}
	writer := s.batchWriter()
	for i, w := range request.Writes {
		if err := ctx.Err(); err != nil {
			if len(result.Applied) > 0 {
				result.Outcome = Partial
			}
			return result, err
		}
		// No automatic retries. Any error after dispatch may conceal an applied SET.
		if err := writer.Set(ctx, w.Key, encoded[i], 0).Err(); err != nil {
			result.Outcome = Unknown
			return result, err
		}
		result.Applied = append(result.Applied, w.Key)
	}
	if len(request.Writes) > 0 {
		if err := writer.Publish(ctx, ResultsChannel, notification).Err(); err != nil {
			result.Outcome = Partial
			return result, fmt.Errorf("writes acknowledged; notification failed: %w", err)
		}
	}
	result.Outcome = Committed
	return result, nil
}
