package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const durableProtocolVersion = "rex-m7-v1"

var ErrDurableProgramMismatch = errors.New("durable event program mismatch")
var ErrDurableOwnership = errors.New("durable partition ownership unavailable")
var ErrDurableInfrastructure = errors.New("durable processing infrastructure unavailable")
var ErrDurableReconciliation = errors.New("durable journal requires reconciliation")
var errDurablePreflight = errors.New("durable transaction preflight failed")

// DurableOptions defines one ordered Redis Streams partition and its recovery
// bounds. One active processor may own a Stream/Group pair.
type DurableOptions struct {
	Stream       string
	Group        string
	Consumer     string
	OutputStream string
	DeadLetter   string
	Namespace    string
	ClaimIdle    time.Duration
	Block        time.Duration
	JournalTTL   time.Duration
	MaxAttempts  int64
	OutputMaxLen int64
	DeadMaxLen   int64
	LockTTL      time.Duration
}

func (o DurableOptions) withDefaults() DurableOptions {
	if o.OutputStream == "" {
		o.OutputStream = "rex_results_stream"
	}
	if o.DeadLetter == "" {
		o.DeadLetter = "rex_dead_letter"
	}
	if o.Namespace == "" {
		o.Namespace = "default"
	}
	if o.ClaimIdle == 0 {
		o.ClaimIdle = 30 * time.Second
	}
	if o.Block == 0 {
		o.Block = time.Second
	}
	if o.JournalTTL == 0 {
		o.JournalTTL = 7 * 24 * time.Hour
	}
	if o.MaxAttempts == 0 {
		o.MaxAttempts = 5
	}
	if o.OutputMaxLen == 0 {
		o.OutputMaxLen = 100000
	}
	if o.DeadMaxLen == 0 {
		o.DeadMaxLen = 10000
	}
	if o.LockTTL == 0 {
		o.LockTTL = 30 * time.Second
	}
	return o
}

func (o DurableOptions) validate() error {
	if o.Stream == "" || o.Group == "" || o.Consumer == "" {
		return fmt.Errorf("durable stream, group, and consumer are required")
	}
	if o.Stream == o.OutputStream || o.Stream == o.DeadLetter || o.OutputStream == o.DeadLetter {
		return fmt.Errorf("durable input, output, and dead-letter streams must be distinct")
	}
	if strings.ContainsAny(o.Namespace, "{} \t\r\n") {
		return fmt.Errorf("durable namespace contains unsupported characters")
	}
	if o.ClaimIdle <= 0 || o.Block <= 0 || o.JournalTTL <= 0 || o.LockTTL <= 0 || o.MaxAttempts <= 0 {
		return fmt.Errorf("durable recovery limits must be positive")
	}
	if o.MaxAttempts > 1000 || o.OutputMaxLen <= 0 || o.DeadMaxLen <= 0 {
		return fmt.Errorf("durable retention or retry limit is invalid")
	}
	if o.LockTTL < 3*time.Millisecond {
		return fmt.Errorf("durable lock TTL must be at least 3ms")
	}
	return nil
}

// DurableEvent is one stable Redis Stream delivery.
type DurableEvent struct {
	ID        string
	Payload   string
	Recovered bool
}

// DurableStats exposes the bounded backlog signals available from Redis.
type DurableStats struct {
	Pending int64
	Lag     int64
}

// JournalStatus is returned before evaluation so terminal transactions can be
// acknowledged idempotently after an uncertain client reply.
type JournalStatus struct {
	Attempts int64
	Terminal string
}

// RedisDurable owns the Streams consumer-group and journal protocol.
type RedisDurable struct {
	store   *RedisStore
	client  *redis.Client
	options DurableOptions
	ownerID string
}

