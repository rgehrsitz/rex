package compiler

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/require"
)

func TestV4SchemaParserAgreement(t *testing.T) {
	raw, err := os.ReadFile("../../examples/rex-rules-schema.json")
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
		strings.Replace(v4Source, `"name":"r"`, `"name":"r","priority":null`, 1),
		strings.Replace(v4Source, `"name":"r"`, `"name":"r","priority":-1`, 1),
		strings.Replace(v4Source, `"name":"r"`, `"name":"r","unknown":true`, 1),
		strings.Replace(v4Source, `"operator":"EQ"`, `"operator":"LT"`, 1),
		strings.Replace(v4Source, `"operator":"EQ"`, `"operator":"NEQ"`, 1),
		strings.Replace(v4Source, `"all":[`, `"all":[{"any":[]},`, 1),
	}
	for i, source := range cases {
		var value interface{}
		require.NoError(t, json.Unmarshal([]byte(source), &value))
		schemaErr := resolved.Validate(value)
		_, parserErr := ParseBatch([]byte(source))
		require.Equal(t, schemaErr == nil, parserErr == nil, "case %d schema=%v parser=%v", i, schemaErr, parserErr)
	}
}
