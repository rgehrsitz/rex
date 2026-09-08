package compiler

import (
	"encoding/binary"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

const v4Source = `{"rules":[{"name":"r","conditions":{"all":[{"fact":"a","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":1}]}]}`

func TestBatchArtifactCompatibilityAndDeterminism(t *testing.T) {
	a, err := CompileBatch([]byte(v4Source))
	require.NoError(t, err)
	b, err := CompileBatch([]byte(v4Source))
	require.NoError(t, err)
	require.Equal(t, a, b)
	require.Equal(t, BatchVersion, binary.LittleEndian.Uint32(a))
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
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = DecodeBatch(data) })
}