// OpenDurable configures the store's durable snapshot/commit behavior and
// creates the consumer group without discarding an existing stream backlog.
func (s *RedisStore) OpenDurable(ctx context.Context, options DurableOptions) (*RedisDurable, error) {
	options = options.withDefaults()
	if err := options.validate(); err != nil {
		return nil, err
	}
	ownerBytes := make([]byte, 16)
	if _, err := rand.Read(ownerBytes); err != nil {
		return nil, fmt.Errorf("create durable owner identity: %w", err)
	}
	durable := &RedisDurable{store: s, client: s.batchWriter(), options: options, ownerID: hex.EncodeToString(ownerBytes)}
	if err := durable.client.XGroupCreateMkStream(ctx, options.Stream, options.Group, "0").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return nil, fmt.Errorf("create durable consumer group: %w", err)
	}
	s.durable = durable
	return durable, nil
}

func (d *RedisDurable) ownerKey() string {
	return "rex:durable:" + d.options.Namespace + ":owner"
}

// AcquireOwnership establishes the single active owner required by an ordered
// stream partition. The lease must be renewed while processing.
func (d *RedisDurable) AcquireOwnership(ctx context.Context) error {
	acquired, err := d.client.SetNX(ctx, d.ownerKey(), d.ownerID, d.options.LockTTL).Result()
	if err != nil {
		return fmt.Errorf("acquire durable ownership: %w", err)
	}
	if !acquired {
		return ErrDurableOwnership
	}
	return nil
}

// RenewOwnership extends the lease only while this instance still owns it.
func (d *RedisDurable) RenewOwnership(ctx context.Context) error {
	err := d.client.Watch(ctx, func(tx *redis.Tx) error {
		owner, err := tx.Get(ctx, d.ownerKey()).Result()
		if err != nil || owner != d.ownerID {
			return ErrDurableOwnership
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.PExpire(ctx, d.ownerKey(), d.options.LockTTL)
			return nil
		})
		return err
	}, d.ownerKey())
	if errors.Is(err, redis.TxFailedErr) {
		return ErrDurableOwnership
	}
	return err
}

// ReleaseOwnership removes this instance's lease without deleting a successor's.
func (d *RedisDurable) ReleaseOwnership(ctx context.Context) error {
	err := d.client.Watch(ctx, func(tx *redis.Tx) error {
		owner, err := tx.Get(ctx, d.ownerKey()).Result()
		if err == redis.Nil {
			return nil
		}
		if err != nil || owner != d.ownerID {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Del(ctx, d.ownerKey())
			return nil
		})
		return err
	}, d.ownerKey())
	if errors.Is(err, redis.TxFailedErr) {
		return nil
	}
	return err
}

func (d *RedisDurable) OwnershipRenewInterval() time.Duration { return d.options.LockTTL / 3 }

func (d *RedisDurable) journalKey(eventID string) string {
	return "rex:durable:" + d.options.Namespace + ":event:" + eventID
}

func (d *RedisDurable) validateFactKey(key string) error {
	if key == "" {
		return fmt.Errorf("durable fact key is empty")
	}
	if key == d.options.Stream || key == d.options.OutputStream || key == d.options.DeadLetter || strings.HasPrefix(key, "rex:durable:") {
		return fmt.Errorf("durable fact key %q is reserved by the processing protocol", key)
	}
	return nil
}

// Next returns the oldest recoverable pending event before reading new work.
func (d *RedisDurable) Next(ctx context.Context) (DurableEvent, error) {
	if messages, err := d.readGroup(ctx, "0", 0); err != nil {
		return DurableEvent{}, err
	} else if len(messages) > 0 {
		event, err := durableEvent(messages[0])
		event.Recovered = true
		return event, err
	}
	pending, err := d.client.XPending(ctx, d.options.Stream, d.options.Group).Result()
	if err != nil {
		return DurableEvent{}, fmt.Errorf("inspect durable pending work: %w", err)
	}
	if pending.Count > 0 {
		messages, _, err := d.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream: d.options.Stream, Group: d.options.Group, Consumer: d.options.Consumer,
			MinIdle: d.options.ClaimIdle, Start: "0-0", Count: 1,
		}).Result()
		if err != nil && err != redis.Nil {
			return DurableEvent{}, fmt.Errorf("claim durable pending work: %w", err)
		}
		if len(messages) > 0 {
			event, err := durableEvent(messages[0])
			event.Recovered = true
			return event, err
		}
		timer := time.NewTimer(minDuration(d.options.Block, d.options.ClaimIdle))
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return DurableEvent{}, ctx.Err()
		case <-timer.C:
			return DurableEvent{}, nil
		}
	}
	messages, err := d.readGroup(ctx, ">", d.options.Block)
	if err != nil {
		return DurableEvent{}, err
	}
	if len(messages) == 0 {
		return DurableEvent{}, nil
	}
	return durableEvent(messages[0])
}

