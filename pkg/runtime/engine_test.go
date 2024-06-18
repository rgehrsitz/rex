// rex/pkg/runtime/engine_test.go

package runtime

import (
	"testing"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"

	"github.com/stretchr/testify/assert"
)

func TestProcessFactUpdate(t *testing.T) {
	ruleset := &compiler.Ruleset{
		Rules: []compiler.Rule{
			{
				Name: "UpdateRule",
				Conditions: compiler.ConditionGroup{
					All: []*compiler.ConditionOrGroup{
						{
							Fact:     "temperature",
							Operator: "GT",
							Value:    30.0,
						},
					},
				},
				Actions: []compiler.Action{
					{
						Type:   "updateFact",
						Target: "alert",
						Value:  true,
					},
				},
			},
		},
	}

	bytecode := compiler.GenerateBytecode(ruleset)
	err := compiler.WriteBytecodeToFile("test_engine_bytecode.bin", bytecode)
	assert.NoError(t, err)

	redisStore := store.NewRedisStore("localhost:6379", "", 0)
	engine, err := NewEngineFromFile("test_engine_bytecode.bin", redisStore)
	assert.NoError(t, err)

	engine.ProcessFactUpdate("temperature", 35.0)
	assert.Equal(t, true, engine.Facts["alert"])
}
