package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// VersionedDurableQueue exposes the program pinned by a previous delivery so
// an EngineManager can route recovery to the exact historical artifact.
type VersionedDurableQueue interface {
	DurableQueue
	PinnedProgram(context.Context, string) (string, error)
}

// ProcessingTimeDurableQueue pins the first processing-time sample for a
// durable event so every retry evaluates temporal conditions at the same time.
type ProcessingTimeDurableQueue interface {
	DurableQueue
	PinProcessingTime(context.Context, string, time.Time) (time.Time, error)
}

// ProgramFactValidator checks a pinned artifact against queue ownership before
// Begin. A failure leaves the event pending as an infrastructure problem.
type ProgramFactValidator interface {
	ValidateProgramFacts(programID string, facts []string) error
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
	if e.coordinator == nil || e.programID == "" {
		return DurableProcessResult{}, fmt.Errorf("durable processing requires a batch artifact")
	}
	event, receiveErr := queue.Next(ctx)
	if event.ID == "" {
		return DurableProcessResult{}, receiveErr
	}
	return e.processDurableEvent(ctx, queue, event, receiveErr)
}

func (e *Engine) processDurableEvent(ctx context.Context, queue DurableQueue, event store.DurableEvent, receiveErr error) (DurableProcessResult, error) {
	result := DurableProcessResult{}
	result.EventID = event.ID
	result.Recovered = event.Recovered
	if validator, ok := queue.(ProgramFactValidator); ok {
		if err := validator.ValidateProgramFacts(e.programID, e.coordinator.program.ownershipFacts); err != nil {
			result.RetryPending = true
			return result, errors.Join(store.ErrDurableInfrastructure, err)
		}
	}
	status, err := queue.Begin(ctx, event, e.programID)
	if err != nil {
		return result, err
	}
	result.Attempts = status.Attempts
	if status.Terminal != "" {
		return result, queue.Acknowledge(ctx, event.ID)
	}
	processingTime := time.Time{}
	if e.HasTemporalConditions() {
		temporalQueue, ok := queue.(ProcessingTimeDurableQueue)
		if !ok {
			result.RetryPending = true
			return result, errors.Join(store.ErrDurableInfrastructure, fmt.Errorf("temporal durable processing requires a processing-time journal"))
		}
		proposed, sampleErr := e.coordinator.processingTime()
		if sampleErr != nil {
			result.RetryPending = true
			return result, errors.Join(store.ErrDurableInfrastructure, sampleErr)
		}
		processingTime, err = temporalQueue.PinProcessingTime(ctx, event.ID, proposed)
		if err != nil {
			result.RetryPending = true
			return result, err
		}
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
			if validationErr := e.ValidateBatchEvent(facts); validationErr != nil {
				receiveErr = fmt.Errorf("validate durable event: %w", validationErr)
			} else if validationErr := e.validateDurableTargets(facts); validationErr != nil {
				receiveErr = validationErr
			} else if applyErr := queue.ApplyInput(ctx, event.ID, e.programID, facts); applyErr != nil {
				receiveErr = applyErr
			} else {
				eventCtx := eventcontext.WithMetadata(ctx, metadata)
				eventCtx = store.WithDurableEvent(eventCtx, event.ID, e.programID)
				_, receiveErr = e.evaluateBatch(eventCtx, facts, processingTime)
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

// Durable inputs are persisted before snapshots, so they must not overwrite
// targets whose prior persisted value determines change-only suppression.
func (e *Engine) validateDurableTargets(facts map[string]interface{}) error {
	program := e.coordinator.program
	if !program.HasChangeOnly() {
		return nil
	}
	rejected := ""
	for key := range facts {
		if program.changeTargetSet[key] && (rejected == "" || key < rejected) {
			rejected = key
		}
	}
	if rejected != "" {
		return fmt.Errorf("durable event includes change-only target %q", rejected)
	}
	return nil
}