func (d *RedisDurable) readGroup(ctx context.Context, id string, block time.Duration) ([]redis.XMessage, error) {
	streams, err := d.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: d.options.Group, Consumer: d.options.Consumer,
		Streams: []string{d.options.Stream, id}, Count: 1, Block: block,
	}).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read durable stream: %w", err)
	}
	if len(streams) == 0 {
		return nil, nil
	}
	return streams[0].Messages, nil
}

func durableEvent(message redis.XMessage) (DurableEvent, error) {
	payload, ok := message.Values["payload"].(string)
	if !ok {
		return DurableEvent{ID: message.ID}, fmt.Errorf("durable event %s has no string payload", message.ID)
	}
	if len(payload) > MaxEventBytes {
		return DurableEvent{ID: message.ID, Payload: payload}, fmt.Errorf("durable event exceeds %d bytes", MaxEventBytes)
	}
	return DurableEvent{ID: message.ID, Payload: payload}, nil
}

// Begin records and verifies immutable event identity, pins the program, and
// increments the bounded poison-attempt counter.
func (d *RedisDurable) Begin(ctx context.Context, event DurableEvent, programID string) (JournalStatus, error) {
	key := d.journalKey(event.ID)
	values, err := d.client.HMGet(ctx, key, "payload", "program_id", "terminal", "attempts").Result()
	if err != nil {
		return JournalStatus{}, fmt.Errorf("%w: read event journal: %w", ErrDurableInfrastructure, err)
	}
	if valueString(values[0]) != "" && valueString(values[0]) != event.Payload {
		return JournalStatus{}, fmt.Errorf("%w: durable event %s payload differs from its journal", ErrDurableReconciliation, event.ID)
	}
	if valueString(values[1]) != "" && valueString(values[1]) != programID {
		return JournalStatus{}, fmt.Errorf("%w: event %s requires program %s", ErrDurableProgramMismatch, event.ID, valueString(values[1]))
	}
	if terminal := valueString(values[2]); terminal != "" {
		attempts, _ := strconv.ParseInt(valueString(values[3]), 10, 64)
		return JournalStatus{Attempts: attempts, Terminal: terminal}, nil
	}
	pipe := d.client.TxPipeline()
	pipe.HSetNX(ctx, key, "payload", event.Payload)
	pipe.HSetNX(ctx, key, "program_id", programID)
	attempts := pipe.HIncrBy(ctx, key, "attempts", 1)
	pipe.PExpire(ctx, key, d.options.JournalTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return JournalStatus{}, fmt.Errorf("%w: begin durable event: %w", ErrDurableInfrastructure, err)
	}
	return JournalStatus{Attempts: attempts.Val(), Terminal: valueString(values[2])}, nil
}

// Acknowledge removes a terminal event from the consumer group's pending list.
func (d *RedisDurable) Acknowledge(ctx context.Context, eventID string) error {
	if err := d.client.XAck(ctx, d.options.Stream, d.options.Group, eventID).Err(); err != nil {
		return fmt.Errorf("%w: acknowledge durable event: %w", ErrDurableInfrastructure, err)
	}
	return nil
}

