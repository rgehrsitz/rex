package store

import (
	"context"
	"fmt"
	"sync"
)

const MaxEventBytes = 1 << 20

// Event is transport-neutral. Err reports a rejected transport payload.
type Event struct {
	Channel string
	Payload string
	Err     error
}
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
}

func (s *redisEventSource) Events() <-chan Event { return s.events }
func (s *redisEventSource) Close() error         { s.once.Do(s.cancel); <-s.done; return nil }
func (s *RedisStore) OpenEvents(ctx context.Context, channels ...string) (EventSource, error) {
	ctx, cancel := context.WithCancel(ctx)
	subscription, err := s.Subscribe(ctx, channels...)
	if err != nil {
		cancel()
		return nil, err
	}
	source := &redisEventSource{events: make(chan Event), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(source.done)
		defer close(source.events)
		defer subscription.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-subscription.Channel():
				if !ok {
					return
				}
				if msg == nil {
					continue
				}
				event := Event{Channel: msg.Channel, Payload: msg.Payload}
				if len(msg.Payload) > MaxEventBytes {
					event.Payload = ""
					event.Err = fmt.Errorf("event exceeds %d bytes", MaxEventBytes)
				}
				select {
				case source.events <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return source, nil
}
