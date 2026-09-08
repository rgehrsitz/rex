package semantics

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/logging"
	rex "rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"
)

type scenario struct {
	Name        string                   `json:"name"`
	Contract    string                   `json:"contract"`
	Rules       json.RawMessage          `json:"ruleset"`
	Initial     map[string]interface{}   `json:"initial"`
	Batches     []map[string]interface{} `json:"batches"`
	FailRead    int                      `json:"fail_read"`
	FailAction  int                      `json:"fail_action"`
	FailPublish int                      `json:"fail_publish"`
	Limit       int                      `json:"action_limit"`
	Deliver     int                      `json:"deliver"`
	Expected    *outcome                 `json:"expected,omitempty"`
}
type attempt struct {
	Key     string      `json:"key"`
	Value   interface{} `json:"value"`
	Outcome string      `json:"outcome"`
	At      string      `json:"at"`
}
type checkpoint struct {
	Facts, Local map[string]interface{}
	Errors       []string
	ActionCount  int
}

type outcome struct {
	Checkpoints []checkpoint           `json:"checkpoints,omitempty"`
	Facts       map[string]interface{} `json:"facts"`
	Local       map[string]interface{} `json:"local"`
	Actions     []attempt              `json:"actions"`
	Errors      []string               `json:"errors"`
}
type clock struct{ tick int }

func (c *clock) now() time.Time {
	t := time.Unix(0, 0).UTC().Add(time.Duration(c.tick) * time.Second)
	c.tick++
	return t
}

type controlledStore struct {
	*store.MemoryStore
	plan    scenario
	clock   *clock
	reads   int
	actions []attempt
	queue   []store.FactUpdate
}

func (s *controlledStore) MGetFactsContext(ctx context.Context, keys ...string) (map[string]interface{}, error) {
	s.reads++
	if s.reads == s.plan.FailRead {
		return nil, errors.New("injected read failure")
	}
	return s.MemoryStore.MGetFactsContext(ctx, keys...)
}
func (s *controlledStore) SetAndPublishFactContext(ctx context.Context, key string, value interface{}) error {
	n := len(s.actions) + 1
	a := attempt{Key: key, Value: value, Outcome: "succeeded", At: s.clock.now().Format(time.RFC3339)}
	var err error
	switch n {
	case s.plan.FailAction:
		a.Outcome = "write_failed"
		err = errors.New("injected write failure")
	case s.plan.FailPublish:
		a.Outcome = "publish_failed"
		err = s.MemoryStore.SetFactContext(ctx, key, value)
		if err == nil {
			err = errors.New("injected publish failure")
		}
	default:
		err = s.MemoryStore.SetAndPublishFactContext(ctx, key, value)
		if err == nil {
			s.queue = append(s.queue, store.FactUpdate{Key: key, Value: value})
		}
	}
	s.actions = append(s.actions, a)
	return err
}

type evaluator interface {
	ProcessFactUpdateContext(context.Context, string, interface{}) error
}

