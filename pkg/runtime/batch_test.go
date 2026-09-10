package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/eventcontext"
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
	failAfter      int
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
	if s.outcome != "" && s.commits > s.failAfter {
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
	require.False(t, p.hasTemporalConditions)
	require.Nil(t, p.temporal)
	require.Nil(t, p.hasTemporal)
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

func TestChangeOnlyEmissionUsesPersistedTargetState(t *testing.T) {
	source := `{"rules":[{"name":"change","emit":"on_change","conditions":{"all":[{"fact":"trigger","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`
	p := batchProgram(t, source)
	require.Equal(t, compiler.ChangeOnlyVersion, p.Version())
	require.True(t, p.HasChangeOnly())

	tests := []struct {
		name       string
		snapshot   store.Fact
		event      map[string]interface{}
		suppressed bool
	}{
		{"equal", store.Fact{State: store.Present, Value: true}, map[string]interface{}{"trigger": true}, true},
		{"different", store.Fact{State: store.Present, Value: false}, map[string]interface{}{"trigger": true}, false},
		{"missing", store.Fact{State: store.Missing}, map[string]interface{}{"trigger": true}, false},
		{"null", store.Fact{State: store.Null}, map[string]interface{}{"trigger": true}, false},
		{"invalid", store.Fact{State: store.Invalid}, map[string]interface{}{"trigger": true}, false},
		{"event target cannot mask persisted difference", store.Fact{State: store.Present, Value: false}, map[string]interface{}{"trigger": true, "out": true}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evaluation, budget, err := p.Evaluate(context.Background(), map[string]store.Fact{"out": test.snapshot}, test.event, DefaultLimits(), Budget{}, true)
			require.NoError(t, err)
			require.Equal(t, []string{"change"}, evaluation.Rules)
			require.Equal(t, 1, budget.Actions, "suppressed proposals still consume the action budget")
			require.Len(t, evaluation.Actions, 1)
			require.Equal(t, test.suppressed, evaluation.Actions[0].Suppressed)
			if test.suppressed {
				require.Empty(t, evaluation.Writes)
			} else {
				require.Equal(t, []store.Write{{Key: "out", Value: true}}, evaluation.Writes)
			}
		})
	}
}

func TestChangeOnlyCoalescingAndConflictPolicy(t *testing.T) {
	tests := []struct {
		name       string
		secondEmit string
		second     string
		wantError  bool
		wantSkip   bool
	}{
		{"all change-only", `,"emit":"on_change"`, "true", false, true},
		{"mixed policy preserves write", "", "true", false, false},
		{"conflict remains an error", `,"emit":"on_change"`, "false", true, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := `{"rules":[{"name":"first","emit":"on_change","conditions":{"all":[{"fact":"trigger","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":true}]},{"name":"second"` + test.secondEmit + `,"conditions":{"all":[{"fact":"trigger","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":` + test.second + `}]}]}`
			evaluation, _, err := batchProgram(t, source).Evaluate(context.Background(), map[string]store.Fact{"out": {State: store.Present, Value: true}}, map[string]interface{}{"trigger": true}, DefaultLimits(), Budget{}, true)
			if test.wantError {
				require.ErrorContains(t, err, "conflicting writes")
				return
			}
			require.NoError(t, err)
			require.Len(t, evaluation.Actions, 2)
			if test.wantSkip {
				require.Empty(t, evaluation.Writes)
				require.True(t, evaluation.Actions[0].Suppressed)
				require.True(t, evaluation.Actions[1].Suppressed)
			} else {
				require.Len(t, evaluation.Writes, 1)
				require.False(t, evaluation.Actions[0].Suppressed)
				require.False(t, evaluation.Actions[1].Suppressed)
			}
		})
	}
}