// Complete atomically records terminal success and acknowledges the input.
func (d *RedisDurable) Complete(ctx context.Context, eventID string) error {
	key := d.journalKey(eventID)
	err := d.client.Watch(ctx, func(tx *redis.Tx) error {
		terminal, err := tx.HGet(ctx, key, "terminal").Result()
		if err == nil && terminal != "completed" {
			return fmt.Errorf("%w: durable event %s is already %s", ErrDurableReconciliation, eventID, terminal)
		}
		if err != nil && err != redis.Nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			if terminal == "" {
				pipe.HSet(ctx, key, "terminal", "completed")
				pipe.PExpire(ctx, key, d.options.JournalTTL)
			}
			pipe.XAck(ctx, d.options.Stream, d.options.Group, eventID)
			return nil
		})
		return err
	}, key)
	if err != nil {
		return fmt.Errorf("%w: complete durable event: %w", ErrDurableInfrastructure, err)
	}
	return nil
}

// DeadLetter atomically records poison termination, appends one repair record,
// and acknowledges the original input. A terminal marker prevents duplicates.
func (d *RedisDurable) DeadLetterEvent(ctx context.Context, event DurableEvent, programID string, attempts int64, processErr error) error {
	key := d.journalKey(event.ID)
	id := stableDurableID("dead", d.options.Namespace, event.ID, programID)
	err := d.client.Watch(ctx, func(tx *redis.Tx) error {
		terminal, err := tx.HGet(ctx, key, "terminal").Result()
		if err == nil {
			if terminal == "dead_lettered" {
				_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
					pipe.XAck(ctx, d.options.Stream, d.options.Group, event.ID)
					return nil
				})
				return err
			}
			if terminal != "" {
				return fmt.Errorf("%w: durable event %s is already %s", ErrDurableReconciliation, event.ID, terminal)
			}
		} else if err != redis.Nil {
			return err
		}
		kind, err := tx.Type(ctx, d.options.DeadLetter).Result()
		if err != nil {
			return err
		}
		if kind != "none" && kind != "stream" {
			return fmt.Errorf("%w: dead-letter key %q has type %s", errDurablePreflight, d.options.DeadLetter, kind)
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.XAdd(ctx, &redis.XAddArgs{Stream: d.options.DeadLetter, MaxLen: d.options.DeadMaxLen, Approx: true, Values: map[string]interface{}{
				"dead_letter_id": id, "input_id": event.ID, "payload": event.Payload,
				"input_stream": d.options.Stream, "namespace": d.options.Namespace,
				"program_id": programID, "attempts": attempts, "error": processErr.Error(),
			}})
			pipe.HSet(ctx, key, "terminal", "dead_lettered", "dead_letter_id", id)
			pipe.PExpire(ctx, key, d.options.JournalTTL)
			pipe.XAck(ctx, d.options.Stream, d.options.Group, event.ID)
			return nil
		})
		return err
	}, key, d.options.DeadLetter)
	if err != nil {
		return fmt.Errorf("%w: dead-letter durable event: %w", ErrDurableInfrastructure, err)
	}
	return nil
}

// Stats returns the consumer group's pending and undelivered counts.
func (d *RedisDurable) Stats(ctx context.Context) (DurableStats, error) {
	groups, err := d.client.XInfoGroups(ctx, d.options.Stream).Result()
	if err != nil {
		return DurableStats{}, err
	}
	for _, group := range groups {
		if group.Name == d.options.Group {
			return DurableStats{Pending: group.Pending, Lag: group.Lag}, nil
		}
	}
	return DurableStats{}, fmt.Errorf("durable consumer group %q not found", d.options.Group)
}

func (d *RedisDurable) MaxAttempts() int64 { return d.options.MaxAttempts }

