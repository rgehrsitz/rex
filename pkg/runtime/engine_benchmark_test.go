// rex/pkg/runtime/engine_benchmark_test.go

package runtime

import (
	"encoding/hex"
	"testing"

	"rgehrsitz/rex/pkg/compiler"

	"github.com/redis/go-redis/v9"
)

// mockStore implements the store.Store interface for testing
type mockStore struct{}

func (m *mockStore) SetFact(key string, value interface{}) error              { return nil }
func (m *mockStore) GetFact(key string) (interface{}, error)                  { return nil, nil }
func (m *mockStore) MGetFacts(keys ...string) (map[string]interface{}, error) { return nil, nil }
func (m *mockStore) SetAndPublishFact(key string, value interface{}) error    { return nil }
func (m *mockStore) Subscribe(channels ...string) *redis.PubSub               { return nil }
func (m *mockStore) ReceiveFacts() <-chan *redis.Message                      { return nil }

// hexBytecode is the hexadecimal representation of the real bytecode
const hexBytecode = "01000000000000000000000001000000B8000000D10000001E010000241174656D70657261747572655F616C6572740F0B74656D7065726174757265130000000000003E4004196C0000000F0868756D6964697479130000000000004940021953000000282A0B75706461746553746F72652B1274656D70657261747572655F7374617475732D046869676829282A0B75706461746553746F72652B0B616C6572745F6C6576656C2C000000000000004029234C303031251100000074656D70657261747572655F616C657274000000000B00000074656D7065726174757265010000001100000074656D70657261747572655F616C6572740800000068756D6964697479010000001100000074656D70657261747572655F616C6572741100000074656D70657261747572655F616C657274020000000B00000074656D70657261747572650800000068756D6964697479"

// Decode the hexadecimal string into a byte slice
var realBytecode, _ = hex.DecodeString(hexBytecode)

// createMockEngine creates a mock engine for benchmarking
func createMockEngine() *Engine {
	return &Engine{
		bytecode: realBytecode,
		ruleExecutionIndex: []compiler.RuleExecutionIndex{
			{RuleName: "temperature_alert", ByteOffset: 28},
		},
		factRuleIndex: map[string][]string{
			"temperature": {"temperature_alert"},
			"humidity":    {"temperature_alert"},
		},
		factDependencyIndex: []compiler.FactDependencyIndex{
			{
				RuleName: "temperature_alert",
				Facts:    []string{"temperature", "humidity"},
			},
		},
		Facts: map[string]interface{}{
			"temperature":        25.0,
			"humidity":           60.0,
			"temperature_status": "",
			"alert_level":        0,
		},
		store: &mockStore{},
	}
}

func BenchmarkProcessFactUpdate(b *testing.B) {
	engine := createMockEngine()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.ProcessFactUpdate("temperature", float64(25+i%10))
	}
}

func BenchmarkEvaluateRule(b *testing.B) {
	engine := createMockEngine()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.evaluateRule("temperature_alert")
	}
}

func BenchmarkCompare(b *testing.B) {
	engine := createMockEngine()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.compare(30.0, 25.0, compiler.GT_FLOAT)
	}
}

func BenchmarkExecuteAction(b *testing.B) {
	engine := createMockEngine()
	action := compiler.Action{
		Type:   "updateStore",
		Target: "temperature_status",
		Value:  "high",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.executeAction(action)
	}
}

func BenchmarkGetStats(b *testing.B) {
	engine := createMockEngine()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.GetStats()
	}
}

func BenchmarkFullRuleEvaluation(b *testing.B) {
	engine := createMockEngine()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.ProcessFactUpdate("temperature", float64(25+i%10))
	}
}