func TestChangeOnlySurvivesRestartAndSelfHealsExternalDrift(t *testing.T) {
	source := `{"rules":[{"name":"change","emit":"on_change","conditions":{"all":[{"fact":"trigger","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`
	s := probe(t, nil)
	process := func(chain string) ChainResult {
		coordinator, err := NewCoordinator(batchProgram(t, source), s, s, DefaultLimits())
		require.NoError(t, err)
		result, err := coordinator.Process(context.Background(), chain, map[string]interface{}{"trigger": true})
		require.NoError(t, err)
		return result
	}

	first := process("first")
	require.Equal(t, 1, s.commits)
	require.Len(t, first.Rounds, 2)

	second := process("after-restart")
	require.Equal(t, 1, s.commits)
	require.Len(t, second.Rounds, 1)
	require.True(t, second.Rounds[0].Evaluation.Actions[0].Suppressed)
	require.Empty(t, second.Rounds[0].Evaluation.Writes)

	require.NoError(t, s.SetFactContext(context.Background(), "out", false))
	process("external-drift")
	require.Equal(t, 2, s.commits)
}

func TestChangeOnlyRedisSnapshotSurvivesRestart(t *testing.T) {
	server, err := miniredis.Run()
	require.NoError(t, err)
	defer server.Close()
	backend, err := store.NewRedisStore(context.Background(), store.RedisOptions{Addr: server.Addr()})
	require.NoError(t, err)
	defer backend.Close()
	require.NoError(t, backend.SetFactContext(context.Background(), "out", false))
	source := `{"rules":[{"name":"change","emit":"on_change","conditions":{"all":[{"fact":"trigger","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`
	process := func(chain string) ChainResult {
		coordinator, err := NewCoordinator(batchProgram(t, source), backend, backend, DefaultLimits())
		require.NoError(t, err)
		result, err := coordinator.Process(context.Background(), chain, map[string]interface{}{"trigger": true})
		require.NoError(t, err)
		return result
	}
	first := process("redis-first")
	require.Len(t, first.Rounds, 2)
	require.Equal(t, store.Committed, first.Rounds[0].Commit.Outcome)
	second := process("redis-restart")
	require.Len(t, second.Rounds, 1)
	require.True(t, second.Rounds[0].Evaluation.Actions[0].Suppressed)
	require.Equal(t, store.NotCommitted, second.Rounds[0].Commit.Outcome)
}

func TestDefaultEmissionStillRepeatsEqualWrites(t *testing.T) {
	p := batchProgram(t, `{"rules":[{"name":"default","conditions":{"all":[{"fact":"trigger","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`)
	evaluation, _, err := p.Evaluate(context.Background(), map[string]store.Fact{"out": {State: store.Present, Value: true}}, map[string]interface{}{"trigger": true}, DefaultLimits(), Budget{}, true)
	require.NoError(t, err)
	require.Len(t, evaluation.Writes, 1)
	require.False(t, evaluation.Actions[0].Suppressed)
}

func TestChangeOnlyReportsSkippedActionOutcome(t *testing.T) {
	source := []byte(`{"rules":[{"name":"change","emit":"on_change","conditions":{"all":[{"fact":"trigger","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`)
	artifact, err := compiler.CompileBatch(source)
	require.NoError(t, err)
	s := probe(t, map[string]interface{}{"out": true})
	engine, err := newBatchEngine(artifact, s)
	require.NoError(t, err)
	observer := &recordingExecutionObserver{}
	engine.SetExecutionObserver(observer)
	result, err := engine.EvaluateBatch(context.Background(), map[string]interface{}{"trigger": true})
	require.NoError(t, err)
	require.Len(t, result.Rounds, 1)
	require.Equal(t, []string{"change"}, observer.rulesFired)
	require.Equal(t, []string{"updateStore"}, observer.actionsSkipped)
	require.Empty(t, observer.actionsSucceeded)
}