// ApplyInput atomically installs the event's fact updates and records that the
// input phase completed. A retry checks the marker before writing again.
func (d *RedisDurable) ApplyInput(ctx context.Context, eventID, programID string, facts map[string]interface{}) error {
	journal := d.journalKey(eventID)
	encoded := make(map[string][]byte, len(facts))
	for key, value := range facts {
		if err := d.validateFactKey(key); err != nil {
			return err
		}
		valueBytes, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode durable input fact %q: %w", key, err)
		}
		encoded[key] = valueBytes
	}
	marker := stableDurableID("input", d.options.Namespace, eventID, programID)
	for attempt := 0; attempt < 3; attempt++ {
		err := d.client.Watch(ctx, func(tx *redis.Tx) error {
			stored, err := tx.HGet(ctx, journal, "input_commit").Result()
			if err == nil {
				if stored != marker {
					return fmt.Errorf("%w: durable input marker mismatch for %s", ErrDurableReconciliation, eventID)
				}
				return nil
			}
			if err != redis.Nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				for key, value := range encoded {
					pipe.Set(ctx, key, value, 0)
				}
				pipe.HSet(ctx, journal, "input_commit", marker)
				pipe.PExpire(ctx, journal, d.options.JournalTTL)
				return nil
			})
			return err
		}, journal)
		if err == nil {
			return nil
		}
		if !errors.Is(err, redis.TxFailedErr) {
			return fmt.Errorf("%w: input transaction outcome unknown: %w", ErrDurableInfrastructure, err)
		}
	}
	return fmt.Errorf("%w: input transaction contention limit exceeded", ErrDurableInfrastructure)
}

func (d *RedisDurable) readSnapshot(ctx context.Context, metadata durableEventContext, keys []string) (map[string]Fact, error) {
	field := "snapshot:" + strconv.Itoa(metadata.Round)
	key := d.journalKey(metadata.EventID)
	if encoded, err := d.client.HGet(ctx, key, field).Bytes(); err == nil {
		return decodeSnapshot(encoded)
	} else if err != redis.Nil {
		return nil, fmt.Errorf("%w: read historical snapshot: %w", ErrDurableInfrastructure, err)
	}
	snapshot, err := d.store.readRedisSnapshot(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("%w: read snapshot: %w", ErrDurableInfrastructure, err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode historical snapshot: %w", err)
	}
	stored, err := d.client.HSetNX(ctx, key, field, encoded).Result()
	if err != nil {
		return nil, fmt.Errorf("%w: record historical snapshot: %w", ErrDurableInfrastructure, err)
	}
	if stored {
		_ = d.client.PExpire(ctx, key, d.options.JournalTTL).Err()
		return snapshot, nil
	}
	encoded, err = d.client.HGet(ctx, key, field).Bytes()
	if err != nil {
		return nil, fmt.Errorf("%w: recover historical snapshot: %w", ErrDurableInfrastructure, err)
	}
	return decodeSnapshot(encoded)
}

func decodeSnapshot(encoded []byte) (map[string]Fact, error) {
	var snapshot map[string]Fact
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return nil, fmt.Errorf("%w: decode durable snapshot: %w", ErrDurableReconciliation, err)
	}
	return snapshot, nil
}

