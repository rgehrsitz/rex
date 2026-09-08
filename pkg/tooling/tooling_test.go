package tooling

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/runtime"
)

func fixture(t *testing.T) ([]byte, Scenario) {
	t.Helper()
	source, err := os.ReadFile("../../examples/m5/rules.json")
	require.NoError(t, err)
	raw, err := os.ReadFile("../../examples/m5/scenario.json")
	require.NoError(t, err)
	var scenario Scenario
	require.NoError(t, Decode(raw, &scenario))
	return source, scenario
}
func TestReplayDeterminismAndIntegrity(t *testing.T) {
	source, s := fixture(t)
	b, err := NewBundle(source, s)
	require.NoError(t, err)
	before, err := JSON(b)
	require.NoError(t, err)
	r, err := Replay(context.Background(), b)
	require.NoError(t, err)
	require.Empty(t, r.Error)
	require.Equal(t, s.Expect.FinalState, r.FinalState)
	require.Equal(t, s.Expect.Actions, Actions(r))
	require.Equal(t, runtime.False, r.Events[0].Chain.Rounds[0].Evaluation.RuleResults[0].Result)
	require.Equal(t, runtime.True, r.Events[1].Chain.Rounds[0].Evaluation.RuleResults[0].Result)
	again, err := Replay(context.Background(), b)
	require.NoError(t, err)
	encoded, err := JSON(r)
	require.NoError(t, err)
	other, err := JSON(again)
	require.NoError(t, err)
	require.Equal(t, encoded, other)
	after, err := JSON(b)
	require.NoError(t, err)
	require.Equal(t, before, after, "replay must not mutate bundle")
	bad := b
	bad.Source += " "
	_, err = Replay(context.Background(), bad)
	require.ErrorContains(t, err, "digest")
	bad = b
	bad.Manifest.ExecutionContract = 3
	_, err = Replay(context.Background(), bad)
	require.ErrorContains(t, err, "unsupported")
	bad = b
	bad.Scenario.InitialState = nil
	_, err = Replay(context.Background(), bad)
	require.ErrorContains(t, err, "complete initial_state")
	bad = b
	bad.Scenario.Events = []Input{{ID: "same", Facts: map[string]interface{}{}}, {ID: "same", Facts: map[string]interface{}{}}}
	_, err = Replay(context.Background(), bad)
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Replay(ctx, b)
	require.ErrorIs(t, err, context.Canceled)
}
func TestCompareAndScenarioAssertions(t *testing.T) {
	source, s := fixture(t)
	b, err := NewBundle(source, s)
	require.NoError(t, err)
	comparison, err := Compare(context.Background(), b, source)
	require.NoError(t, err)
	require.False(t, comparison.Changed)
	candidate, err := os.ReadFile("../../examples/m5/candidate.json")
	require.NoError(t, err)
	comparison, err = Compare(context.Background(), b, candidate)
	require.NoError(t, err)
	require.True(t, comparison.Changed)
	require.Contains(t, comparison.Changes, "actions")
	require.Contains(t, comparison.Changes, "final_state")
	report, err := Test(context.Background(), source, Suite{SchemaVersion, []Scenario{s}})
	require.NoError(t, err)
	require.True(t, report.Passed)
	s.Expect.Actions = []runtime.ActionProposal{}
	report, err = Test(context.Background(), source, Suite{SchemaVersion, []Scenario{s}})
	require.NoError(t, err)
	require.False(t, report.Passed)
}
func TestReplayCycleRetainsPriorVirtualCommits(t *testing.T) {
	source := []byte(`{"rules":[{"name":"cycle","conditions":{"all":[{"fact":"a","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"a","value":true}]}]}`)
	l := runtime.DefaultLimits()
	l.Rounds = 2
	s := Scenario{SchemaVersion: 1, Name: "cycle", InitialState: map[string]interface{}{}, Events: []Input{{ID: "first", Facts: map[string]interface{}{"a": true}}, {ID: "unreached", Facts: map[string]interface{}{"b": true}}}, Limits: &l}
	b, err := NewBundle(source, s)
	require.NoError(t, err)
	r, err := Replay(context.Background(), b)
	require.NoError(t, err)
	require.Contains(t, r.Error, "round limit")
	require.Len(t, r.Events, 1)
	require.Len(t, Actions(r), 2)
	require.Equal(t, map[string]interface{}{"a": true}, r.FinalState)
}
func TestCLIOutputsAndExitCodes(t *testing.T) {
	source, s := fixture(t)
	b, err := NewBundle(source, s)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "bundle.json")
	raw, err := JSON(b)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0600))
	cases := []struct {
		args []string
		code int
	}{
		{[]string{"simulate", "-bundle", path}, 0},
		{[]string{"explain", "-rules", "../../examples/m5/rules.json"}, 0},
		{[]string{"bundle", "-rules", "../../examples/m5/rules.json", "-scenario", "../../examples/m5/scenario.json"}, 0},
		{[]string{"test", "-rules", "../../examples/m5/rules.json", "-scenario", "../../examples/m5/suite.json"}, 0},
		{[]string{"compare", "-rules", "../../examples/m5/candidate.json", "-bundle", path}, 2},
		{[]string{"lint", "-rules", "../../examples/m5/rules.json", "-channels", "rex_results"}, 0},
		{[]string{"simulate"}, 1},
		{[]string{"simulate", "-bundle", path, "unexpected"}, 1},
	}
	for _, tc := range cases {
		var out, diag bytes.Buffer
		code := RunCLI(context.Background(), tc.args, &out, &diag)
		require.Equal(t, tc.code, code, "%v: %s", tc.args, diag.String())
		if code != 1 {
			require.True(t, json.Valid(out.Bytes()), out.String())
		}
	}
}
func TestLintStableDiagnostics(t *testing.T) {
	source := []byte(`{"rules":[{"name":"a","conditions":{"all":[{"fact":"x","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"x","value":true},{"type":"updateStore","target":"x","value":false}]}]}`)
	r := Lint(source, []string{"rex_results", ""})
	ids := []string{}
	for _, d := range r.Diagnostics {
		ids = append(ids, d.ID)
	}
	require.Contains(t, ids, "REX-L002")
	require.Contains(t, ids, "REX-L003")
	require.Contains(t, ids, "REX-L005")
	require.True(t, HasLintErrors(r))
	missing := strings.Replace(string(source), `"value":false}`, `"value":"{missing}"}`, 1)
	r = Lint([]byte(missing), nil)
	require.Equal(t, "REX-L004", r.Diagnostics[0].ID)
	artifact, err := compiler.CompileBatch(source)
	require.NoError(t, err)
	explanation, err := Explain(artifact)
	require.NoError(t, err)
	require.Equal(t, []string{"x"}, explanation.Rules[0].Dependencies)
}

