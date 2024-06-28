package runtime

import (
	"encoding/hex"
	"testing"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"

	"github.com/alicebob/miniredis/v2"
)

// setupMiniRedis creates a new miniredis instance and returns it along with a RedisStore
func setupMiniRedis(b *testing.B) (*miniredis.Miniredis, *store.RedisStore) {
	s, err := miniredis.Run()
	if err != nil {
		b.Fatalf("Failed to create miniredis: %v", err)
	}

	redisStore := store.NewRedisStore(s.Addr(), "", 0)
	return s, redisStore
}

// hexBytecode is the hexadecimal representation of the real bytecode
const hexBytecode = "01000000000000000000000001000000B8000000D10000001E010000241174656D70657261747572655F616C6572740F0B74656D7065726174757265130000000000003E4004196C0000000F0868756D6964697479130000000000004940021953000000282A0B75706461746553746F72652B1274656D70657261747572655F7374617475732D046869676829282A0B75706461746553746F72652B0B616C6572745F6C6576656C2C000000000000004029234C303031251100000074656D70657261747572655F616C657274000000000B00000074656D7065726174757265010000001100000074656D70657261747572655F616C657274080000006875"

// createMockEngine creates a mock engine for benchmarking
// createMockEngine creates a mock engine for benchmarking
func createMockEngine(redisStore *store.RedisStore) *Engine {
	realBytecode, _ := hex.DecodeString(hexBytecode)

	engine := &Engine{
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
		store: redisStore,
	}

	return engine
}

func BenchmarkProcessFactUpdate(b *testing.B) {
	s, redisStore := setupMiniRedis(b)
	defer s.Close()

	engine := createMockEngine(redisStore)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.ProcessFactUpdate("temperature", float64(25+i%10))
	}
}

func BenchmarkEvaluateRule(b *testing.B) {
	s, redisStore := setupMiniRedis(b)
	defer s.Close()

	engine := createMockEngine(redisStore)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.evaluateRule("temperature_alert")
	}
}

func BenchmarkCompare(b *testing.B) {
	s, redisStore := setupMiniRedis(b)
	defer s.Close()

	engine := createMockEngine(redisStore)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.compare(30.0, 25.0, compiler.GT_FLOAT)
	}
}

func BenchmarkExecuteAction(b *testing.B) {
	s, redisStore := setupMiniRedis(b)
	defer s.Close()

	engine := createMockEngine(redisStore)
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
	s, redisStore := setupMiniRedis(b)
	defer s.Close()

	engine := createMockEngine(redisStore)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.GetStats()
	}
}

func BenchmarkFullRuleEvaluation(b *testing.B) {
	s, redisStore := setupMiniRedis(b)
	defer s.Close()

	engine := createMockEngine(redisStore)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.ProcessFactUpdate("temperature", float64(25+i%10))
	}
}