func (d *RedisDurable) commit(ctx context.Context, metadata durableEventContext, request CommitRequest) (CommitResult, error) {
	failed := CommitResult{Outcome: NotCommitted}
	if metadata.Round != request.Round {
		return failed, fmt.Errorf("durable round context %d does not match commit %d", metadata.Round, request.Round)
	}
	if err := validateWrites(request.Writes); err != nil {
		return failed, err
	}
	journal := d.journalKey(metadata.EventID)
	field := "commit:" + strconv.Itoa(request.Round)
	if existing, err := d.client.HGet(ctx, journal, field).Bytes(); err == nil {
		var result CommitResult
		if err := json.Unmarshal(existing, &result); err != nil {
			return CommitResult{Outcome: Unknown}, fmt.Errorf("%w: decode durable commit marker: %w", ErrDurableReconciliation, err)
		}
		return result, nil
	} else if err != redis.Nil {
		return CommitResult{Outcome: Unknown}, fmt.Errorf("%w: read commit marker: %w", ErrDurableInfrastructure, err)
	}

	encoded := make([][]byte, len(request.Writes))
	facts := make(map[string]interface{}, len(request.Writes))
	writeIDs := make(map[string]string, len(request.Writes))
	for i, write := range request.Writes {
		if err := d.validateFactKey(write.Key); err != nil {
			return failed, err
		}
		var err error
		encoded[i], err = json.Marshal(write.Value)
		if err != nil {
			return failed, err
		}
		facts[write.Key] = write.Value
		writeIDs[write.Key] = stableDurableID("write", d.options.Namespace, metadata.EventID, metadata.ProgramID, strconv.Itoa(request.Round), write.Key)
	}
	actionIDs := make([]string, len(request.Actions))
	for i, action := range request.Actions {
		actionIDs[i] = stableDurableID("action", d.options.Namespace, metadata.EventID, metadata.ProgramID, strconv.Itoa(request.Round), action.Rule, strconv.Itoa(action.Index), action.Target)
	}
	outputID := stableDurableID("output", d.options.Namespace, metadata.EventID, metadata.ProgramID, strconv.Itoa(request.Round))
	notification, err := json.Marshal(struct {
		Metadata  map[string]interface{} `json:"_rex"`
		Facts     map[string]interface{} `json:"facts"`
		WriteIDs  map[string]string      `json:"write_ids"`
		ActionIDs []string               `json:"action_ids"`
	}{map[string]interface{}{
		"trace_id": request.ChainID, "hop": request.Round + 1, "kind": "committed_output",
		"input_id": metadata.EventID, "input_stream": d.options.Stream, "namespace": d.options.Namespace,
		"program_id": metadata.ProgramID, "output_id": outputID,
	}, facts, writeIDs, actionIDs})
	if err != nil {
		return failed, err
	}
	if len(notification) > MaxEventBytes {
		return failed, fmt.Errorf("durable output notification exceeds %d bytes", MaxEventBytes)
	}
	result := CommitResult{Outcome: Committed, Applied: make([]string, len(request.Writes))}
	for i, write := range request.Writes {
		result.Applied[i] = write.Key
	}
	marker, err := json.Marshal(result)
	if err != nil {
		return failed, err
	}

	for attempt := 0; attempt < 3; attempt++ {
		err = d.client.Watch(ctx, func(tx *redis.Tx) error {
			if value, err := tx.HGet(ctx, journal, field).Bytes(); err == nil {
				var committed CommitResult
				if err := json.Unmarshal(value, &committed); err != nil {
					return fmt.Errorf("%w: decode durable commit marker: %w", ErrDurableReconciliation, err)
				}
				result = committed
				return nil
			} else if err != redis.Nil {
				return err
			}
			kind, err := tx.Type(ctx, d.options.OutputStream).Result()
			if err != nil {
				return err
			}
			if kind != "none" && kind != "stream" {
				return fmt.Errorf("%w: output key %q has type %s", errDurablePreflight, d.options.OutputStream, kind)
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				for i, write := range request.Writes {
					pipe.Set(ctx, write.Key, encoded[i], 0)
				}
				pipe.XAdd(ctx, &redis.XAddArgs{Stream: d.options.OutputStream, MaxLen: d.options.OutputMaxLen, Approx: true, Values: map[string]interface{}{
					"protocol": durableProtocolVersion, "input_id": metadata.EventID,
					"input_stream": d.options.Stream, "namespace": d.options.Namespace,
					"program_id": metadata.ProgramID, "round": request.Round, "output_id": outputID, "payload": notification,
				}})
				pipe.HSet(ctx, journal, field, marker)
				pipe.PExpire(ctx, journal, d.options.JournalTTL)
				return nil
			})
			return err
		}, journal, d.options.OutputStream)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, redis.TxFailedErr) {
			if errors.Is(err, errDurablePreflight) {
				return failed, fmt.Errorf("%w: %w", ErrDurableInfrastructure, err)
			}
			return CommitResult{Outcome: Unknown}, fmt.Errorf("%w: durable transaction outcome unknown: %w", ErrDurableInfrastructure, err)
		}
	}
	return CommitResult{Outcome: NotCommitted}, fmt.Errorf("%w: durable transaction contention limit exceeded", ErrDurableInfrastructure)
}

// ResolvesUnknownOnRetry reports whether this store has durable commit markers.
func (s *RedisStore) ResolvesUnknownOnRetry() bool { return s.durable != nil }

func stableDurableID(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(part))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func valueString(value interface{}) string {
	text, _ := value.(string)
	return text
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
