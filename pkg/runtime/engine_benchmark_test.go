package runtime

import (
	"runtime"
	"testing"
	"time"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/scripting"
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
					{
						Type:   "updateStore",
						Target: "heat_index",
						Value:  "{calculate_heat_index}",
					},
				},
				Scripts: map[string]compiler.Script{
					"calculate_heat_index": {
						Params: []string{"temperature", "humidity"},
						Body:   "return temperature * 1.8 + 32 + (humidity / 100) * 10;",
					},
				},
			},
		},
	}

	// Generate bytecode
	bytecodeFile := compiler.GenerateBytecode(ruleset)

	engine := &Engine{
		bytecode:            bytecodeFile.Instructions,
		ruleExecutionIndex:  bytecodeFile.RuleExecIndex,
		factRuleIndex:       bytecodeFile.FactRuleLookupIndex,
		factDependencyIndex: bytecodeFile.FactDependencyIndex,
		Facts: map[string]interface{}{
			"temperature":        25.0,
			"humidity":           60.0,
			"temperature_status": "",
			"heat_index":         0.0,
		},
		store:        redisStore,
		ScriptEngine: scripting.NewSafeVM(),
	}

	// Initialize Redis store with the same facts
	for key, value := range engine.Facts {
		err := redisStore.SetFact(key, value)
		if err != nil {
			b.Fatalf("Failed to set fact in Redis store: %v", err)
		}
	}

	// Add the script to the engine
	err := engine.ScriptEngine.SetScript("calculate_heat_index", compiler.Script{
		Params: []string{"temperature", "humidity"},
		Body:   "return temperature * 1.8 + 32 + (humidity / 100) * 10;",
	})
	if err != nil {
		b.Fatalf("Failed to set script: %v", err)
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

func BenchmarkSetScript(b *testing.B) {
	s, redisStore := setupMiniRedis(b)
	defer s.Close()

	engine := createMockEngine(b, redisStore)
	script := compiler.Script{
		Params: []string{"temperature", "humidity"},
		Body:   "return temperature + humidity;",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.ScriptEngine.SetScript("calculate_heat_index", script)
	}
}

func BenchmarkRunScript(b *testing.B) {
	s, redisStore := setupMiniRedis(b)
	defer s.Close()

	engine := createMockEngine(b, redisStore)

	expectedResult := 25.0*1.8 + 32 + (60.0/100)*10

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		initialGoroutines := runtime.NumGoroutine()
		result, err := engine.ScriptEngine.RunScript("calculate_heat_index", map[string]interface{}{"temperature": 25.0, "humidity": 60.0}, 1*time.Second)
		elapsed := time.Since(start)
		finalGoroutines := runtime.NumGoroutine()

		// Log and verify results
		b.Logf("Script execution time: %s, Goroutines before: %d, after: %d", elapsed, initialGoroutines, finalGoroutines)
		if err != nil {
			b.Fatalf("Failed to run script: %v (elapsed time: %s)", err, elapsed)
		}

		if result != expectedResult {
			b.Fatalf("Incorrect result: got %v, want %v (elapsed time: %s)", result, expectedResult, elapsed)
		}
	}
}
