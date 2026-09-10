package tooling

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"rgehrsitz/rex/pkg/compiler"
)

func TestPartitionPlanConnectivity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pairs  [][2]string
		groups [][]string
	}{
		{"independent", [][2]string{{"a", "x"}, {"b", "y"}}, [][]string{{"r0"}, {"r1"}}},
		{"shared reader", [][2]string{{"a", "x"}, {"a", "y"}}, [][]string{{"r0", "r1"}}},
		{"shared writer", [][2]string{{"a", "x"}, {"b", "x"}}, [][]string{{"r0", "r1"}}},
		{"derived chain", [][2]string{{"a", "b"}, {"b", "c"}}, [][]string{{"r0", "r1"}}},
		{"transitive bridge", [][2]string{{"a", "x"}, {"b", "y"}, {"x", "y"}, {"c", "z"}}, [][]string{{"r0", "r1", "r2"}, {"r3"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rules compiler.Ruleset
			names := []string{"r0", "r1", "r2", "r3"}
			for i, pair := range tc.pairs {
				rules.Rules = append(rules.Rules, compiler.Rule{Name: names[i], Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{Fact: pair[0], Operator: "EQ", Value: true}}}, Actions: []compiler.Action{{Type: "updateStore", Target: pair[1], Value: true}}})
			}
			source, err := json.Marshal(rules)
			require.NoError(t, err)
			artifact, err := compiler.CompileBatch(source)
			require.NoError(t, err)
			report, err := PlanPartitions(artifact)
			require.NoError(t, err)
			require.True(t, report.AdvisoryOnly)
			var got [][]string
			for _, group := range report.Groups {
				got = append(got, group.Rules)
				require.IsIncreasing(t, group.Facts)
			}
			require.Equal(t, tc.groups, got)
			first, err := JSON(report)
			require.NoError(t, err)
			for i := 0; i < 10; i++ {
				next, err := PlanPartitions(artifact)
				require.NoError(t, err)
				data, err := JSON(next)
				require.NoError(t, err)
				require.Equal(t, first, data)
			}
		})
	}
}

func TestPartitionPlanCapabilitiesAndCLI(t *testing.T) {
	source := []byte(`{"facts":{"a":{"type":"boolean"},"b":{"type":"boolean"},"out":{"type":"boolean"},"unused":{"type":"string"}},"rules":[{"name":"temporal","emit":"on_change","conditions":{"all":[{"any":[{"fact":"a","operator":"EQ","value":true,"for":"1s"},{"fact":"b","operator":"EQ","value":true}]}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`)
	artifact, err := compiler.CompileBatch(source)
	require.NoError(t, err)
	report, err := PlanPartitions(artifact)
	require.NoError(t, err)
	require.Equal(t, uint32(7), report.ExecutionContract)
	require.Equal(t, []string{"a", "b", "out"}, report.Groups[0].Facts)
	require.Equal(t, []string{"unused"}, report.UnusedDeclarations)
	require.Equal(t, Digest(artifact), report.ArtifactSHA256)
	dir := t.TempDir()
	src := filepath.Join(dir, "rules.json")
	bin := filepath.Join(dir, "rules.bin")
	require.NoError(t, os.WriteFile(src, source, 0600))
	require.NoError(t, os.WriteFile(bin, artifact, 0600))
	var expected []byte
	for _, args := range [][]string{{"partition-plan", "-rules", src}, {"partition-plan", "-artifact", bin}} {
		var out, diag bytes.Buffer
		require.Equal(t, 0, RunCLI(context.Background(), args, &out, &diag), diag.String())
		if expected == nil {
			expected = append([]byte{}, out.Bytes()...)
		} else {
			require.Equal(t, expected, out.Bytes())
		}
	}
	for _, args := range [][]string{{"partition-plan"}, {"partition-plan", "-rules", src, "-artifact", bin}, {"partition-plan", "-artifact", src}} {
		var out, diag bytes.Buffer
		require.Equal(t, 1, RunCLI(context.Background(), args, &out, &diag))
		require.Empty(t, out.String())
		require.NotEmpty(t, diag.String())
	}
	_, err = PlanPartitions([]byte("invalid"))
	require.Error(t, err)
}

func TestPartitionPlanAllBatchVersions(t *testing.T) {
	for _, tc := range []struct {
		version                 uint32
		typed, temporal, change bool
	}{
		{4, false, false, false}, {5, true, false, false}, {6, false, true, false}, {7, false, false, true},
	} {
		t.Run(string(rune('0'+tc.version)), func(t *testing.T) {
			node := &compiler.ConditionOrGroup{Fact: "a", Operator: "EQ", Value: true}
			rule := compiler.Rule{Name: "r", Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{node}}, Actions: []compiler.Action{{Type: "updateStore", Target: "b", Value: true}}}
			if tc.temporal {
				node.For = "1s"
			}
			if tc.change {
				rule.Emit = compiler.EmitOnChange
			}
			source := compiler.Ruleset{Rules: []compiler.Rule{rule}}
			if tc.typed {
				source.Facts = map[string]compiler.FactDeclaration{"a": {Type: compiler.FactBoolean}, "b": {Type: compiler.FactBoolean}}
			}
			raw, err := json.Marshal(source)
			require.NoError(t, err)
			artifact, err := compiler.CompileBatch(raw)
			require.NoError(t, err)
			report, err := PlanPartitions(artifact)
			require.NoError(t, err)
			require.Equal(t, tc.version, report.ExecutionContract)
			require.Equal(t, []PartitionGroup{{ID: 0, Rules: []string{"r"}, Facts: []string{"a", "b"}}}, report.Groups)
		})
	}
}
