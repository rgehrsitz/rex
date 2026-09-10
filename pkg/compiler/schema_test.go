package compiler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/require"
)

func TestSchemaParserAgreement(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	packageDir := filepath.Dir(sourceFile)
	raw, err := os.ReadFile(filepath.Join(packageDir, "..", "..", "examples", "rex-rules-schema.json"))
	require.NoError(t, err)
	var schema jsonschema.Schema
	require.NoError(t, json.Unmarshal(raw, &schema))
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	cases := []string{v4Source,
		strings.Replace(v4Source, `"all":`, `"any":`, 1),
		strings.Replace(v4Source, `"all":`, `"any":[],"all":`, 1),
		strings.Replace(v4Source, `"all":`, `"any":null,"all":`, 1),
		strings.Replace(v4Source, `"name":"r"`, `"name":"r","scripts":{}`, 1),
		strings.Replace(v4Source, `"value":true`, `"value":null`, 1),
		strings.Replace(v4Source, `"value":true`, `"value":[]`, 1),
		strings.Replace(v4Source, `"value":true`, `"value":""`, 1),
		strings.Replace(v4Source, `"value":1`, `"value":""`, 1),
		strings.Replace(v4Source, `"value":1`, `"value":"{missing}"`, 1),
		strings.Replace(v4Source, `"target":"out"`, `"target":"__rex_temporal_forged"`, 1),
		strings.Replace(v4Source, `"name":"r"`, `"name":"r","priority":null`, 1),
		strings.Replace(v4Source, `"name":"r"`, `"name":"r","priority":-1`, 1),
		strings.Replace(v4Source, `"name":"r"`, `"name":"r","unknown":true`, 1),
		strings.Replace(v4Source, `"operator":"EQ"`, `"operator":"LT"`, 1),
		strings.Replace(v4Source, `"operator":"EQ"`, `"operator":"NEQ"`, 1),
		strings.Replace(v4Source, `"all":[`, `"all":[{"any":[]},`, 1),
		strings.Replace(v4Source, `"value":true`, `"value":true,"for":"5m"`, 1),
		v7Source,
		strings.Replace(v7Source, `"emit":"on_change"`, `"emit":"always"`, 1),
		strings.Replace(v7Source, `"emit":"on_change"`, `"emit":null`, 1),
		strings.Replace(v7Source, `"rules":`, `"capabilities":["change_only"],"rules":`, 1),
		v5Source,
		strings.Replace(v5Source, `"facts":{"a":{"type":"boolean"},"out":{"type":"number"}}`, `"facts":null`, 1),
		strings.Replace(v5Source, `"facts":{"a":{"type":"boolean"},"out":{"type":"number"}}`, `"facts":{}`, 1),
		strings.Replace(v5Source, `"type":"boolean"`, `"type":"object"`, 1),
		strings.Replace(v5Source, `"type":"boolean"`, `"type":"boolean","nullable":"yes"`, 1),
		strings.Replace(v5Source, `"type":"boolean"`, `"type":"boolean","extra":true`, 1),
		strings.Replace(v5Source, `"a":`, `"`+strings.Repeat("a", 256)+`":`, 1),
		strings.Replace(strings.Replace(v5Source, `"type":"number"`, `"type":"number","nullable":true`, 1), `"value":1`, `"value":null`, 1),
	}
	for i, source := range cases {
		var value interface{}
		require.NoError(t, json.Unmarshal([]byte(source), &value))
		schemaErr := resolved.Validate(value)
		_, parserErr := ParseBatch([]byte(source))
		require.Equal(t, schemaErr == nil, parserErr == nil, "case %d schema=%v parser=%v", i, schemaErr, parserErr)
	}

	corpusData, err := os.ReadFile(filepath.Join(packageDir, "testdata", "schema-batch.json"))
	require.NoError(t, err)
	var corpus []struct {
		Name     string          `json:"name"`
		Valid    bool            `json:"valid"`
		Document json.RawMessage `json:"document"`
	}
	require.NoError(t, json.Unmarshal(corpusData, &corpus))
	for _, test := range corpus {
		t.Run(test.Name, func(t *testing.T) {
			var value interface{}
			require.NoError(t, json.Unmarshal(test.Document, &value))
			schemaErr := resolved.Validate(value)
			_, parserErr := ParseBatch(test.Document)
			require.Equal(t, test.Valid, schemaErr == nil, "schema: %v", schemaErr)
			require.Equal(t, test.Valid, parserErr == nil, "parser: %v", parserErr)
		})
	}

	_, err = ParseBatch(append([]byte(v4Source), 0xff))
	require.ErrorContains(t, err, "valid UTF-8")
}
