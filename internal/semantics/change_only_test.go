package semantics

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"rgehrsitz/rex/pkg/compiler"
	rex "rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"
)

// changeOnlyOracle is intentionally independent of the production evaluator.
// It models the v7 scalar contract directly from a persisted target value.
func changeOnlyOracle(persisted store.Fact, proposed interface{}) bool {
	if persisted.State != store.Present {
		return false
	}
	switch want := proposed.(type) {
	case float64:
		got, ok := persisted.Value.(float64)
		return ok && got == want
	case string:
		got, ok := persisted.Value.(string)
		return ok && got == want
	case bool:
		got, ok := persisted.Value.(bool)
		return ok && got == want
	default:
		return false
	}
}

func TestIndependentV7ChangeOnlyEquality(t *testing.T) {
	random := rand.New(rand.NewSource(84))
	values := []interface{}{true, false, "", "ready", float64(0), float64(-1), float64(1.5)}
	states := []store.FactState{store.Present, store.Missing, store.Null, store.Invalid}
	programs := make(map[string]*rex.Program, len(values))
	for _, proposed := range values {
		source := []byte(fmt.Sprintf(`{"rules":[{"name":"change","emit":"on_change","conditions":{"all":[{"fact":"trigger","operator":"EQ","value":true}]},"actions":[{"type":"updateStore","target":"out","value":%s}]}]}`, jsonScalar(proposed)))
		artifact, err := compiler.CompileBatch(source)
		if err != nil {
			t.Fatal(err)
		}
		programs[scalarKey(proposed)], err = rex.LoadProgram(artifact)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 256; i++ {
		proposed := values[random.Intn(len(values))]
		persisted := store.Fact{State: states[random.Intn(len(states))]}
		if persisted.State == store.Present {
			persisted.Value = values[random.Intn(len(values))]
		}
		program := programs[scalarKey(proposed)]
		got, _, err := program.Evaluate(context.Background(), map[string]store.Fact{"out": persisted}, map[string]interface{}{"trigger": true}, rex.DefaultLimits(), rex.Budget{}, true)
		if err != nil {
			t.Fatal(err)
		}
		wantSuppressed := changeOnlyOracle(persisted, proposed)
		if len(got.Actions) != 1 || got.Actions[0].Suppressed != wantSuppressed || (len(got.Writes) == 0) != wantSuppressed {
			t.Fatalf("case %d persisted=%#v proposed=%#v: got %#v", i, persisted, proposed, got)
		}
		if !wantSuppressed && !reflect.DeepEqual(got.Writes, []store.Write{{Key: "out", Value: proposed}}) {
			t.Fatalf("case %d: wrong write %#v", i, got.Writes)
		}
	}
}

func scalarKey(value interface{}) string { return fmt.Sprintf("%T:%v", value, value) }

func jsonScalar(value interface{}) string {
	switch value := value.(type) {
	case string:
		return fmt.Sprintf("%q", value)
	default:
		return fmt.Sprint(value)
	}
}
