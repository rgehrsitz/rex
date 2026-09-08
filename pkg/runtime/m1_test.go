package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/logging"
)

func TestRepeatedDependencyRecordsRetainAllDependencies(t *testing.T) {
	rules := &compiler.Ruleset{Rules: []compiler.Rule{{Name: "r", Priority: 10,
		Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{
			{Fact: "a", Operator: "GT", Value: 0.0}, {Fact: "b", Operator: "GT", Value: 0.0},
		}}, Actions: []compiler.Action{{Type: "updateStore", Target: "out", Value: true}},
	}}}
	artifact := mustGenerateBytecode(t, rules)
	artifact.FactDependencyIndex = append(artifact.FactDependencyIndex, compiler.FactDependencyIndex{RuleName: "r", Facts: []string{"extra"}})
	path := t.TempDir() + "/repeated.bytecode"
	require.NoError(t, compiler.WriteBytecodeToFile(path, artifact))
	s := &contextCaptureStore{mGetValues: map[string]interface{}{"extra": 1.0}}
	e, err := NewEngineFromFile(path, s, 0)
	require.NoError(t, err)
	require.NoError(t, e.ProcessFactUpdateContext(context.Background(), "a", 1.0))
	assert.ElementsMatch(t, []string{"b", "extra"}, s.mGetKeys)
	assert.Zero(t, s.publishCount)
	s.mGetValues["b"] = 1.0
	require.NoError(t, e.ProcessFactUpdateContext(context.Background(), "a", 1.0))
	assert.Equal(t, 1, s.publishCount)
}

func TestMissingDependenciesPreservePriorityAndRecoverAcrossUpdates(t *testing.T) {
	rules := &compiler.Ruleset{Rules: []compiler.Rule{
		{Name: "last", Priority: 10}, {Name: "first", Priority: 1},
		{Name: "second", Priority: 1}, {Name: "middle", Priority: 5},
	}}
	for i := range rules.Rules {
		r := &rules.Rules[i]
		r.Conditions.All = []*compiler.ConditionOrGroup{
			{Fact: "a", Operator: "GT", Value: 0.0},
			{Fact: r.Name, Operator: "GT", Value: 0.0},
			{Fact: "shared", Operator: "GT", Value: 0.0},
		}
		r.Actions = []compiler.Action{{Type: "updateStore", Target: "out:" + r.Name, Value: true}}
	}
	path := t.TempDir() + "/filter.bytecode"
	require.NoError(t, compiler.WriteBytecodeToFile(path, mustGenerateBytecode(t, rules)))
	s := &contextCaptureStore{mGetValues: map[string]interface{}{"shared": 1.0, "last": 1.0, "second": 1.0}}
	e, err := NewEngineFromFile(path, s, 0)
	require.NoError(t, err)
	observer := &recordingExecutionObserver{}
	e.SetExecutionObserver(observer)
	all := []string{"first", "second", "middle", "last"}
	for _, round := range []struct {
		values map[string]interface{}
		want   []string
	}{
		{s.mGetValues, []string{"second", "last"}},
		{map[string]interface{}{"shared": 1.0, "last": 1.0, "first": 1.0, "second": 1.0, "middle": 1.0}, all},
		{map[string]interface{}{}, nil},
		{s.mGetValues, []string{"second", "last"}},
	} {
		s.mGetValues = round.values
		observer.rulesFired = nil
		require.NoError(t, e.ProcessFactUpdateContext(context.Background(), "a", 1.0))
		assert.Equal(t, round.want, observer.rulesFired)
		assert.Equal(t, all, e.factRuleIndex["a"])
		assert.ElementsMatch(t, []string{"first", "second", "middle", "last", "shared"}, s.mGetKeys)
	}
}

