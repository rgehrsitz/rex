package store

import "context"

type durableEventContext struct {
	EventID   string
	ProgramID string
	Round     int
}

type durableEventContextKey struct{}

// WithDurableEvent identifies one stream delivery for journaled snapshot and
// commit operations. ProgramID pins retries to the same compiled artifact.
func WithDurableEvent(ctx context.Context, eventID, programID string) context.Context {
	return context.WithValue(ctx, durableEventContextKey{}, durableEventContext{EventID: eventID, ProgramID: programID})
}

// WithEvaluationRound identifies the coordinator round whose dependency
// snapshot is being read or committed.
func WithEvaluationRound(ctx context.Context, round int) context.Context {
	metadata, ok := ctx.Value(durableEventContextKey{}).(durableEventContext)
	if !ok {
		return ctx
	}
	metadata.Round = round
	return context.WithValue(ctx, durableEventContextKey{}, metadata)
}

func durableEventFromContext(ctx context.Context) (durableEventContext, bool) {
	metadata, ok := ctx.Value(durableEventContextKey{}).(durableEventContext)
	return metadata, ok && metadata.EventID != "" && metadata.ProgramID != ""
}
