package tooling

import (
	"bytes"
	"context"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"rgehrsitz/rex/pkg/compiler"
	"testing"
)

func TestOwnershipCheckCLI(t *testing.T) {
	source, _ := fixture(t)
	artifact, err := compiler.CompileBatch(source)
	require.NoError(t, err)
	plan, err := PlanPartitions(artifact)
	require.NoError(t, err)
	var facts []string
	for _, group := range plan.Groups {
		facts = append(facts, group.Facts...)
	}
	report, err := CheckOwnership(artifact, facts)
	require.NoError(t, err)
	require.False(t, HasLintErrors(report))
	dir := t.TempDir()
	rules := filepath.Join(dir, "rules.json")
	spec := filepath.Join(dir, "ownership.json")
	require.NoError(t, os.WriteFile(rules, source, 0600))
	for _, tc := range []struct {
		body string
		code int
	}{{`{"facts":["unrelated"]}`, 2}, {`{"facts":[]}`, 1}, {`{}`, 1}, {`{"facts":["a"],"extra":true}`, 1}} {
		require.NoError(t, os.WriteFile(spec, []byte(tc.body), 0600))
		var out, diag bytes.Buffer
		require.Equal(t, tc.code, RunCLI(context.Background(), []string{"partition-check", "-rules", rules, "-ownership", spec}, &out, &diag), diag.String())
		if tc.code == 2 {
			require.Contains(t, out.String(), "REX-P001")
		} else {
			require.Empty(t, out.String())
		}
	}
	encoded, err := JSON(OwnershipSpec{Facts: facts})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(spec, encoded, 0600))
	var out, diag bytes.Buffer
	require.Equal(t, 0, RunCLI(context.Background(), []string{"partition-check", "-rules", rules, "-ownership", spec}, &out, &diag), diag.String())
}
