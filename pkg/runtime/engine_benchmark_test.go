package runtime

import (
	"context"
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

	redisStore, err := store.NewRedisStore(context.Background(), store.RedisOptions{Addr: s.Addr()})
	if err != nil {
		b.Fatal(err)
	}
	return s, redisStore
}

func createMockEngine(b *testing.B, redisStore *store.RedisStore) *Engine {
	// Create a sample ruleset
	ruleset := &compiler.Ruleset{
		Rules: []compiler.Rule{
			{
				Name: "temperature_alert",
				Conditions: compiler.ConditionGroup{
					All: []*compiler.ConditionOrGroup{
						{
							Fact:     "temperature",
							Operator: "GT",
							Value:    30.0,
						},
						{
							Fact:     "humidity",
							Operator: "GT",
							Value:    60.0,
						},
					},
				},
				Actions: []compiler.Action{
					{
						Type:   "updateStore",
						Target: "temperature_status",
						Value:  "high",
					},
				},
			},
		},
	}

	// Generate bytecode
	bytecodeFile := mustGenerateBytecode(b, ruleset)

	engine := &Engine{
		bytecode:            bytecodeFile.Instructions,
		ruleExecutionIndex:  make(map[string]compiler.RuleExecutionIndex),
		factRuleIndex:       bytecodeFile.FactRuleLookupIndex,
		factDependencyIndex: make(map[string][]string),
		facts: map[string]interface{}{
			"temperature":        25.0,
			"humidity":           60.0,
			"temperature_status": "",
			"heat_index":         0.0,
		},
		store: redisStore,
	}

	// Initialize Redis store with the same facts
	for _, rule := range bytecodeFile.RuleExecIndex {
		engine.ruleExecutionIndex[rule.RuleName] = rule
	}
	for _, dependency := range bytecodeFile.FactDependencyIndex {
		engine.factDependencyIndex[dependency.RuleName] = dependency.Facts
	}
	for key, value := range engine.facts {
		err := redisStore.SetFact(key, value)
		if err != nil {
			b.Fatalf("Failed to set fact in Redis store: %v", err)
		}
	}

	return engine
}

func BenchmarkProcessFactUpdate(b *testing.B) {
	s, redisStore := setupMiniRedis(b)
	defer s.Close()

	engine := createMockEngine(b, redisStore)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.ProcessFactUpdate("temperature", float64(25+i%10))
	}
}

func BenchmarkEvaluateRule(b *testing.B) {
	s, redisStore := setupMiniRedis(b)
	defer s.Close()

	engine := createMockEngine(b, redisStore)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.evaluateRule("temperature_alert")
	}
}

func BenchmarkCompare(b *testing.B) {
	s, redisStore := setupMiniRedis(b)
	defer s.Close()

	engine := createMockEngine(b, redisStore)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.compare(30.0, 25.0, compiler.GT_FLOAT)
	}
}

func BenchmarkExecuteAction(b *testing.B) {
	s, redisStore := setupMiniRedis(b)
	defer s.Close()

	engine := createMockEngine(b, redisStore)
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

func BenchmarkFullRuleEvaluation(b *testing.B) {
	s, redisStore := setupMiniRedis(b)
	defer s.Close()

	engine := createMockEngine(b, redisStore)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.ProcessFactUpdate("temperature", float64(25+i%10))
	}
}
