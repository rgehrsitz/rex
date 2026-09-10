package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/redis/go-redis/v9"
)

// Claims intentionally outlive leases and journals. Removing a claim is an
// offline administrative migration, never an automatic crash-recovery action.
const ownershipRegistry = "rex:durable:ownership"
const maxOwnershipPartitions = 256

type ownershipClaim struct {
	Facts []string `json:"facts"`
	Keys  []string `json:"keys"`
	Group string   `json:"group"`
}

func (d *RedisDurable) claim() string { return d.claimJSON }
func (d *RedisDurable) encodeClaim() string {
	b, _ := json.Marshal(ownershipClaim{Facts: d.ownership.Names(), Keys: []string{d.options.Stream, d.options.OutputStream, d.options.DeadLetter}, Group: d.options.Group})
	return string(b)
}
func (d *RedisDurable) ValidateProgramFacts(programID string, keys []string) error {
	if d.ownership == nil {
		return nil
	}
	d.validationMu.Lock()
	defer d.validationMu.Unlock()
	if d.validatedPrograms[programID] {
		return nil
	}
	for _, key := range keys {
		if err := d.validateFactKey(key); err != nil {
			return err
		}
	}
	// Keep memory bounded without evicting known-good entries. After saturation,
	// new artifacts are revalidated on each delivery; correctness is unchanged.
	if len(d.validatedPrograms) < 1024 {
		if d.validatedPrograms == nil {
			d.validatedPrograms = map[string]bool{}
		}
		d.validatedPrograms[programID] = true
	}
	return nil
}
func (d *RedisDurable) acquireFactOwnership(ctx context.Context) error {
	return d.reserveFactOwnership(ctx, true)
}

// The colon field cannot collide with a validated namespace.
const legacyOwnershipMarker = ":"

func (d *RedisDurable) reserveFactOwnership(ctx context.Context, acquire bool) error {
	for attempt := 0; attempt < 8; attempt++ {
		err := d.client.Watch(ctx, func(tx *redis.Tx) error {
			count, err := tx.HLen(ctx, ownershipRegistry).Result()
			if err != nil {
				return err
			}
			if count > maxOwnershipPartitions {
				return fmt.Errorf("ownership registry exceeds partition limit")
			}
			claims, err := tx.HGetAll(ctx, ownershipRegistry).Result()
			if err != nil {
				return err
			}
			field, candidate := d.options.Namespace, d.claim()
			if d.ownership == nil {
				if len(claims) > 0 && (len(claims) != 1 || claims[legacyOwnershipMarker] != "legacy") {
					return fmt.Errorf("managed and legacy partitions cannot share a Redis commit domain")
				}
				field, candidate = legacyOwnershipMarker, "legacy"
			} else {
				if _, legacy := claims[legacyOwnershipMarker]; legacy {
					return fmt.Errorf("managed and legacy partitions cannot share a Redis commit domain")
				}
				previous, exists := claims[field]
				if exists && previous != candidate {
					return fmt.Errorf("partition ownership is immutable; offline migration required")
				}
				if !exists && len(claims) >= maxOwnershipPartitions {
					return fmt.Errorf("ownership registry partition limit reached")
				}
				if len(candidate) > 65536 {
					return fmt.Errorf("ownership claim exceeds 64 KiB")
				}
				if !exists {
					if err := tx.Watch(ctx, d.options.Stream, d.options.OutputStream, d.options.DeadLetter).Err(); err != nil {
						return err
					}
					owner, err := tx.Get(ctx, d.ownerKey()).Result()
					if err != nil && err != redis.Nil {
						return err
					}
					if err == nil && owner != d.ownerID {
						return ErrDurableOwnership
					}
					pending, err := tx.XPending(ctx, d.options.Stream, d.options.Group).Result()
					if err != nil && !redis.HasErrorPrefix(err, "NOGROUP ") && !redis.HasErrorPrefix(err, "no such key") {
						return err
					}
					if err == nil && pending.Count > 0 {
						return fmt.Errorf("cannot enable ownership with pending events")
					}
				}
				wanted := map[string]bool{}
				for _, key := range d.ownership.Names() {
					wanted[key] = true
				}
				for _, key := range []string{d.options.Stream, d.options.OutputStream, d.options.DeadLetter} {
					if wanted[key] {
						return fmt.Errorf("protocol key %q overlaps owned fact", key)
					}
					wanted[key] = true
				}
				namespaces := make([]string, 0, len(claims))
				for namespace := range claims {
					namespaces = append(namespaces, namespace)
				}
				sort.Strings(namespaces)
				for _, namespace := range namespaces {
					var other ownershipClaim
					if err := json.Unmarshal([]byte(claims[namespace]), &other); err != nil || other.Facts == nil {
						return fmt.Errorf("invalid ownership claim for %q", namespace)
					}
					if namespace == field {
						continue
					}
					for _, key := range append(other.Facts, other.Keys...) {
						if wanted[key] {
							return fmt.Errorf("ownership overlap with partition %q on %q", namespace, key)
						}
					}
				}
				// Avoid leaving a reservation for a predictable configuration/type failure.
				for _, key := range []string{d.options.Stream, d.options.OutputStream, d.options.DeadLetter} {
					kind, err := tx.Type(ctx, key).Result()
					if err != nil {
						return err
					}
					if kind != "none" && kind != "stream" {
						return fmt.Errorf("protocol key %q has type %s", key, kind)
					}
				}
			}
			if acquire {
				owner, err := tx.Get(ctx, d.ownerKey()).Result()
				if err != nil && err != redis.Nil {
					return err
				}
				if err == nil && owner != d.ownerID {
					return ErrDurableOwnership
				}
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				// An idempotent restart must not abort other partitions watching the registry.
				if claims[field] != candidate {
					pipe.HSet(ctx, ownershipRegistry, field, candidate)
				}
				if acquire {
					pipe.Set(ctx, d.ownerKey(), d.ownerID, d.options.LockTTL)
				}
				return nil
			})
			return err
		}, ownershipRegistry, d.ownerKey())
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
	}
	return fmt.Errorf("ownership claim contention limit exceeded")
}
func (d *RedisDurable) ownershipWatchKeys(keys ...string) []string {
	if d.ownership != nil {
		keys = append(keys, d.ownerKey(), ownershipRegistry)
	}
	return keys
}
func (d *RedisDurable) checkOwnershipFence(ctx context.Context, tx *redis.Tx) error {
	if d.ownership == nil {
		return nil
	}
	owner, err := tx.Get(ctx, d.ownerKey()).Result()
	if err == redis.Nil {
		return ErrDurableOwnership
	}
	if err != nil {
		return err
	}
	if owner != d.ownerID {
		return ErrDurableOwnership
	}
	claim, err := tx.HGet(ctx, ownershipRegistry, d.options.Namespace).Result()
	if err == redis.Nil {
		return ErrDurableOwnership
	}
	if err != nil {
		return err
	}
	if claim != d.claim() {
		return ErrDurableOwnership
	}
	return nil
}

// Retry managed optimistic conflicts, including this process renewing its own
// lease. Every attempt re-runs the caller's fence before issuing effects.
func (d *RedisDurable) watchOwnership(ctx context.Context, fn func(*redis.Tx) error, keys ...string) error {
	attempts := 1
	if d.ownership != nil {
		attempts = 8
	}
	for i := 0; i < attempts; i++ {
		err := d.client.Watch(ctx, fn, keys...)
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return fmt.Errorf("ownership transaction contention limit exceeded: %w", redis.TxFailedErr)
}
