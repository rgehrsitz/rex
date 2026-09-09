package runtime

import (
	"context"
	"errors"
	"fmt"

	"rgehrsitz/rex/pkg/eventcontext"
	"rgehrsitz/rex/pkg/store"
)

// DurableQueue is the journal and acknowledgement boundary used by one
// ordered processor. Redis Streams is the first implementation.
type DurableQueue interface {
	Next(context.Context) (store.DurableEvent, error)
	Begin(context.Context, store.DurableEvent, string) (store.JournalStatus, error)
	ApplyInput(context.Context, string, string, map[string]interface{}) error
	Acknowledge(context.Context, string) error
	Complete(context.Context, string) error
	DeadLetterEvent(context.Context, store.DurableEvent, string, int64, error) error
	MaxAttempts() int64
}

type DurableProcessResult struct {
	EventID      string
	Attempts     int64
	Processed    bool
	RetryPending bool
	DeadLettered bool
	Recovered    bool
}

// ProcessNext processes at most one stream event. Failed events remain pending
// until their bounded attempt count sends them to the dead-letter stream.
func (e *Engine) ProcessNextDurable(ctx context.Context, queue DurableQueue) (DurableProcessResult, error) {
	result := DurableProcessResult{}
	if e.coordinator == nil || e.programID == "" {
		return result, fmt.Errorf("durable processing requires a v4 artifact")
	}
	event, receiveErr := queue.Next(ctx)
	if event.ID == "" {
		return result, receiveErr
	}
	result.EventID = event.ID
	result.Recovered = event.Recovered
	status, err := queue.Begin(ctx, event, e.programID)
	if err != nil {
		return result, err
	}
	result.Attempts = status.Attempts
	if status.Terminal != "" {
		return result, queue.Acknowledge(ctx, event.ID)
	}
	if receiveErr == nil {
		var metadata eventcontext.Metadata
		facts, decodedMetadata, _, decodeErr := eventcontext.DecodeFactEvent([]byte(event.Payload))
		if decodeErr != nil {
			receiveErr = fmt.Errorf("decode durable event: %w", decodeErr)
		} else if decodedMetadata.Kind != "" {
			receiveErr = fmt.Errorf("durable input has unsupported event kind %q", decodedMetadata.Kind)
		} else {
			metadata = decodedMetadata
			metadata.TraceID = event.ID
			metadata.Hop = 0
			if applyErr := queue.ApplyInput(ctx, event.ID, e.programID, facts); applyErr != nil {
				receiveErr = applyErr
			} else {
				eventCtx := eventcontext.WithMetadata(ctx, metadata)
				eventCtx = store.WithDurableEvent(eventCtx, event.ID, e.programID)
				_, receiveErr = e.EvaluateBatch(eventCtx, facts)
			}
		}
	}
	if receiveErr != nil {
		result.RetryPending = true
		if errors.Is(receiveErr, store.ErrDurableInfrastructure) {
			return result, receiveErr
		}
		if status.Attempts >= queue.MaxAttempts() {
			if err := queue.DeadLetterEvent(ctx, event, e.programID, status.Attempts, receiveErr); err != nil {
				return result, err
			}
			result.RetryPending = false
			result.DeadLettered = true
			return result, nil
		}
		return result, receiveErr
	}
	if err := queue.Complete(ctx, event.ID); err != nil {
		result.RetryPending = true
		return result, err
	}
	result.Processed = true
	return result, nil
}
