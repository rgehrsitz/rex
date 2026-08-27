package runtime

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"rgehrsitz/rex/pkg/compiler"
)

func fuzzSeedBytecode(f *testing.F) []byte {
	f.Helper()

	ruleset := &compiler.Ruleset{Rules: []compiler.Rule{{
		Name: "temperature_rule",
		Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{
			Fact:     "temperature",
			Operator: "GT",
			Value:    30.0,
		}}},
		Actions: []compiler.Action{{Type: "updateStore", Target: "status", Value: "hot"}},
	}}}
	filename := f.TempDir() + "/valid.bytecode"
	if err := compiler.WriteBytecodeToFile(filename, compiler.GenerateBytecode(ruleset)); err != nil {
		f.Fatal(err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		f.Fatal(err)
	}
	return data
}

func FuzzValidateBytecode(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, compiler.HeaderSize))
	f.Add(fuzzSeedBytecode(f))

	f.Fuzz(func(t *testing.T, data []byte) {
		_ = validateBytecode(data)
	})
}

func TestValidateBytecodeRejectsEveryTruncatedPrefix(t *testing.T) {
	data := validBytecode(t)
	for length := 0; length < len(data); length++ {
		require.Error(t, validateBytecode(data[:length]), "truncated artifact length %d", length)
	}
}

func TestValidateBytecodeRejectsEverySingleByteCorruption(t *testing.T) {
	data := validBytecode(t)
	for offset := range data {
		corrupted := append([]byte(nil), data...)
		corrupted[offset] ^= 0xff
		require.Error(t, validateBytecode(corrupted), "corrupted artifact byte %d", offset)
	}
}
