package semantics

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"testing"

	"rgehrsitz/rex/pkg/compiler"
)

func generated(seed int64) scenario {
	rng := rand.New(rand.NewSource(seed))
	var node func(int) *compiler.ConditionOrGroup
	node = func(depth int) *compiler.ConditionOrGroup {
		if depth > 0 && rng.Intn(3) == 0 {
			children := []*compiler.ConditionOrGroup{node(depth - 1), node(depth - 1)}
			if rng.Intn(2) == 0 {
				return &compiler.ConditionOrGroup{All: children}
			}
			return &compiler.ConditionOrGroup{Any: children}
		}
		n := &compiler.ConditionOrGroup{Fact: fmt.Sprintf("f%d", rng.Intn(4))}
		switch rng.Intn(3) {
		case 0:
			n.Value = float64(rng.Intn(3))
			n.Operator = []string{"EQ", "NEQ", "LT", "LTE", "GT", "GTE"}[rng.Intn(6)]
		case 1:
			n.Value = []string{"x", "abc", "z"}[rng.Intn(3)]
			n.Operator = []string{"EQ", "NEQ", "CONTAINS", "NOT_CONTAINS"}[rng.Intn(4)]
		case 2:
			n.Value = rng.Intn(2) == 0
			n.Operator = []string{"EQ", "NEQ"}[rng.Intn(2)]
		}
		return n
	}
	rules := compiler.Ruleset{}
	for i := 0; i < 1+rng.Intn(5); i++ {
		r := compiler.Rule{Name: fmt.Sprintf("r%d", i), Priority: rng.Intn(3)}
		nodes := []*compiler.ConditionOrGroup{node(3), node(2)}
		if rng.Intn(2) == 0 {
			r.Conditions.All = nodes
		} else {
			r.Conditions.Any = nodes
		}
		for j := 0; j < 1+rng.Intn(3); j++ {
			r.Actions = append(r.Actions, compiler.Action{Type: "updateStore", Target: fmt.Sprintf("f%d", rng.Intn(6)), Value: float64(rng.Intn(3))})
		}
		rules.Rules = append(rules.Rules, r)
	}
	data, _ := json.Marshal(rules)
	s := scenario{Name: fmt.Sprintf("seed-%d", seed), Contract: "current-v3", Rules: data, Initial: map[string]interface{}{}, Limit: 1 + rng.Intn(3)}
	value := func() interface{} {
		return []interface{}{nil, float64(0), float64(1), float64(2), "abc", "x", true, false}[rng.Intn(8)]
	}
	for i := 0; i < 4; i++ {
		if rng.Intn(3) != 0 {
			s.Initial[fmt.Sprintf("f%d", i)] = value()
		}
	}
	for i := 0; i < 3; i++ {
		batch := map[string]interface{}{}
		for j := 0; j < 1+rng.Intn(4); j++ {
			batch[fmt.Sprintf("f%d", rng.Intn(4))] = value()
		}
		s.Batches = append(s.Batches, batch)
	}
	switch rng.Intn(5) {
	case 0:
		s.FailRead = 1
	case 1:
		s.FailAction = 1
	case 2:
		s.FailPublish = 2
	}
	return s
}

// Greedy reduction produces a replayable smaller scenario, not a claim of
// globally minimal input. Each accepted reduction must preserve the mismatch.
func minimize(s scenario, fails func(scenario) bool) scenario {
	for changed := true; changed; {
		changed = false
		for i := range s.Batches {
			if len(s.Batches) <= 1 {
				break
			}
			c := s
			c.Batches = append(append([]map[string]interface{}{}, s.Batches[:i]...), s.Batches[i+1:]...)
			if fails(c) {
				s = c
				changed = true
				break
			}
		}
		var rules compiler.Ruleset
		_ = json.Unmarshal(s.Rules, &rules)
		for i := range rules.Rules {
			if len(rules.Rules) <= 1 {
				break
			}
			c := s
			rr := compiler.Ruleset{Rules: append(append([]compiler.Rule{}, rules.Rules[:i]...), rules.Rules[i+1:]...)}
			c.Rules, _ = json.Marshal(rr)
			if fails(c) {
				s = c
				changed = true
				break
			}
		}
	}
	return s
}

func TestGeneratedCurrentV3(t *testing.T) {
	quiet(t)
	start, end := int64(0), int64(128)
	if value := os.Getenv("REX_SEED"); value != "" {
		seed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		start, end = seed, seed+1
	}
	for seed := start; seed < end; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			s := generated(seed)
			fails := func(c scenario) bool {
				return !reflect.DeepEqual(runScenario(t, c, true, nil), runScenario(t, c, false, nil))
			}
			if fails(s) {
				small := minimize(s, fails)
				t.Fatalf("seed %d mismatch; save this JSON and replay with REX_SCENARIO=<file> go test ./internal/semantics -run TestReplayScenario:\n%s", seed, asJSON(small))
			}
		})
	}
}

func TestReducerPreservesFailure(t *testing.T) {
	s := generated(1)
	s.Batches = []map[string]interface{}{{"keep": true}, {"drop": false}, {"drop2": false}}
	fails := func(c scenario) bool {
		for _, b := range c.Batches {
			if b["keep"] == true {
				return true
			}
		}
		return false
	}
	small := minimize(s, fails)
	if !fails(small) || len(small.Batches) != 1 {
		t.Fatal("reducer lost failure or failed to reduce")
	}
	var rules compiler.Ruleset
	_ = json.Unmarshal(small.Rules, &rules)
	if len(rules.Rules) != 1 {
		t.Fatal("rules not reduced")
	}
}

func TestAllOperatorTruthTables(t *testing.T) {
	quiet(t)
	for _, tc := range []struct {
		op                string
		constant, yes, no interface{}
	}{
		{"EQ", float64(2), float64(2), float64(1)}, {"NEQ", float64(2), float64(1), float64(2)},
		{"LT", float64(2), float64(1), float64(2)}, {"LTE", float64(2), float64(2), float64(3)},
		{"GT", float64(2), float64(3), float64(2)}, {"GTE", float64(2), float64(2), float64(1)},
		{"EQ", "x", "x", "y"}, {"NEQ", "x", "y", "x"},
		{"CONTAINS", "x", "axb", "ab"}, {"NOT_CONTAINS", "x", "ab", "axb"},
		{"EQ", true, true, false}, {"NEQ", true, false, true},
	} {
		t.Run(fmt.Sprintf("%T-%s", tc.constant, tc.op), func(t *testing.T) {
			for _, sample := range []struct {
				value   interface{}
				matched bool
			}{{tc.yes, true}, {tc.no, false}, {nil, false}, {map[string]interface{}{"wrong": "type"}, false}} {
				rules := compiler.Ruleset{Rules: []compiler.Rule{{Name: "truth", Priority: 10, Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{Fact: "a", Operator: tc.op, Value: tc.constant}}}, Actions: []compiler.Action{{Type: "updateStore", Target: "out", Value: true}}}}}
				data, _ := json.Marshal(rules)
				s := scenario{Contract: "current-v3", Rules: data, Batches: []map[string]interface{}{{"a": sample.value}}}
				a, b := runScenario(t, s, true, nil), runScenario(t, s, false, nil)
				if !reflect.DeepEqual(a, b) || (len(a.Actions) > 0) != sample.matched {
					t.Fatalf("truth table mismatch for %v: %s / %s", sample.value, asJSON(a), asJSON(b))
				}
			}
		})
	}
}