func TestV7ComposesTemporalAndChangeOnlyCapabilities(t *testing.T) {
	source := `{"rules":[{"name":"sustained","emit":"on_change","conditions":{"all":[{"fact":"hot","operator":"EQ","value":true,"for":"5m"}]},"actions":[{"type":"updateStore","target":"alert","value":true}]}]}`
	p := batchProgram(t, source)
	require.Equal(t, compiler.ChangeOnlyVersion, p.Version())
	require.True(t, p.HasTemporal())
	require.True(t, p.HasChangeOnly())
	start := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	first, _, err := p.EvaluateAt(context.Background(), map[string]store.Fact{"alert": {State: store.Present, Value: true}}, map[string]interface{}{"hot": true}, DefaultLimits(), Budget{}, true, start)
	require.NoError(t, err)
	require.Len(t, first.Writes, 1)
	require.True(t, first.Writes[0].Internal)
	tracker := first.Writes[0]
	boundary, _, err := p.EvaluateAt(context.Background(), map[string]store.Fact{
		"alert":     {State: store.Present, Value: true},
		tracker.Key: {State: store.Present, Value: tracker.Value},
	}, map[string]interface{}{"hot": true}, DefaultLimits(), Budget{}, true, start.Add(5*time.Minute))
	require.NoError(t, err)
	require.Equal(t, []string{"sustained"}, boundary.Rules)
	require.Empty(t, boundary.Writes)
	require.True(t, boundary.Actions[0].Suppressed)
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

type testClock struct{ at time.Time }

func (c *testClock) Now() time.Time { return c.at }

func temporalProgram(t *testing.T, conditions string) (*Program, []byte) {
	t.Helper()
	source := `{"rules":[{"name":"sustained","conditions":` + conditions + `,"actions":[{"type":"updateStore","target":"alert","value":true}]}]}`
	artifact, err := compiler.CompileBatch([]byte(source))
	require.NoError(t, err)
	p, err := LoadProgram(artifact)
	require.NoError(t, err)
	return p, artifact
}

func TestTemporalConditionBoundaryResetAndRestart(t *testing.T) {
	p, artifact := temporalProgram(t, `{"all":[{"fact":"hot","operator":"EQ","value":true,"for":"5m"}]}`)
	memory, err := store.NewMemoryStore(nil)
	require.NoError(t, err)
	clock := &testClock{at: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)}
	coordinator, err := NewCoordinator(p, memory, memory, DefaultLimits())
	require.NoError(t, err)
	require.NoError(t, coordinator.SetClock(clock))

	first, err := coordinator.Process(context.Background(), "first", map[string]interface{}{"hot": true})
	require.NoError(t, err)
	require.Equal(t, Indeterminate, first.Rounds[0].Evaluation.RuleResults[0].Result)
	require.Empty(t, first.Rounds[0].Evaluation.Actions)
	require.Empty(t, first.Rounds[0].Commit.Applied)
	require.Empty(t, snapshotMemory(t, memory), "timer state must stay out of public snapshots")
	require.Empty(t, drainMemory(t, memory), "timer state must not publish")

	clock.at = clock.at.Add(5*time.Minute - time.Nanosecond)
	before, err := coordinator.Process(context.Background(), "before", map[string]interface{}{"hot": true})
	require.NoError(t, err)
	require.Equal(t, Indeterminate, before.Rounds[0].Evaluation.RuleResults[0].Result)

	// Re-loading the same artifact reconstructs the same private tracker key.
	p, err = LoadProgram(artifact)
	require.NoError(t, err)
	coordinator, err = NewCoordinator(p, memory, memory, DefaultLimits())
	require.NoError(t, err)
	clock.at = clock.at.Add(time.Nanosecond)
	require.NoError(t, coordinator.SetClock(clock))
	atBoundary, err := coordinator.Process(context.Background(), "boundary", map[string]interface{}{"hot": true})
	require.NoError(t, err)
	require.Equal(t, True, atBoundary.Rounds[0].Evaluation.RuleResults[0].Result)
	require.Len(t, atBoundary.Rounds[0].Evaluation.Actions, 1)
	require.Equal(t, true, snapshotMemory(t, memory)["alert"])
	clock.at = clock.at.Add(time.Minute)
	afterBoundary, err := coordinator.Process(context.Background(), "after-boundary", map[string]interface{}{"hot": true})
	require.NoError(t, err)
	require.Len(t, afterBoundary.Rounds[0].Evaluation.Actions, 1, "temporal conditions remain level-triggered until reset")

	clock.at = clock.at.Add(time.Minute)
	_, err = coordinator.Process(context.Background(), "reset", map[string]interface{}{"hot": false})
	require.NoError(t, err)
	var trackerKey string
	for _, temporal := range p.temporal {
		trackerKey = temporal.key
	}
	tracker, err := memory.ReadSnapshot(context.Background(), []string{trackerKey})
	require.NoError(t, err)
	require.Equal(t, store.Missing, tracker[trackerKey].State)
	clock.at = clock.at.Add(time.Minute)
	restarted, err := coordinator.Process(context.Background(), "restart", map[string]interface{}{"hot": true})
	require.NoError(t, err)
	require.Equal(t, Indeterminate, restarted.Rounds[0].Evaluation.RuleResults[0].Result)
}

