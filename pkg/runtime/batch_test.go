package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"
)

func batchProgram(t *testing.T, source string) *Program {
	t.Helper()
	data, err := compiler.CompileBatch([]byte(source))
	require.NoError(t, err)
	p, err := LoadProgram(data)
	require.NoError(t, err)
	return p
}

const batchOne = `{"rules":[{"name":"r","conditions":{"all":[{"fact":"a","operator":"GT","value":0},{"fact":"b","operator":"GT","value":0}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`

type batchProbe struct {
	*store.MemoryStore
	reads, commits int
	failRead       bool
	outcome        store.CommitOutcome
	fail           error
	cancel         context.CancelFunc
}

func (s *batchProbe) ReadSnapshot(ctx context.Context, keys []string) (map[string]store.Fact, error) {
	s.reads++
	if s.failRead {
		return nil, errors.New("read failed")
	}
	if s.cancel != nil {
		s.cancel()
	}
	return s.MemoryStore.ReadSnapshot(ctx, keys)
}
func (s *batchProbe) Commit(ctx context.Context, r store.CommitRequest) (store.CommitResult, error) {
	s.commits++
	if s.outcome != "" {
		return store.CommitResult{Outcome: s.outcome}, s.fail
	}
	return s.MemoryStore.Commit(ctx, r)
}
func probe(t *testing.T, initial map[string]interface{}) *batchProbe {
	t.Helper()
	s, err := store.NewMemoryStore(initial)
	require.NoError(t, err)
	return &batchProbe{MemoryStore: s}
}
func TestBatchOnceAndSnapshotIsolation(t *testing.T) {
	p := batchProgram(t, batchOne)
	s := probe(t, map[string]interface{}{"a": float64(-1), "b": float64(-1)})
	c, err := NewCoordinator(p, s, s, DefaultLimits())
	require.NoError(t, err)
	result, err := c.Process(context.Background(), "once", map[string]interface{}{"b": float64(1), "a": float64(1)})
	require.NoError(t, err)
	require.Equal(t, 1, s.commits)
	require.Zero(t, s.reads)
	require.Len(t, result.Rounds[0].Evaluation.Actions, 1)
	// Staged writes cannot change a later rule's condition in the same round.
	source := `{"rules":[{"name":"first","priority":0,"conditions":{"all":[{"fact":"a","operator":"GT","value":0}]},"actions":[{"type":"updateStore","target":"b","value":1}]},{"name":"later","priority":1,"conditions":{"all":[{"fact":"a","operator":"GT","value":0},{"fact":"b","operator":"GT","value":0}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`
	p = batchProgram(t, source)
	eval, _, err := p.Evaluate(context.Background(), map[string]store.Fact{"b": {State: store.Present, Value: float64(0)}}, map[string]interface{}{"a": float64(1)}, DefaultLimits(), Budget{}, true)
	require.NoError(t, err)
	require.Equal(t, []string{"first"}, eval.Rules)
	require.Len(t, eval.Actions, 1)
}
func TestBatchConflictsAndCoalescing(t *testing.T) {
	for _, same := range []bool{true, false} {
		t.Run(fmt.Sprint(same), func(t *testing.T) {
			value := "true"
			if !same {
				value = "false"
			}
			source := `{"rules":[{"name":"r","conditions":{"all":[{"fact":"a","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":true},{"type":"updateStore","target":"out","value":` + value + `}]}]}`
			p := batchProgram(t, source)
			s := probe(t, nil)
			c, err := NewCoordinator(p, s, s, DefaultLimits())
			require.NoError(t, err)
			result, err := c.Process(context.Background(), "conflict", map[string]interface{}{"a": true})
			if same {
				require.NoError(t, err)
				require.Len(t, result.Rounds[0].Evaluation.Actions, 2)
				require.Len(t, result.Rounds[0].Evaluation.Writes, 1)
			} else {
				require.ErrorContains(t, err, "conflicting")
				require.Zero(t, s.commits)
				facts, _ := s.Snapshot()
				require.Empty(t, facts)
			}
		})
	}
}
func TestBatchFailureOutcomes(t *testing.T) {
	for _, outcome := range []store.CommitOutcome{store.NotCommitted, store.Partial, store.Unknown} {
		t.Run(string(outcome), func(t *testing.T) {
			s := probe(t, map[string]interface{}{"b": float64(1)})
			s.outcome = outcome
			s.fail = errors.New("injected")
			c, err := NewCoordinator(batchProgram(t, batchOne), s, s, DefaultLimits())
			require.NoError(t, err)
			result, err := c.Process(context.Background(), "failure", map[string]interface{}{"a": float64(1)})
			require.Error(t, err)
			require.Equal(t, outcome, result.Rounds[0].Commit.Outcome)
			require.Equal(t, 1, s.commits)
			_, err = c.Process(context.Background(), "next", map[string]interface{}{"a": float64(1)})
			if outcome == store.NotCommitted {
				require.Equal(t, 2, s.commits)
			} else {
				require.ErrorIs(t, err, ErrReconciliationRequired)
				require.Equal(t, 1, s.commits)
			}
		})
	}
	s := probe(t, nil)
	s.failRead = true
	c, _ := NewCoordinator(batchProgram(t, batchOne), s, s, DefaultLimits())
	_, err := c.Process(context.Background(), "read", map[string]interface{}{"a": float64(1)})
	require.ErrorContains(t, err, "snapshot")
	require.Zero(t, s.commits)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Process(ctx, "canceled", map[string]interface{}{"a": float64(1)})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, s.reads)
}
func TestBatchBudgetsAndChurn(t *testing.T) {
	source := `{"rules":[{"name":"cycle","conditions":{"all":[{"fact":"a","operator":"GT","value":0}]},"actions":[{"type":"updateStore","target":"a","value":1}]}]}`
	for _, kind := range []string{"round", "actions", "work", "bytes", "facts", "per-rule", "per-round"} {
		t.Run(kind, func(t *testing.T) {
			l := DefaultLimits()
			switch kind {
			case "round":
				l.Rounds = 2
			case "actions":
				l.ChainActions = 2
			case "work":
				l.ChainWork = 2
			case "bytes":
				l.StagedBytes = 1
			case "facts":
				l.EventFacts = 1
			case "per-rule":
				l.ActionsPerRule = 1
			case "per-round":
				l.ActionsPerRound = 1
			}
			s := probe(t, nil)
			p := batchProgram(t, source)
			if kind == "per-rule" || kind == "per-round" {
				var rules compiler.Ruleset
				_ = json.Unmarshal([]byte(source), &rules)
				rules.Rules[0].Actions = append(rules.Rules[0].Actions, rules.Rules[0].Actions[0])
				raw, _ := json.Marshal(rules)
				p = batchProgram(t, string(raw))
			}
			c, err := NewCoordinator(p, s, s, l)
			require.NoError(t, err)
			event := map[string]interface{}{"a": float64(1)}
			if kind == "facts" {
				event["b"] = true
			}
			_, err = c.Process(context.Background(), "bounded", event)
			require.Error(t, err)
			require.LessOrEqual(t, s.commits, 2)
			if kind == "bytes" || kind == "facts" || kind == "per-rule" || kind == "per-round" {
				require.Zero(t, s.commits)
			}
		})
	}
	data, err := compiler.CompileBatch([]byte(batchOne))
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "v4")
	require.NoError(t, os.WriteFile(path, data, 0644))
	s := probe(t, nil)
	e, err := NewEngineFromFile(path, s, 0)
	require.NoError(t, err)
	e.SetConditionTracing(false)
	for i := 0; i < 10000; i++ {
		require.NoError(t, e.ProcessBatchContext(context.Background(), map[string]interface{}{fmt.Sprintf("unrelated-%d", i): float64(i)}))
	}
	require.Empty(t, e.Snapshot())
	require.Zero(t, s.reads)
	require.Zero(t, s.commits)
}
func TestProgramInputOwnershipAndTruthTables(t *testing.T) {
	p := batchProgram(t, batchOne)
	snapshot := map[string]store.Fact{"b": {State: store.Present, Value: float64(1)}}
	event := map[string]interface{}{"a": float64(1)}
	copyBefore := fmt.Sprint(snapshot)
	a, _, err := p.Evaluate(context.Background(), snapshot, event, DefaultLimits(), Budget{}, true)
	require.NoError(t, err)
	a.Writes[0].Value = false
	b, _, err := p.Evaluate(context.Background(), snapshot, event, DefaultLimits(), Budget{}, true)
	require.NoError(t, err)
	require.Equal(t, true, b.Writes[0].Value)
	require.Equal(t, copyBefore, fmt.Sprint(snapshot))
	for _, state := range []store.FactState{store.Missing, store.Null, store.Invalid} {
		require.Equal(t, Indeterminate, compareV4(store.Fact{State: state}, true, "NEQ"))
	}
	require.Equal(t, Indeterminate, compareV4(store.Fact{State: store.Present, Value: "wrong"}, true, "NEQ"))
	require.False(t, reflect.DeepEqual(a, b))
}

func TestBatchCancellationDuringSnapshot(t *testing.T) {
	s := probe(t, map[string]interface{}{"b": float64(1)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.cancel = cancel
	c, err := NewCoordinator(batchProgram(t, batchOne), s, s, DefaultLimits())
	require.NoError(t, err)
	_, err = c.Process(ctx, "cancel-read", map[string]interface{}{"a": float64(1)})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, s.commits)
}
func TestBatchInconsistentCommitRequiresReconciliation(t *testing.T) {
	s := probe(t, nil)
	s.outcome, s.fail = store.Committed, errors.New("contradictory acknowledgement")
	c, err := NewCoordinator(batchProgram(t, batchOne), s, s, DefaultLimits())
	require.NoError(t, err)
	_, err = c.Process(context.Background(), "inconsistent", map[string]interface{}{"a": float64(1), "b": float64(1)})
	require.ErrorIs(t, err, ErrReconciliationRequired)
	_, err = c.Process(context.Background(), "later", map[string]interface{}{"a": float64(1)})
	require.ErrorIs(t, err, ErrReconciliationRequired)
	require.Equal(t, 1, s.commits)
}
