package compiler

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

const v4Source = `{"rules":[{"name":"r","conditions":{"all":[{"fact":"a","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":1}]}]}`
const v5Source = `{"facts":{"a":{"type":"boolean"},"out":{"type":"number"}},"rules":[{"name":"r","conditions":{"all":[{"fact":"a","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":1}]}]}`

func TestBatchArtifactCompatibilityAndDeterminism(t *testing.T) {
	a, err := CompileBatch([]byte(v4Source))
	require.NoError(t, err)
	b, err := CompileBatch([]byte(v4Source))
	require.NoError(t, err)
	require.Equal(t, a, b)
	require.Equal(t, BatchVersion, binary.LittleEndian.Uint32(a))
	require.Equal(t, "59ca979b9a73e65c8f97e40b10dee5b52a94affe71a90d181f8d6640554186b0", fmt.Sprintf("%x", sha256.Sum256(a)), "untyped v4 artifact bytes are a compatibility contract")
	_, err = DecodeBatch(a)
	require.NoError(t, err)
	for i := range a {
		_, err = DecodeBatch(a[:i])
		require.Error(t, err)
	}
	for i := range a {
		bad := append([]byte{}, a...)
		bad[i] ^= 1
		_, err = DecodeBatch(bad)
		require.Error(t, err, "byte %d", i)
	}
	old := append([]byte{}, a...)
	binary.LittleEndian.PutUint32(old, Version)
	_, err = DecodeBatch(old)
	require.Error(t, err)
}
func TestBatchRejectsUnavailableCapabilitiesAndBounds(t *testing.T) {
	for _, source := range []string{
		strings.Replace(v4Source, `"value":true`, `"value":null`, 1),
		strings.Replace(v4Source, `"value":true`, `"value":{}`, 1),
		strings.Replace(v4Source, `"value":1`, `"value":"{script}"`, 1),
		strings.Repeat("[", 65) + strings.Repeat("]", 65),
		strings.Repeat(" ", MaxProgramBytes+1),
	} {
		_, err := CompileBatch([]byte(source))
		require.Error(t, err)
	}
}
func FuzzDecodeBatch(f *testing.F) {
	artifact, _ := CompileBatch([]byte(v4Source))
	f.Add(artifact)
	typedArtifact, _ := CompileBatch([]byte(v5Source))
	f.Add(typedArtifact)
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = DecodeBatch(data) })
}

func TestBatchRejectsAmbiguousAndEmptyConditions(t *testing.T) {
	for _, group := range []string{`{}`, `{"all":[],"any":[]}`, `{"all":[{"fact":"a","operator":"EQ","value":true}],"any":[{"fact":"b","operator":"EQ","value":true}]}`} {
		source := `{"rules":[{"name":"r","conditions":` + group + `,"actions":[{"type":"updateStore","target":"out","value":true}]}]}`
		_, err := CompileBatch([]byte(source))
		require.Error(t, err)
	}
}

func TestBatchScriptFieldErrorNamesRule(t *testing.T) {
	source := strings.Replace(v4Source, `"name":"r"`, `"name":"r","scripts":{}`, 1)
	_, err := CompileBatch([]byte(source))
	require.ErrorContains(t, err, `rule "r"`)
}

func TestTypedFactArtifactContract(t *testing.T) {
	legacy, err := CompileBatch([]byte(v4Source))
	require.NoError(t, err)
	require.Equal(t, BatchVersion, binary.LittleEndian.Uint32(legacy))

	typed, err := CompileBatch([]byte(v5Source))
	require.NoError(t, err)
	require.Equal(t, TypedFactsVersion, binary.LittleEndian.Uint32(typed))
	rules, err := DecodeBatch(typed)
	require.NoError(t, err)
	require.Equal(t, FactBoolean, rules.Facts["a"].Type)
	require.Equal(t, FactNumber, rules.Facts["out"].Type)

	v5WithoutDeclarations := append([]byte(nil), legacy...)
	binary.LittleEndian.PutUint32(v5WithoutDeclarations, TypedFactsVersion)
	_, err = DecodeBatch(v5WithoutDeclarations)
	require.ErrorContains(t, err, "v5 artifact requires typed fact declarations")

	v4WithDeclarations := append([]byte(nil), typed...)
	binary.LittleEndian.PutUint32(v4WithDeclarations, BatchVersion)
	_, err = DecodeBatch(v4WithDeclarations)
	require.ErrorContains(t, err, "v4 artifact cannot contain typed fact declarations")
}