func TestTemporalStateMaintainedPastBooleanShortCircuit(t *testing.T) {
	p, _ := temporalProgram(t, `{"all":[{"fact":"gate","operator":"EQ","value":true},{"fact":"hot","operator":"EQ","value":true,"for":"1m"}]}`)
	memory, err := store.NewMemoryStore(nil)
	require.NoError(t, err)
	clock := &testClock{at: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)}
	coordinator, err := NewCoordinator(p, memory, memory, DefaultLimits())
	require.NoError(t, err)
	require.NoError(t, coordinator.SetClock(clock))

	_, err = coordinator.Process(context.Background(), "start", map[string]interface{}{"gate": false, "hot": true})
	require.NoError(t, err)
	clock.at = clock.at.Add(30 * time.Second)
	_, err = coordinator.Process(context.Background(), "reset", map[string]interface{}{"gate": false, "hot": false})
	require.NoError(t, err)
	clock.at = clock.at.Add(31 * time.Second)
	result, err := coordinator.Process(context.Background(), "check", map[string]interface{}{"gate": true, "hot": true})
	require.NoError(t, err)
	require.Equal(t, Indeterminate, result.Rounds[0].Evaluation.RuleResults[0].Result, "false while short-circuited must reset the timer")
}

func TestTemporalConditionRequiresClockAndRestartsAfterRegression(t *testing.T) {
	p, _ := temporalProgram(t, `{"all":[{"fact":"hot","operator":"EQ","value":true,"for":"1m"}]}`)
	_, _, err := p.Evaluate(context.Background(), nil, map[string]interface{}{"hot": true}, DefaultLimits(), Budget{}, true)
	require.ErrorContains(t, err, "injected processing time")
	var trackerKey string
	for _, temporal := range p.temporal {
		trackerKey = temporal.key
	}
	_, _, err = p.EvaluateAt(context.Background(), map[string]store.Fact{trackerKey: {State: store.Present, Value: "corrupt"}}, map[string]interface{}{"hot": true}, DefaultLimits(), Budget{}, true, time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC))
	require.ErrorContains(t, err, "invalid temporal state")
	memory, err := store.NewMemoryStore(nil)
	require.NoError(t, err)
	clock := &testClock{at: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)}
	coordinator, err := NewCoordinator(p, memory, memory, DefaultLimits())
	require.NoError(t, err)
	require.NoError(t, coordinator.SetClock(clock))
	_, err = coordinator.Process(context.Background(), "start", map[string]interface{}{"hot": true})
	require.NoError(t, err)
	clock.at = clock.at.Add(-time.Second)
	result, err := coordinator.Process(context.Background(), "backward", map[string]interface{}{"hot": true})
	require.NoError(t, err)
	require.Equal(t, Indeterminate, result.Rounds[0].Evaluation.RuleResults[0].Result)
	require.Len(t, result.Rounds[0].Evaluation.Writes, 1)
	require.True(t, result.Rounds[0].Evaluation.Writes[0].Internal)
	require.False(t, result.Rounds[0].Evaluation.Writes[0].Delete)
	_, err = coordinator.Process(context.Background(), "forged", map[string]interface{}{trackerKey: "2000-01-01T00:00:00Z"})
	require.ErrorContains(t, err, "reserved internal state prefix")
}

