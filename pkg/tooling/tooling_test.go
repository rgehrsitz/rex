package tooling

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"
)

func fixture(t *testing.T) ([]byte, Scenario) {
	t.Helper()
	source, err := os.ReadFile(fixturePath(t, "rules.json"))
	require.NoError(t, err)
	raw, err := os.ReadFile(fixturePath(t, "scenario.json"))
	require.NoError(t, err)
	var scenario Scenario
	require.NoError(t, Decode(raw, &scenario))
	return source, scenario
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := stdruntime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(file), "..", "..", "examples", "m5", name)
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
	candidate, err := os.ReadFile(fixturePath(t, "candidate.json"))
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

	valid := s
	valid.Expect.Actions = Actions(comparison.Before)
	malformed := valid
	malformed.Name = "malformed"
	malformed.Events = []Input{{ID: "bad", Facts: nil}}
	report, err = Test(context.Background(), source, Suite{SchemaVersion, []Scenario{valid, malformed}})
	require.NoError(t, err)
	require.False(t, report.Passed)
	require.Len(t, report.Scenarios, 2)
	require.True(t, report.Scenarios[0].Passed)
	require.Equal(t, []string{"validation"}, report.Scenarios[1].Mismatches)
	require.NotEmpty(t, report.Scenarios[1].Error)
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
		{[]string{"explain", "-rules", fixturePath(t, "rules.json")}, 0},
		{[]string{"bundle", "-rules", fixturePath(t, "rules.json"), "-scenario", fixturePath(t, "scenario.json")}, 0},
		{[]string{"test", "-rules", fixturePath(t, "rules.json"), "-scenario", fixturePath(t, "suite.json")}, 0},
		{[]string{"compare", "-rules", fixturePath(t, "candidate.json"), "-bundle", path}, 2},
		{[]string{"lint", "-rules", fixturePath(t, "rules.json"), "-channels", "rex_results"}, 0},
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

	malformed := s
	malformed.Name = "malformed"
	malformed.Events = []Input{{ID: "bad", Facts: nil}}
	suitePath := filepath.Join(t.TempDir(), "malformed-suite.json")
	suiteJSON, err := JSON(Suite{SchemaVersion: SchemaVersion, Scenarios: []Scenario{s, malformed}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(suitePath, suiteJSON, 0600))
	var out, diagnostics bytes.Buffer
	code := RunCLI(context.Background(), []string{"test", "-rules", fixturePath(t, "rules.json"), "-scenario", suitePath}, &out, &diagnostics)
	require.Equal(t, 2, code, diagnostics.String())
	var testReport TestReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &testReport))
	require.Len(t, testReport.Scenarios, 2)
	require.True(t, testReport.Scenarios[0].Passed)
	require.False(t, testReport.Scenarios[1].Passed)

	out.Reset()
	diagnostics.Reset()
	require.Equal(t, 1, RunCLI(context.Background(), []string{"simulate", "-h"}, &out, &diagnostics))
	require.Contains(t, diagnostics.String(), "-bundle")
	require.NotContains(t, diagnostics.String(), "-rules")
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
	require.Len(t, r.Diagnostics, 1)
	require.Equal(t, "REX-L004", r.Diagnostics[0].ID)
	require.Contains(t, r.Diagnostics[0].Message, "migrate the calculation to producer facts or declarative rules")
	artifact, err := compiler.CompileBatch(source)
	require.NoError(t, err)
	explanation, err := Explain(artifact)
	require.NoError(t, err)
	require.Equal(t, []string{"x"}, explanation.Rules[0].Dependencies)
}