func TestConditionTracingCanBeDisabledWithoutHidingSummariesOrFailures(t *testing.T) {
	output := captureStructuredLogs(t)
	s := &contextCaptureStore{}
	rules := &compiler.Ruleset{Rules: []compiler.Rule{{Name: "r", Priority: 10,
		Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{Fact: "a", Operator: "GT", Value: 0.0}}},
		Actions:    []compiler.Action{{Type: "updateStore", Target: "out", Value: true}},
	}}}
	path := t.TempDir() + "/trace.bytecode"
	require.NoError(t, compiler.WriteBytecodeToFile(path, mustGenerateBytecode(t, rules)))
	e, err := NewEngineFromFile(path, s, 0)
	require.NoError(t, err)
	ctx := WithTraceID(context.Background(), "m1-event")
	e.SetConditionTracing(false)
	output.Reset()
	require.NoError(t, e.ProcessFactUpdateContext(ctx, "a", 1.0))
	events := structuredLogEvents(t, output)
	assert.Nil(t, findStructuredEvent(events, "rule_condition_evaluated"))
	for _, name := range []string{"rule_evaluation_candidates", "action_completed", "rule_evaluation_completed"} {
		event := findStructuredEvent(events, name)
		require.NotNil(t, event, name)
		assert.Equal(t, "m1-event", event["trace_id"])
	}
	output.Reset()
	s.setAndPublishErr = errors.New("write failed")
	require.ErrorIs(t, e.ProcessFactUpdateContext(ctx, "a", 1.0), s.setAndPublishErr)
	events = structuredLogEvents(t, output)
	assert.NotNil(t, findStructuredEvent(events, "action_failed"))
	assert.NotNil(t, findStructuredEvent(events, "rule_evaluation_failed"))
	assert.Nil(t, findStructuredEvent(events, "rule_condition_evaluated"))
	s.setAndPublishErr = nil
	e.SetConditionTracing(true)
	output.Reset()
	require.NoError(t, e.ProcessFactUpdateContext(ctx, "a", 1.0))
	assert.NotNil(t, findStructuredEvent(structuredLogEvents(t, output), "rule_condition_evaluated"))
}

// Profile the CPU path after the changes without Redis or compiler/setup time.
// The full cross-version performance comparison uses the unchanged M0 harness.
func BenchmarkM1DenseEvaluation(b *testing.B) {
	for _, tc := range []struct {
		name       string
		level      zerolog.Level
		conditions bool
	}{
		{"disabled", zerolog.Disabled, true},
		{"info-conditions", zerolog.InfoLevel, true},
		{"info-summary", zerolog.InfoLevel, false},
	} {
		b.Run(tc.name, func(b *testing.B) { benchmarkM1Dense(b, tc.level, tc.conditions) })
	}
}

func benchmarkM1Dense(b *testing.B, level zerolog.Level, conditions bool) {
	oldLogger, oldLevel := logging.Logger, zerolog.GlobalLevel()
	logging.Logger = zerolog.New(io.Discard)
	zerolog.SetGlobalLevel(level)
	b.Cleanup(func() { logging.Logger = oldLogger; zerolog.SetGlobalLevel(oldLevel) })
	rules := &compiler.Ruleset{}
	for i := 0; i < 1000; i++ {
		rules.Rules = append(rules.Rules, compiler.Rule{Name: fmt.Sprintf("r%d", i), Priority: 10,
			Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{
				{Fact: "a", Operator: "GT", Value: 0.0}, {Fact: "b", Operator: "GT", Value: 0.0},
			}}, Actions: []compiler.Action{{Type: "updateStore", Target: "out", Value: true}},
		})
	}
	path := b.TempDir() + "/profile.bytecode"
	require.NoError(b, compiler.WriteBytecodeToFile(path, mustGenerateBytecode(b, rules)))
	s := &contextCaptureStore{mGetValues: map[string]interface{}{"b": 1.0}}
	e, err := NewEngineFromFile(path, s, 0)
	require.NoError(b, err)
	e.SetConditionTracing(conditions)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := e.ProcessFactUpdateContext(ctx, "a", 1.0); err != nil {
			b.Fatal(err)
		}
	}
}
