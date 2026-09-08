package semantics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"rgehrsitz/rex/pkg/compiler"
	rex "rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"
)

// The v4 oracle folds authored AST nodes into -1/0/1 (unknown/false/true).
// It does not use production truth functions, program indices, or traversal.
func oracleV4(source []byte, snapshot map[string]interface{}, event map[string]interface{}) ([]string, []store.Write, int, error) {
	var rules compiler.Ruleset
	if err := json.Unmarshal(source, &rules); err != nil {
		return nil, nil, 0, err
	}
	var raw struct {
		Rules []map[string]json.RawMessage `json:"rules"`
	}
	_ = json.Unmarshal(source, &raw)
	for i := range rules.Rules {
		if _, ok := raw.Rules[i]["priority"]; !ok {
			rules.Rules[i].Priority = 10
		}
	}
	sort.SliceStable(rules.Rules, func(i, j int) bool { return rules.Rules[i].Priority < rules.Rules[j].Priority })
	facts := map[string]interface{}{}
	for k, v := range snapshot {
		facts[k] = v
	}
	for k, v := range event {
		facts[k] = v
	}
	var match func([]*compiler.ConditionOrGroup, bool) int
	match = func(nodes []*compiler.ConditionOrGroup, all bool) int {
		unknown := false
		for _, n := range nodes {
			value := -1
			switch {
			case len(n.All) > 0:
				value = match(n.All, true)
			case len(n.Any) > 0:
				value = match(n.Any, false)
			default:
				actual := facts[n.Fact]
				if actual != nil && reflect.TypeOf(actual) == reflect.TypeOf(n.Value) {
					if leaf(actual, n.Value, n.Operator) {
						value = 1
					} else {
						value = 0
					}
				}
			}
			if all && value == 0 {
				return 0
			}
			if !all && value == 1 {
				return 1
			}
			unknown = unknown || value < 0
		}
		if unknown {
			return -1
		}
		if all {
			return 1
		}
		return 0
	}
	fired := []string{}
	writes := []store.Write{}
	actions := 0
	targets := map[string]interface{}{}
	for _, r := range rules.Rules {
		deps := map[string]bool{}
		dependencies(r.Conditions.All, deps)
		dependencies(r.Conditions.Any, deps)
		affected := false
		for k := range event {
			affected = affected || deps[k]
		}
		if !affected {
			continue
		}
		yes := match(r.Conditions.All, true)
		if len(r.Conditions.Any) > 0 {
			yes = match(r.Conditions.Any, false)
		}
		if yes != 1 {
			continue
		}
		fired = append(fired, r.Name)
		for _, a := range r.Actions {
			actions++
			if value, ok := targets[a.Target]; ok {
				if !reflect.DeepEqual(value, a.Value) {
					return nil, nil, 0, fmt.Errorf("conflict")
				}
				continue
			}
			targets[a.Target] = a.Value
			writes = append(writes, store.Write{Key: a.Target, Value: a.Value})
		}
	}
	return fired, writes, actions, nil
}
func compareV4Scenario(t *testing.T, s scenario) {
	t.Helper()
	data, err := compiler.CompileBatch(s.Rules)
	if err != nil {
		t.Fatal(err)
	}
	program, err := rex.LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := map[string]store.Fact{}
	for k, v := range s.Initial {
		raw, _ := json.Marshal(v)
		snapshot[k] = store.DecodeFact(raw)
	}
	for _, event := range s.Batches {
		wantRules, wantWrites, wantActions, wantErr := oracleV4(s.Rules, s.Initial, event)
		got, _, err := program.Evaluate(context.Background(), snapshot, event, rex.DefaultLimits(), rex.Budget{}, true)
		if wantErr != nil {
			if err == nil || !strings.Contains(err.Error(), "conflicting") {
				t.Fatalf("expected conflict, got %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(wantRules, got.Rules) || !reflect.DeepEqual(wantWrites, got.Writes) || wantActions != len(got.Actions) {
			t.Fatalf("v4 mismatch for %s\noracle %s %s\nprogram %s", s.Name, asJSON(wantRules), asJSON(wantWrites), asJSON(got))
		}
	}
}
func TestIndependentV4Generated(t *testing.T) {
	quiet(t)
	for seed := int64(0); seed < 128; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) { compareV4Scenario(t, generated(seed)) })
	}
}
func TestIndependentV4TruthTables(t *testing.T) {
	quiet(t)
	for _, all := range []bool{true, false} {
		for _, a := range []interface{}{true, false, nil} {
			for _, b := range []interface{}{true, false, nil} {
				mode := "any"
				if all {
					mode = "all"
				}
				source := fmt.Sprintf(`{"rules":[{"name":"r","conditions":{"%s":[{"fact":"a","operator":"EQ","value":true},{"fact":"b","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":true}]}]}`, mode)
				s := scenario{Name: "truth", Rules: []byte(source), Batches: []map[string]interface{}{{"a": a, "b": b}}}
				compareV4Scenario(t, s)
			}
		}
	}
}

func TestAuthoredBatchV4MigrationCorpus(t *testing.T) {
	quiet(t)
	var cases []struct {
		Name, Contract string
		Rules          json.RawMessage        `json:"ruleset"`
		Snapshot       map[string]interface{} `json:"snapshot"`
		Event          map[string]interface{} `json:"event"`
		RulesExpected  []string               `json:"expected_rules"`
		Writes         []store.Write          `json:"expected_writes"`
		Actions        int                    `json:"expected_actions"`
		Error          string                 `json:"expected_error"`
	}
	data, err := os.ReadFile("testdata/batch-v4.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if c.Contract != "batch-v4" {
				t.Fatal("wrong contract")
			}
			rules, writes, actions, err := oracleV4(c.Rules, c.Snapshot, c.Event)
			if c.Error != "" {
				if err == nil || !strings.Contains(err.Error(), c.Error) {
					t.Fatalf("expected %s, got %v", c.Error, err)
				}
			} else if err != nil || !reflect.DeepEqual(rules, c.RulesExpected) || !reflect.DeepEqual(writes, c.Writes) || actions != c.Actions {
				t.Fatalf("authored expectation differs from independent oracle: %v %v %v %v", rules, writes, actions, err)
			}
			compareV4Scenario(t, scenario{Name: c.Name, Rules: c.Rules, Initial: c.Snapshot, Batches: []map[string]interface{}{c.Event}})
		})
	}
}