func TestReportBudgetUsesSnakeCase(t *testing.T) {
	source, scenario := fixture(t)
	bundle, err := NewBundle(source, scenario)
	require.NoError(t, err)
	report, err := Replay(context.Background(), bundle)
	require.NoError(t, err)
	encoded, err := JSON(report)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"actions"`)
	require.Contains(t, string(encoded), `"work"`)
	require.NotContains(t, string(encoded), `"Actions"`)
	require.NotContains(t, string(encoded), `"Work"`)
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

func TestTypedFactToolingContract(t *testing.T) {
	source := []byte(`{"facts":{"temperature":{"type":"number"},"note":{"type":"string","nullable":true},"alert":{"type":"boolean"}},"rules":[{"name":"hot","conditions":{"all":[{"fact":"temperature","operator":"GT","value":30}]},"actions":[{"type":"updateStore","target":"alert","value":true}]}]}`)
	scenario := Scenario{SchemaVersion: SchemaVersion, Name: "typed", InitialState: map[string]interface{}{"note": nil}, Events: []Input{{ID: "reading", Facts: map[string]interface{}{"temperature": float64(31)}}}}
	bundle, err := NewBundle(source, scenario)
	require.NoError(t, err)
	require.Equal(t, compiler.TypedFactsVersion, bundle.Manifest.ExecutionContract)
	report, err := Replay(context.Background(), bundle)
	require.NoError(t, err)
	require.Equal(t, true, report.FinalState["alert"])

	artifact, err := compiler.CompileBatch(source)
	require.NoError(t, err)
	explanation, err := Explain(artifact)
	require.NoError(t, err)
	require.Equal(t, compiler.TypedFactsVersion, explanation.ExecutionContract)
	require.True(t, explanation.Facts["note"].Nullable)

	badInitial := scenario
	badInitial.InitialState = map[string]interface{}{"temperature": "hot"}
	_, err = NewBundle(source, badInitial)
	require.ErrorContains(t, err, "initial state")

	badEvent := scenario
	badEvent.Events = []Input{{ID: "bad", Facts: map[string]interface{}{"extra": true}}}
	_, err = NewBundle(source, badEvent)
	require.ErrorContains(t, err, "undeclared")

	for name, invalid := range map[string]string{
		"unsupported type": strings.Replace(string(source), `"type":"number"`, `"type":"object"`, 1),
		"nullable type":    strings.Replace(string(source), `"type":"number"`, `"type":"number","nullable":"yes"`, 1),
		"extra field":      strings.Replace(string(source), `"type":"number"`, `"type":"number","extra":true`, 1),
	} {
		t.Run("lint "+name, func(t *testing.T) {
			lint := Lint([]byte(invalid), nil)
			require.Len(t, lint.Diagnostics, 1)
			require.Equal(t, "REX-L006", lint.Diagnostics[0].ID)
		})
	}
}

func TestTemporalReplayUsesExplicitProcessingTime(t *testing.T) {
	source := []byte(`{"rules":[{"name":"sustained","conditions":{"all":[{"fact":"hot","operator":"EQ","value":true,"for":"5m"}]},"actions":[{"type":"updateStore","target":"alert","value":true}]}]}`)
	scenario := Scenario{SchemaVersion: SchemaVersion, Name: "temporal", InitialState: map[string]interface{}{}, Events: []Input{
		{ID: "start", At: "2026-09-09T12:00:00Z", Facts: map[string]interface{}{"hot": true}},
		{ID: "before", At: "2026-09-09T12:04:59.999999999Z", Facts: map[string]interface{}{"hot": true}},
		{ID: "boundary", At: "2026-09-09T12:05:00Z", Facts: map[string]interface{}{"hot": true}},
	}}
	bundle, err := NewBundle(source, scenario)
	require.NoError(t, err)
	require.Equal(t, compiler.TemporalVersion, bundle.Manifest.ExecutionContract)
	report, err := Replay(context.Background(), bundle)
	require.NoError(t, err)
	require.Len(t, Actions(report), 1)
	require.Equal(t, true, report.FinalState["alert"])

	missing := scenario
	missing.Events = append([]Input(nil), scenario.Events...)
	missing.Events[0].At = ""
	_, err = NewBundle(source, missing)
	require.ErrorContains(t, err, "requires processing time")

	regressing := scenario
	regressing.Events = append([]Input(nil), scenario.Events...)
	regressing.Events[1].At = "2026-09-09T11:59:59Z"
	_, err = NewBundle(source, regressing)
	require.ErrorContains(t, err, "precedes the prior event")

	forged := scenario
	forged.InitialState = map[string]interface{}{store.InternalStatePrefix + "forged": "2000-01-01T00:00:00Z"}
	_, err = NewBundle(source, forged)
	require.ErrorContains(t, err, "reserved internal state prefix")

	nonTemporal, plain := fixture(t)
	plain.Events[0].At = "2026-09-09T12:00:00Z"
	_, err = NewBundle(nonTemporal, plain)
	require.ErrorContains(t, err, "non-temporal replay")
}

func TestChangeOnlyExplainLintAndReplay(t *testing.T) {
	source := []byte(`{"rules":[{"name":"change","emit":"on_change","conditions":{"all":[{"fact":"trigger","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`)
	scenario := Scenario{SchemaVersion: SchemaVersion, Name: "change-only", InitialState: map[string]interface{}{"out": true}, Events: []Input{{ID: "same", Facts: map[string]interface{}{"trigger": true}}}}
	bundle, err := NewBundle(source, scenario)
	require.NoError(t, err)
	require.Equal(t, compiler.ChangeOnlyVersion, bundle.Manifest.ExecutionContract)
	report, err := Replay(context.Background(), bundle)
	require.NoError(t, err)
	require.Equal(t, true, report.FinalState["out"])
	actions := Actions(report)
	require.Len(t, actions, 1)
	require.True(t, actions[0].Suppressed)

	artifact, err := compiler.CompileBatch(source)
	require.NoError(t, err)
	explanation, err := Explain(artifact)
	require.NoError(t, err)
	require.Equal(t, []string{compiler.CapabilityChangeOnly}, explanation.Capabilities)
	require.Equal(t, "on_change", explanation.Rules[0].Emit)
	require.Equal(t, []string{"out", "trigger"}, explanation.Rules[0].Dependencies)

	self := []byte(`{"rules":[{"name":"stable","emit":"on_change","conditions":{"all":[{"fact":"out","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`)
	require.Empty(t, Lint(self, nil).Diagnostics, "change-only self-writes terminate after observing equal persisted state")

	missing := scenario
	missing.Name = "change-only-missing"
	missing.InitialState = map[string]interface{}{}
	report, err = Replay(context.Background(), mustBundle(t, source, missing))
	require.NoError(t, err)
	require.False(t, Actions(report)[0].Suppressed)
	require.Equal(t, true, report.FinalState["out"])

	ambiguous := scenario
	ambiguous.Events = []Input{{ID: "ambiguous", Facts: map[string]interface{}{"trigger": true, "out": true}}}
	_, err = NewBundle(source, ambiguous)
	require.ErrorContains(t, err, "includes change-only target")
}

func mustBundle(t *testing.T, source []byte, scenario Scenario) Bundle {
	t.Helper()
	bundle, err := NewBundle(source, scenario)
	require.NoError(t, err)
	return bundle
}
