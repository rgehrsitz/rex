package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const MaxEventBytes = 1 << 20

// Event is transport-neutral. Err reports a rejected transport payload.
type Event struct {
	Channel string
	Payload string
	Err     error
	State   SubscriptionState
}

// SubscriptionState marks explicit transport state transitions. The empty
// value identifies ordinary data and payload-rejection events.
type SubscriptionState string

const (
	SubscriptionDisconnected SubscriptionState = "disconnected"
	SubscriptionConnected    SubscriptionState = "connected"
)

type EventSource interface {
	Events() <-chan Event
	Close() error
}
type EventSubscriber interface {
	OpenEvents(context.Context, ...string) (EventSource, error)
}
type redisEventSource struct {
	events chan Event
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
	pubsub *redis.PubSub
}

func (s *redisEventSource) Events() <-chan Event { return s.events }
func (s *redisEventSource) Close() error {
	s.once.Do(func() {
		s.cancel()
		_ = s.pubsub.Close()
	})
	<-s.done
	return nil
}
func (s *RedisStore) OpenEvents(ctx context.Context, channels ...string) (EventSource, error) {
	ctx, cancel := context.WithCancel(ctx)
	subscription, err := s.Subscribe(ctx, channels...)
	if err != nil {
		cancel()
		return nil, err
	}
	source := &redisEventSource{events: make(chan Event), cancel: cancel, done: make(chan struct{}), pubsub: subscription}
	go func() {
		defer close(source.done)
		defer close(source.events)
		defer subscription.Close()
		failed := false
		for {
			received, err := subscription.Receive(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if !failed && !source.send(ctx, Event{Err: fmt.Errorf("receive Redis event: %w", err), State: SubscriptionDisconnected}) {
					return
				}
				failed = true
				timer := time.NewTimer(100 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				continue
			}
			switch value := received.(type) {
			case *redis.Subscription:
				if failed && value.Count > 0 {
					if !source.send(ctx, Event{State: SubscriptionConnected}) {
						return
					}
					failed = false
				}
			case *redis.Message:
				failed = false
				event := Event{Channel: value.Channel, Payload: value.Payload}
				if len(value.Payload) > MaxEventBytes {
					event.Payload = ""
					event.Err = fmt.Errorf("event exceeds %d bytes", MaxEventBytes)
				}
				if !source.send(ctx, event) {
					return
				}
			}
		}
	}()
	return source, nil
}

func (s *redisEventSource) send(ctx context.Context, event Event) bool {
	select {
	case s.events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}
