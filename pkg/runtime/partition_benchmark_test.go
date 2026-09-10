package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"
)

// BenchmarkBatchPartitionBaseline measures the existing single-owner path.
// It deliberately does not simulate parallel ownership or durable Redis I/O.
// Memory publications are drained each iteration to keep retained memory bounded.
func BenchmarkBatchPartitionBaseline(b *testing.B) {
	for _, size := range []int{10, 1000} {
		for _, dense := range []bool{false, true} {
			name := fmt.Sprintf("rules=%d/dense=%t", size, dense)
			b.Run(name, func(b *testing.B) {
				rules := compiler.Ruleset{}
				initial := map[string]interface{}{}
				event := map[string]interface{}{"trigger.0": true}
				snapshot := map[string]store.Fact{}
				for i := 0; i < size; i++ {
					trigger := fmt.Sprintf("trigger.%d", i)
					if dense {
						trigger = "trigger.0"
					}
					gate := fmt.Sprintf("gate.%d", i)
					initial[gate] = true
					snapshot[gate] = store.Fact{State: store.Present, Value: true}
					rules.Rules = append(rules.Rules, compiler.Rule{Name: fmt.Sprintf("rule.%d", i), Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{Fact: trigger, Operator: "EQ", Value: true}, {Fact: gate, Operator: "EQ", Value: true}}}, Actions: []compiler.Action{{Type: "updateStore", Target: fmt.Sprintf("out.%d", i), Value: true}}})
				}
				raw, err := json.Marshal(rules)
				if err != nil {
					b.Fatal(err)
				}
				artifact, err := compiler.CompileBatch(raw)
				if err != nil {
					b.Fatal(err)
				}
				program, err := LoadProgram(artifact)
				if err != nil {
					b.Fatal(err)
				}
				limits := DefaultLimits()
				limits.ActionsPerRound = 1024
				limits.EventFacts = 1024
				expected := 1
				if dense {
					expected = size
				}
				b.Run("evaluate", func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						result, _, err := program.Evaluate(context.Background(), snapshot, event, limits, Budget{}, false)
						if err != nil {
							b.Fatal(err)
						}
						if len(result.Writes) != expected {
							b.Fatalf("got %d writes, want %d", len(result.Writes), expected)
						}
					}
				})
				b.Run("coordinator-memory", func(b *testing.B) {
					memory, err := store.NewMemoryStore(initial)
					if err != nil {
						b.Fatal(err)
					}
					defer memory.Close()
					coordinator, err := NewCoordinator(program, memory, memory, limits)
					if err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						result, err := coordinator.Process(context.Background(), "baseline", event)
						if err != nil {
							b.Fatal(err)
						}
						if len(result.Rounds) != 2 {
							b.Fatalf("got %d rounds, want 2", len(result.Rounds))
						}
						publications, err := memory.DrainPublications()
						if err != nil {
							b.Fatal(err)
						}
						if len(publications) != expected {
							b.Fatalf("got %d publications, want %d", len(publications), expected)
						}
					}
				})
			})
		}
	}
}