func TestReplaySnapshotDiagnostics(t *testing.T) {
	source := []byte(`{"rules":[{"name":"check","conditions":{"all":[{"fact":"trigger","operator":"EQ","value":true},{"fact":"dependency","operator":"NEQ","value":0}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`)
	for _, state := range []string{"missing", "null", "invalid"} {
		t.Run(state, func(t *testing.T) {
			initial := map[string]interface{}{}
			if state == "null" {
				initial["dependency"] = nil
			}
			if state == "invalid" {
				initial["dependency"] = map[string]interface{}{"nested": true}
			}
			s := Scenario{SchemaVersion: 1, Name: state, InitialState: initial, Events: []Input{{ID: "trigger", Facts: map[string]interface{}{"trigger": true}}}}
			b, err := NewBundle(source, s)
			require.NoError(t, err)
			r, err := Replay(context.Background(), b)
			require.NoError(t, err)
			require.Empty(t, Actions(r))
			evaluation := r.Events[0].Chain.Rounds[0].Evaluation
			require.Equal(t, runtime.Indeterminate, evaluation.RuleResults[0].Result)
			require.Equal(t, state, string(evaluation.Conditions[1].State))
		})
	}
}

func TestConflictTraceHasNoStagedEffects(t *testing.T) {
	source := []byte(`{"rules":[{"name":"conflict","conditions":{"all":[{"fact":"a","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":true},{"type":"updateStore","target":"out","value":false}]}]}`)
	s := Scenario{SchemaVersion: 1, Name: "conflict", InitialState: map[string]interface{}{}, Events: []Input{{ID: "input", Facts: map[string]interface{}{"a": true}}}}
	b, err := NewBundle(source, s)
	require.NoError(t, err)
	r, err := Replay(context.Background(), b)
	require.NoError(t, err)
	require.Contains(t, r.Error, "conflicting writes")
	round := r.Events[0].Chain.Rounds[0]
	require.Contains(t, round.EvaluationError, "conflicting writes")
	require.Len(t, round.Evaluation.Conditions, 1)
	require.Empty(t, round.Evaluation.Actions)
	require.Empty(t, round.Evaluation.Writes)
	require.Equal(t, map[string]interface{}{"a": true}, r.FinalState)
}