func TestTemporalConditionUnknownStatesResetTracker(t *testing.T) {
	for _, state := range []store.FactState{store.Missing, store.Null, store.Invalid} {
		t.Run(string(state), func(t *testing.T) {
			p, _ := temporalProgram(t, `{"all":[{"fact":"trigger","operator":"EQ","value":true},{"fact":"hot","operator":"EQ","value":true,"for":"1m"}]}`)
			var trackerKey string
			for _, temporal := range p.temporal {
				trackerKey = temporal.key
			}
			snapshot := map[string]store.Fact{
				"hot":      {State: state},
				trackerKey: {State: store.Present, Value: "2026-09-09T12:00:00Z"},
			}
			evaluation, _, err := p.EvaluateAt(context.Background(), snapshot, map[string]interface{}{"trigger": true}, DefaultLimits(), Budget{}, true, time.Date(2026, 9, 9, 12, 2, 0, 0, time.UTC))
			require.NoError(t, err)
			require.Contains(t, evaluation.Writes, store.Write{Key: trackerKey, Value: nil, Internal: true, Delete: true})
			require.Empty(t, evaluation.Actions)
		})
	}
}

func TestTemporalAndActionWritesShareStagedByteBudget(t *testing.T) {
	source := `{"rules":[{"name":"timer","priority":0,"conditions":{"all":[{"fact":"hot","operator":"EQ","value":true,"for":"1m"}]},"actions":[{"type":"updateStore","target":"unused","value":true}]},{"name":"output","priority":1,"conditions":{"all":[{"fact":"hot","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"result","value":"` + strings.Repeat("x", 100) + `"}]}]}`
	p := batchProgram(t, source)
	var trackerKey string
	for _, temporal := range p.temporal {
		trackerKey = temporal.key
	}
	limits := DefaultLimits()
	timerBytes, err := fieldBytes(trackerKey, "2026-09-09T12:00:00Z", limits.StagedBytes)
	require.NoError(t, err)
	actionBytes, err := fieldBytes("result", strings.Repeat("x", 100), limits.StagedBytes)
	require.NoError(t, err)
	limits.StagedBytes = max(timerBytes, actionBytes)
	_, _, err = p.EvaluateAt(context.Background(), nil, map[string]interface{}{"hot": true}, limits, Budget{}, true, time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC))
	require.ErrorContains(t, err, "staged byte budget")
}

func TestTemporalMaintenanceSkipsUnrelatedShortCircuitedSiblings(t *testing.T) {
	p, _ := temporalProgram(t, `{"all":[{"fact":"gate","operator":"EQ","value":true},{"fact":"unused","operator":"EQ","value":true},{"fact":"hot","operator":"EQ","value":true,"for":"1m"}]}`)
	evaluation, _, err := p.EvaluateAt(context.Background(), nil, map[string]interface{}{"gate": false, "unused": true, "hot": true}, DefaultLimits(), Budget{}, true, time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, []ConditionResult{
		{Rule: "sustained", Fact: "gate", State: store.Present, Result: False},
		{Rule: "sustained", Fact: "hot", State: store.Present, Result: Indeterminate},
	}, evaluation.Conditions)
}

func snapshotMemory(t *testing.T, memory *store.MemoryStore) map[string]interface{} {
	t.Helper()
	values, err := memory.Snapshot()
	require.NoError(t, err)
	return values
}

func drainMemory(t *testing.T, memory *store.MemoryStore) []store.FactUpdate {
	t.Helper()
	events, err := memory.DrainPublications()
	require.NoError(t, err)
	return events
}

