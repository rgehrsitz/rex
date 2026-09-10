package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const v4Source = `{"rules":[{"name":"r","conditions":{"all":[{"fact":"a","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":1}]}]}`
const v5Source = `{"facts":{"a":{"type":"boolean"},"out":{"type":"number"}},"rules":[{"name":"r","conditions":{"all":[{"fact":"a","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":1}]}]}`
const v7Source = `{"rules":[{"name":"r","emit":"on_change","conditions":{"all":[{"fact":"a","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":1}]}]}`

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
	changeOnlyArtifact, _ := CompileBatch([]byte(v7Source))
	f.Add(changeOnlyArtifact)
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
	require.Equal(t, "f6af69a3fdffd86dbd3175d180a71d7dc253700f0da3aeea8a00bf903bdadbd2", fmt.Sprintf("%x", sha256.Sum256(typed)), "v5 artifact bytes are a compatibility contract")
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

func TestV6ExampleArtifactBytesRemainCompatible(t *testing.T) {
	source := []byte(`{"facts":{"temperature":{"type":"number"},"alert":{"type":"boolean"}},"rules":[{"name":"sustained-high-temperature","conditions":{"all":[{"fact":"temperature","operator":"GTE","value":30,"for":"5m"}]},"actions":[{"type":"updateStore","target":"alert","value":true}]}]}`)
	artifact, err := CompileBatch(source)
	require.NoError(t, err)
	require.Equal(t, TemporalVersion, binary.LittleEndian.Uint32(artifact))
	require.Equal(t, "62e72984a8511cdb9b8cd7851e68d7bd619c431ef8fb485050c327f620310c93", fmt.Sprintf("%x", sha256.Sum256(artifact)), "v6 artifact bytes are a compatibility contract")
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

func TestChangeOnlyArtifactContractAndCapabilities(t *testing.T) {
	artifact, err := CompileBatch([]byte(v7Source))
	require.NoError(t, err)
	again, err := CompileBatch([]byte(v7Source))
	require.NoError(t, err)
	require.Equal(t, artifact, again)
	require.Equal(t, ChangeOnlyVersion, binary.LittleEndian.Uint32(artifact))
	rules, err := DecodeBatch(artifact)
	require.NoError(t, err)
	require.Equal(t, "on_change", rules.Rules[0].Emit)
	require.Equal(t, []string{CapabilityChangeOnly}, rules.Capabilities)
	badCapabilities := append([]byte(nil), artifact...)
	index := bytes.Index(badCapabilities[batchHeaderSize:], []byte(CapabilityChangeOnly))
	require.NotEqual(t, -1, index)
	badCapabilities[batchHeaderSize+index] = 'X'
	binary.LittleEndian.PutUint32(badCapabilities[4:], crc32.ChecksumIEEE(badCapabilities[batchHeaderSize:]))
	_, err = DecodeBatch(badCapabilities)
	require.ErrorContains(t, err, "capabilities")

	combined := `{"facts":{"a":{"type":"boolean"},"out":{"type":"number"}},"rules":[{"name":"r","emit":"on_change","conditions":{"all":[{"fact":"a","operator":"EQ","value":true,"for":"1m"}]},"actions":[{"type":"updateStore","target":"out","value":1}]}]}`
	artifact, err = CompileBatch([]byte(combined))
	require.NoError(t, err)
	rules, err = DecodeBatch(artifact)
	require.NoError(t, err)
	require.Equal(t, []string{CapabilityChangeOnly, CapabilityTemporal, CapabilityTypedFacts}, rules.Capabilities)

	v6WithChangeOnly := append([]byte(nil), artifact...)
	binary.LittleEndian.PutUint32(v6WithChangeOnly, TemporalVersion)
	_, err = DecodeBatch(v6WithChangeOnly)
	require.Error(t, err)
	v7WithoutChangeOnly, err := CompileBatch([]byte(v4Source))
	require.NoError(t, err)
	binary.LittleEndian.PutUint32(v7WithoutChangeOnly, ChangeOnlyVersion)
	_, err = DecodeBatch(v7WithoutChangeOnly)
	require.ErrorContains(t, err, "v7 artifact capabilities")
}

func TestChangeOnlySourceValidationAndLegacyRejection(t *testing.T) {
	for _, source := range []string{
		strings.Replace(v7Source, `"on_change"`, `"always"`, 1),
		strings.Replace(v7Source, `"on_change"`, `"sometimes"`, 1),
		strings.Replace(v7Source, `{"rules":`, `{"capabilities":["change_only"],"rules":`, 1),
	} {
		_, err := CompileBatch([]byte(source))
		require.Error(t, err)
	}
	rules, err := Parse([]byte(v7Source))
	require.NoError(t, err)
	_, err = GenerateBytecode(rules)
	require.ErrorContains(t, err, "v7 batch compiler")
}
