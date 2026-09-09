package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/eventcontext"
	"rgehrsitz/rex/pkg/store"
)

func newBatchEngine(data []byte, backend store.ContextStore) (*Engine, error) {
	program, err := LoadProgram(data)
	if err != nil {
		return nil, err
	}
	reader, ok := backend.(store.SnapshotReader)
	if !ok {
		return nil, fmt.Errorf("batch execution requires SnapshotReader adapter")
	}
	writer, ok := backend.(store.Committer)
	if !ok {
		return nil, fmt.Errorf("batch execution requires Committer adapter")
	}
	coordinator, err := NewCoordinator(program, reader, writer, DefaultLimits())
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	return &Engine{coordinator: coordinator, traceConditions: true, maxActionsPerEvaluation: DefaultMaxActionsPerEvaluation, programID: hex.EncodeToString(digest[:])}, nil
}

// ProgramID is the stable digest used to pin durable retries to one artifact.
func (e *Engine) ProgramID() string { return e.programID }
func (e *Engine) BytecodeVersion() uint32 {
	if e.coordinator != nil {
		return e.coordinator.program.Version()
	}
	if len(e.bytecode) >= 4 {
		return binary.LittleEndian.Uint32(e.bytecode)
	}
	return 0
}

// Snapshot replaces mutable Engine.Facts. Batch execution retains no fact state; inspect the
// explicit ChainResult returned by EvaluateBatch instead. V3 returns a copy.
func (e *Engine) Snapshot() map[string]interface{} {
	out := make(map[string]interface{}, len(e.facts))
	for k, v := range e.facts {
		out[k] = copyFactValue(v)
	}
	return out
}
func copyFactValue(v interface{}) interface{} {
	switch value := v.(type) {
	case map[string]interface{}:
		out := map[string]interface{}{}
		for k, x := range value {
			out[k] = copyFactValue(x)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(value))
		for i, x := range value {
			out[i] = copyFactValue(x)
		}
		return out
	default:
		return v
	}
}

// SetBatchLimits is initialization-only; batch execution never permits unbounded limits.
func (e *Engine) SetBatchLimits(limits Limits) error {
	if e.coordinator == nil {
		return fmt.Errorf("batch limits require a batch artifact")
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	e.coordinator.limits = limits
	e.maxActionsPerEvaluation = limits.ActionsPerRule
	return nil
}
func (e *Engine) EvaluateBatch(ctx context.Context, event map[string]interface{}) (ChainResult, error) {
	if e.coordinator == nil {
		return ChainResult{}, fmt.Errorf("batch evaluation requires a batch artifact")
	}
	metadata, _ := eventcontext.MetadataFromContext(ctx)
	if metadata.Kind == "committed_output" {
		return ChainResult{ChainID: metadata.TraceID}, nil
	}
	if metadata.Kind != "" {
		return ChainResult{}, fmt.Errorf("unsupported event kind %q", metadata.Kind)
	}
	result, err := e.coordinator.Process(ctx, metadata.TraceID, event)
	logger := traceLogger(ctx)
	for _, round := range result.Rounds {
		for _, condition := range round.Evaluation.Conditions {
			logger.Info().Str("event", "rule_condition_evaluated").Str("rule_name", condition.Rule).Str("fact_name", condition.Fact).Str("fact_state", string(condition.State)).Str("result", string(condition.Result)).Msg("Evaluated condition")
		}
		for _, rule := range round.Evaluation.Rules {
			e.recordRuleFired(rule)
		}
		for _, action := range round.Evaluation.Actions {
			record := compiler.Action{Type: "updateStore", Target: action.Target, Value: action.Value}
			if round.Commit.Outcome == store.Committed && round.CommitError == "" {
				e.recordActionSucceeded(record)
			} else if err != nil {
				e.recordActionFailed(record, err)
			}
		}
		logger.Info().Str("event", "round_completed").Str("chain_id", result.ChainID).Int("round", round.Round).Str("commit_outcome", string(round.Commit.Outcome)).Int("actions", len(round.Evaluation.Actions)).Msg("Completed evaluation round")
	}
	if err != nil {
		logger.Error().Err(err).Str("chain_id", result.ChainID).Msg("Batch failed")
	}
	return result, err
}
func (e *Engine) ProcessBatchContext(ctx context.Context, event map[string]interface{}) error {
	if e.coordinator != nil {
		_, err := e.EvaluateBatch(ctx, event)
		return err
	}
	// Explicit legacy dispatch preserves v3 artifact meaning.
	keys := make([]string, 0, len(event))
	for key := range event {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := e.ProcessFactUpdateContext(ctx, key, event[key]); err != nil {
			return err
		}
	}
	return nil
}