func runScenario(t *testing.T, s scenario, oracle bool, mutate func(*compiler.BytecodeFile)) outcome {
	t.Helper()
	// Decode two independent trees; the oracle sees authored AST order. Only the
	// production side uses Parse, whose validation may reorder condition groups.
	var authored compiler.Ruleset
	if err := json.Unmarshal(s.Rules, &authored); err != nil {
		t.Fatal(err)
	}
	var presence struct {
		Rules []map[string]json.RawMessage `json:"rules"`
	}
	_ = json.Unmarshal(s.Rules, &presence)
	for i := range authored.Rules {
		if _, ok := presence.Rules[i]["priority"]; !ok {
			authored.Rules[i].Priority = compiler.DefaultRulePriority
		}
		if len(authored.Rules[i].Scripts) > 0 {
			t.Fatal("scripts excluded from deterministic oracle")
		}
	}
	memory, err := store.NewMemoryStore(s.Initial)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	backend := &controlledStore{MemoryStore: memory, plan: s, clock: &clock{}, actions: []attempt{}}
	limit := s.Limit
	if limit == 0 {
		limit = rex.DefaultMaxActionsPerEvaluation
	}
	local := map[string]interface{}{}
	var eval evaluator
	var inspect func() map[string]interface{}
	if oracle {
		eval = &reference{rules: authored.Rules, facts: local, store: backend, limit: limit}
	} else {
		parsed, err := compiler.Parse(s.Rules)
		if err != nil {
			t.Fatal(err)
		}
		artifact, err := compiler.GenerateBytecode(parsed)
		if err != nil {
			t.Fatal(err)
		}
		if mutate != nil {
			mutate(&artifact)
		}
		path := filepath.Join(t.TempDir(), "rules.bytecode")
		if err = compiler.WriteBytecodeToFile(path, artifact); err != nil {
			t.Fatal(err)
		}
		engine, err := rex.NewEngineFromFile(path, backend, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer engine.Shutdown()
		engine.SetMaxActionsPerEvaluation(limit)
		eval = engine
		local = engine.Snapshot()
		inspect = engine.Snapshot
	}
	out := outcome{Errors: []string{}}
	record := func() {
		if inspect != nil {
			local = inspect()
		}
		var saved map[string]interface{}
		raw, _ := json.Marshal(local)
		_ = json.Unmarshal(raw, &saved)
		out.Checkpoints = append(out.Checkpoints, checkpoint{Facts: memorySnapshot(t, memory), Local: saved, Errors: append([]string{}, out.Errors...), ActionCount: len(backend.actions)})
	}
	process := func(batch map[string]interface{}, persist bool) {
		keys := []string{}
		for k := range batch {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		// Producers persist the entire batch before daemon-style sorted dispatch.
		if persist {
			for _, k := range keys {
				if err := memory.SetFactContext(context.Background(), k, batch[k]); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, k := range keys {
			if err := eval.ProcessFactUpdateContext(context.Background(), k, batch[k]); err != nil {
				out.Errors = append(out.Errors, err.Error())
				record()
				break
			}
			record()
		}
	}
	for _, batch := range s.Batches {
		process(batch, true)
	}
	for i := 0; i < s.Deliver && len(backend.queue) > 0; i++ {
		event := backend.queue[0]
		backend.queue = backend.queue[1:]
		process(map[string]interface{}{event.Key: event.Value}, false)
	}
	if s.Deliver > 0 && len(backend.queue) > 0 {
		out.Errors = append(out.Errors, "harness delivery limit reached")
	}
	out.Facts = memorySnapshot(t, memory)
	if inspect != nil {
		local = inspect()
	}
	out.Local = local
	out.Actions = backend.actions
	return out
}
func quiet(t *testing.T) {
	t.Helper()
	old := logging.Logger
	logging.Logger = zerolog.Nop()
	t.Cleanup(func() { logging.Logger = old })
}
func loadScenarios(t *testing.T) []scenario {
	t.Helper()
	data, err := os.ReadFile("testdata/current-v3.json")
	if err != nil {
		t.Fatal(err)
	}
	var s []scenario
	if err = json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestCurrentV3Scenarios(t *testing.T) {
	quiet(t)
	for _, s := range loadScenarios(t) {
		t.Run(s.Name, func(t *testing.T) {
			if s.Contract != "current-v3" || s.Expected == nil {
				t.Fatal("explicit contract and expectations required")
			}
			want := runScenario(t, s, true, nil)
			got := runScenario(t, s, false, nil)
			authoredWant := want
			authoredWant.Checkpoints = nil
			if !reflect.DeepEqual(*s.Expected, authoredWant) {
				t.Fatalf("authored contract differs from reference:\nexpected %s\nreference %s", asJSON(s.Expected), asJSON(want))
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("differential mismatch:\nreference %s\nbytecode %s", asJSON(want), asJSON(got))
			}
		})
	}
}
func asJSON(v interface{}) string { b, _ := json.MarshalIndent(v, "", "  "); return string(b) }

func TestDifferentialDetectsSemanticFault(t *testing.T) {
	quiet(t)
	for _, tc := range []struct {
		name     string
		scenario int
		mutate   func(*compiler.BytecodeFile)
	}{
		{"comparator", 0, func(b *compiler.BytecodeFile) {
			if compiler.Opcode(b.Instructions[20]) != compiler.GT_FLOAT {
				t.Fatal("mutation site changed")
			}
			b.Instructions[20] = byte(compiler.LT_FLOAT)
		}},
		{"action-target", 0, func(b *compiler.BytecodeFile) {
			b.Instructions = bytes.Replace(b.Instructions, []byte("out"), []byte("bad"), 1)
		}},
		{"priority", 1, func(b *compiler.BytecodeFile) {
			for i := range b.RuleExecIndex {
				r := &b.RuleExecIndex[i]
				r.Priority = 20 - r.Priority
				start := r.ByteOffset + 3 + len(r.RuleName)
				binary.LittleEndian.PutUint32(b.Instructions[start:start+4], uint32(r.Priority))
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := loadScenarios(t)[tc.scenario]
			want := runScenario(t, s, true, nil)
			got := runScenario(t, s, false, tc.mutate)
			if reflect.DeepEqual(want, got) {
				t.Fatal("oracle failed to detect semantic mutation")
			}
		})
	}
}

// Compact failure output is directly replayable through REX_SCENARIO; no
// transient files or random seed discovery are required.
func TestReplayScenario(t *testing.T) {
	path := os.Getenv("REX_SCENARIO")
	if path == "" {
		t.Skip("set REX_SCENARIO to a minimized scenario JSON file")
	}
	quiet(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s scenario
	if err = json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if a, b := runScenario(t, s, true, nil), runScenario(t, s, false, nil); !reflect.DeepEqual(a, b) {
		t.Fatalf("replayed mismatch: %s\n%s", asJSON(a), asJSON(b))
	}
}

func memorySnapshot(t *testing.T, memory *store.MemoryStore) map[string]interface{} {
	t.Helper()
	values, err := memory.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return values
}
func TestCurrentV3Defaults(t *testing.T) {
	// Pin the characterized contract even though harness setup uses production
	// constants. A default change requires explicit corpus/migration review.
	if compiler.DefaultRulePriority != 10 {
		t.Fatal("current-v3 priority default changed")
	}
	if rex.DefaultMaxActionsPerEvaluation != 32 {
		t.Fatal("current-v3 action limit changed")
	}
}
