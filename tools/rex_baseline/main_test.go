package main

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/rs/zerolog"
	"rgehrsitz/rex/pkg/logging"
)

func TestMeasurementExercisesFixtureDimensions(t *testing.T) {
	oldLogger, oldLevel := logging.Logger, zerolog.GlobalLevel()
	logging.Logger = zerolog.New(io.Discard)
	zerolog.SetGlobalLevel(zerolog.Disabled)
	t.Cleanup(func() { logging.Logger = oldLogger; zerolog.SetGlobalLevel(oldLevel) })
	base := fixture{Name: "test", Rules: 20, Affected: 4, Dependencies: 2, Shared: 2, Batch: 1, MatchPercent: 100, Actions: 1}
	for _, tc := range []struct {
		name                  string
		change                func(*fixture)
		actions, dependencies int
	}{
		{"shared", func(*fixture) {}, 400, 200},
		{"unique", func(f *fixture) { f.Shared = 0 }, 400, 800},
		{"missing", func(f *fixture) { f.Shared = 0; f.MissingEvery = 2 }, 200, 800},
		{"batch", func(f *fixture) { f.Batch = 4 }, 1600, 2000},
		{"half-match", func(f *fixture) { f.MatchPercent = 50 }, 200, 200},
		{"no-match", func(f *fixture) { f.MatchPercent = 0 }, 0, 200},
		{"actions", func(f *fixture) { f.Actions = 3 }, 1200, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			tc.change(&f)
			if err := f.validate(); err != nil {
				t.Fatal(err)
			}
			r, err := measure(context.Background(), f, options{mode: "memory", source: "test", events: 100, warmup: 10}, 1)
			if err != nil {
				t.Fatal(err)
			}
			if r.Calls.SetPublish != tc.actions || r.Calls.DependencyKeys != tc.dependencies {
				t.Fatalf("unexpected measured work: %+v", r.Calls)
			}
			if r.P50NS > r.P95NS || r.P95NS > r.P99NS || r.P99NS > r.MaxNS || r.EventsPerSecond <= 0 {
				t.Fatalf("invalid timing result: %+v", r)
			}
		})
	}
}

func TestFixtureDeterminismAndValidation(t *testing.T) {
	f := fixture{Name: "test", Rules: 20, Affected: 4, Dependencies: 2, Shared: 1, Batch: 1, MatchPercent: 100, Actions: 1}
	a, _, _ := f.generate()
	b, _, _ := f.generate()
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	if digest(x) != digest(y) {
		t.Fatal("fixture generation is nondeterministic")
	}
	f.MissingEvery = 2
	f.Shared = 2
	if f.validate() == nil {
		t.Fatal("shared-only missing fixture cannot express per-rule absence")
	}
	f.Shared = 1
	f.Batch = 2
	f.MatchPercent = 50
	if f.validate() == nil {
		t.Fatal("mixed-match batches need a different expected-action model")
	}
}