func TestBatchEngineReviewBoundaries(t *testing.T) {
	s := probe(t, nil)
	c, err := NewCoordinator(batchProgram(t, batchOne), s, s, DefaultLimits())
	require.NoError(t, err)
	e := &Engine{coordinator: c, maxActionsPerEvaluation: 32}
	observer := &recordingExecutionObserver{}
	e.SetExecutionObserver(observer)
	event := map[string]interface{}{"a": float64(1), "b": float64(1)}
	for _, kind := range []string{"committed_output", "unsupported"} {
		ctx := eventcontext.WithMetadata(context.Background(), eventcontext.Metadata{Kind: kind})
		err := e.ProcessBatchContext(ctx, event)
		if kind == "committed_output" {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, "unsupported event kind")
		}
	}
	require.Zero(t, s.commits)
	for _, limit := range []int{0, -1, 1025} {
		require.ErrorContains(t, e.SetMaxActionsPerEvaluation(limit), "actions_per_rule")
		require.Equal(t, 32, e.maxActionsPerEvaluation)
		require.Equal(t, 32, c.limits.ActionsPerRule)
	}
	s.outcome, s.fail = store.Committed, errors.New("inconsistent")
	result, err := e.EvaluateBatch(context.Background(), event)
	require.ErrorIs(t, err, ErrReconciliationRequired)
	require.Equal(t, "inconsistent", result.Rounds[0].CommitError)
	require.Empty(t, observer.actionsSucceeded)
	require.Len(t, observer.actionFailures, 1)
}

func TestBatchEarlierSuccessSurvivesLaterCommitError(t *testing.T) {
	source := `{"rules":[{"name":"first","conditions":{"all":[{"fact":"a","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"b","value":true}]},{"name":"second","conditions":{"all":[{"fact":"b","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`
	s := probe(t, nil)
	s.failAfter, s.outcome, s.fail = 1, store.Committed, errors.New("second round inconsistent")
	c, err := NewCoordinator(batchProgram(t, source), s, s, DefaultLimits())
	require.NoError(t, err)
	e := &Engine{coordinator: c}
	observer := &recordingExecutionObserver{}
	e.SetExecutionObserver(observer)
	result, err := e.EvaluateBatch(context.Background(), map[string]interface{}{"a": true})
	require.ErrorIs(t, err, ErrReconciliationRequired)
	require.Len(t, result.Rounds, 2)
	require.Empty(t, result.Rounds[0].CommitError)
	require.Equal(t, "second round inconsistent", result.Rounds[1].CommitError)
	require.Len(t, observer.actionsSucceeded, 1)
	require.Len(t, observer.actionFailures, 1)
}

func TestTypedFactRuntimeContract(t *testing.T) {
	source := `{"facts":{"trigger":{"type":"boolean"},"dependency":{"type":"number","nullable":true},"out":{"type":"boolean"}},"rules":[{"name":"typed","conditions":{"all":[{"fact":"trigger","operator":"EQ","value":true},{"fact":"dependency","operator":"GT","value":0}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`
	p := batchProgram(t, source)
	require.Equal(t, compiler.TypedFactsVersion, p.Version())
	require.Equal(t, compiler.FactNumber, p.FactDeclarations()["dependency"].Type)

	limits := DefaultLimits()
	require.NoError(t, ValidateProgramEvent(p, map[string]interface{}{"trigger": true}, limits), "missing declared facts are valid")
	require.NoError(t, ValidateProgramEvent(p, map[string]interface{}{"dependency": nil}, limits), "nullable facts accept null")
	require.ErrorContains(t, ValidateProgramEvent(p, map[string]interface{}{"unknown": true}, limits), "undeclared")
	require.ErrorContains(t, ValidateProgramEvent(p, map[string]interface{}{"trigger": "true"}, limits), "must be boolean")
	require.ErrorContains(t, ValidateProgramEvent(p, map[string]interface{}{"out": nil}, limits), "is null")
	require.Error(t, ValidateProgramFacts(p, map[string]interface{}{"dependency": math.Inf(1)}))

	evaluation, _, err := p.Evaluate(context.Background(), map[string]store.Fact{
		"dependency": {State: store.Present, Value: "wrong"},
	}, map[string]interface{}{"trigger": true}, limits, Budget{}, true)
	require.NoError(t, err)
	require.Equal(t, Indeterminate, evaluation.RuleResults[0].Result)
	require.Equal(t, store.Invalid, evaluation.Conditions[1].State)
	require.Empty(t, evaluation.Actions)
}