func TestTypedFactCompilerValidation(t *testing.T) {
	cases := map[string]string{
		"empty declarations":          strings.Replace(v5Source, `"facts":{"a":{"type":"boolean"},"out":{"type":"number"}}`, `"facts":{}`, 1),
		"null declarations":           strings.Replace(v5Source, `"facts":{"a":{"type":"boolean"},"out":{"type":"number"}}`, `"facts":null`, 1),
		"unsupported type":            strings.Replace(v5Source, `"boolean"`, `"object"`, 1),
		"undeclared condition":        strings.Replace(v5Source, `"fact":"a"`, `"fact":"missing"`, 1),
		"condition constant mismatch": strings.Replace(v5Source, `"value":true`, `"value":"true"`, 1),
		"undeclared action target":    strings.Replace(v5Source, `"target":"out"`, `"target":"missing"`, 1),
		"action value mismatch":       strings.Replace(v5Source, `"value":1`, `"value":"one"`, 1),
		"nullable null action":        strings.Replace(strings.Replace(v5Source, `"type":"number"`, `"type":"number","nullable":true`, 1), `"value":1`, `"value":null`, 1),
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := CompileBatch([]byte(source))
			require.Error(t, err)
			if name == "nullable null action" {
				require.ErrorContains(t, err, "Null action values are unsupported")
			} else {
				require.True(t, IsTypedFactError(err), "%v", err)
			}
		})
	}
}

func TestLegacyCompilerRejectsTypedFacts(t *testing.T) {
	rules, err := Parse([]byte(v5Source))
	require.NoError(t, err)
	_, err = GenerateBytecode(rules)
	require.ErrorContains(t, err, "v5 batch compiler")
}

func TestTemporalArtifactContractAndValidation(t *testing.T) {
	temporalSource := strings.Replace(v4Source, `"value":true}`, `"value":true,"for":"5m"}`, 1)
	artifact, err := CompileBatch([]byte(temporalSource))
	require.NoError(t, err)
	require.Equal(t, TemporalVersion, binary.LittleEndian.Uint32(artifact))
	rules, err := DecodeBatch(artifact)
	require.NoError(t, err)
	require.Equal(t, "5m", rules.Rules[0].Conditions.All[0].For)

	typedTemporal := strings.Replace(v5Source, `"value":true}`, `"value":true,"for":"1s"}`, 1)
	artifact, err = CompileBatch([]byte(typedTemporal))
	require.NoError(t, err)
	require.Equal(t, TemporalVersion, binary.LittleEndian.Uint32(artifact))

	v6WithoutTemporal, err := CompileBatch([]byte(v4Source))
	require.NoError(t, err)
	binary.LittleEndian.PutUint32(v6WithoutTemporal, TemporalVersion)
	_, err = DecodeBatch(v6WithoutTemporal)
	require.ErrorContains(t, err, "v6 artifact requires temporal conditions")

	for name, replacement := range map[string]string{
		"empty":       `"for":""`,
		"null":        `"for":null`,
		"invalid":     `"for":"later"`,
		"zero":        `"for":"0s"`,
		"negative":    `"for":"-1s"`,
		"over bound":  `"for":"8761h"`,
		"wrong type":  `"for":5`,
		"group field": `"for":"1s","all":[{"fact":"a","operator":"EQ","value":true}]`,
	} {
		t.Run(name, func(t *testing.T) {
			var source string
			if name == "group field" {
				source = strings.Replace(v4Source, `"all":[{"fact":"a","operator":"EQ","value":true}]`, replacement, 1)
			} else {
				source = strings.Replace(v4Source, `"value":true`, `"value":true,`+replacement, 1)
			}
			_, err := CompileBatch([]byte(source))
			require.Error(t, err)
		})
	}

	reserved := strings.Replace(temporalSource, `"target":"out"`, `"target":"__rex_temporal_user"`, 1)
	_, err = CompileBatch([]byte(reserved))
	require.ErrorContains(t, err, "reserved temporal state prefix")
	reservedV4 := strings.Replace(v4Source, `"target":"out"`, `"target":"__rex_temporal_user"`, 1)
	_, err = CompileBatch([]byte(reservedV4))
	require.ErrorContains(t, err, "reserved temporal state prefix")

	parsed, err := Parse([]byte(temporalSource))
	require.NoError(t, err)
	_, err = GenerateBytecode(parsed)
	require.ErrorContains(t, err, "v6 batch compiler")
}

func TestTemporalConditionCountIsBounded(t *testing.T) {
	var source strings.Builder
	source.WriteString(`{"rules":[{"name":"bounded","conditions":{"all":[`)
	for i := 0; i <= MaxTemporalConditions; i++ {
		if i > 0 {
			source.WriteByte(',')
		}
		source.WriteString(`{"fact":"a","operator":"EQ","value":true,"for":"1s"}`)
	}
	source.WriteString(`]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`)
	_, err := CompileBatch([]byte(source.String()))
	require.ErrorContains(t, err, "temporal conditions")
}
