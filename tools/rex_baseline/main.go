// rex_baseline measures the synchronous runtime API with deterministic inputs.
// It deliberately excludes ingress, JSON decoding, and derived-event consumption.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/logging"
	rex "rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"
)

type fixture struct {
	Name         string `json:"name"`
	Rules        int    `json:"rules"`
	Affected     int    `json:"affected"`
	Dependencies int    `json:"dependencies"`
	Shared       int    `json:"shared"`
	MissingEvery int    `json:"missing_every,omitempty"`
	Batch        int    `json:"batch"`
	MatchPercent int    `json:"match_percent"`
	Actions      int    `json:"actions"`
}

func (f fixture) validate() error {
	if f.Name == "" || f.Rules < 1 || f.Affected < 1 || f.Affected > f.Rules ||
		f.Dependencies < 0 || f.Shared < 0 || f.Shared > f.Dependencies ||
		f.MissingEvery < 0 || (f.MissingEvery > 0 && f.Shared == f.Dependencies) ||
		f.Batch < 1 || f.MatchPercent < 0 || f.MatchPercent > 100 ||
		(f.Batch > 1 && f.MatchPercent != 100) || f.Actions < 1 || f.Actions > 32 {
		return fmt.Errorf("invalid fixture: %+v", f)
	}
	return nil
}

// The affected rules are last, making execution-index lookup cost visible.
// Outputs are unique per rule/action and do not feed any rule in this fixture.
func (f fixture) generate() (*compiler.Ruleset, map[string]interface{}, []string) {
	facts := map[string]interface{}{}
	inputs := make([]string, f.Batch)
	for j := range inputs {
		inputs[j] = fmt.Sprintf("m0:input:%02d", j)
		facts[inputs[j]] = float64(1)
	}
	rules := &compiler.Ruleset{}
	for i := 0; i < f.Rules; i++ {
		r := compiler.Rule{Name: fmt.Sprintf("rule-%05d", i), Priority: 10}
		add := func(key string, threshold float64) {
			r.Conditions.All = append(r.Conditions.All, &compiler.ConditionOrGroup{Fact: key, Operator: "GT", Value: threshold})
		}
		if i < f.Rules-f.Affected {
			add(fmt.Sprintf("m0:unrelated:%05d", i), 0)
		} else {
			for j, key := range inputs {
				threshold := float64(0)
				if j == 0 {
					threshold = float64(100 - f.MatchPercent)
				}
				add(key, threshold)
			}
			for j := 0; j < f.Dependencies; j++ {
				key := fmt.Sprintf("m0:dep:%05d:%02d", i, j)
				if j < f.Shared {
					key = fmt.Sprintf("m0:shared:%02d", j)
				}
				add(key, 0)
				missing := f.MissingEvery > 0 && (i-(f.Rules-f.Affected))%f.MissingEvery == 0 && j == f.Shared
				if !missing {
					facts[key] = float64(1)
				}
			}
		}
		for j := 0; j < f.Actions; j++ {
			r.Actions = append(r.Actions, compiler.Action{Type: "updateStore", Target: fmt.Sprintf("m0:output:%05d:%02d", i, j), Value: float64(1)})
		}
		rules.Rules = append(rules.Rules, r)
	}
	return rules, facts, inputs
}

type memoryStore map[string]interface{}

func (m memoryStore) Close() error { return nil }
func (m memoryStore) SetFactContext(ctx context.Context, k string, v interface{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m[k] = v
	return nil
}
func (m memoryStore) SetAndPublishFactContext(ctx context.Context, k string, v interface{}) error {
	return m.SetFactContext(ctx, k, v)
}
func (m memoryStore) GetFactContext(ctx context.Context, k string) (interface{}, error) {
	return m[k], ctx.Err()
}
func (m memoryStore) MGetFactsContext(ctx context.Context, keys ...string) (map[string]interface{}, error) {
	values := make(map[string]interface{}, len(keys))
	for _, k := range keys {
		values[k] = m[k]
	}
	return values, ctx.Err()
}

type counts struct {
	MGet           int `json:"mget"`
	DependencyKeys int `json:"dependency_keys"`
	Get            int `json:"get"`
	Set            int `json:"set"`
	SetPublish     int `json:"set_publish"`
}
type countedStore struct {
	store.ContextStore
	Calls counts
}

func (s *countedStore) MGetFactsContext(ctx context.Context, keys ...string) (map[string]interface{}, error) {
	s.Calls.MGet++
	s.Calls.DependencyKeys += len(keys)
	return s.ContextStore.MGetFactsContext(ctx, keys...)
}
func (s *countedStore) GetFactContext(ctx context.Context, k string) (interface{}, error) {
	s.Calls.Get++
	return s.ContextStore.GetFactContext(ctx, k)
}
func (s *countedStore) SetFactContext(ctx context.Context, k string, v interface{}) error {
	s.Calls.Set++
	return s.ContextStore.SetFactContext(ctx, k, v)
}
func (s *countedStore) SetAndPublishFactContext(ctx context.Context, k string, v interface{}) error {
	s.Calls.SetPublish++
	return s.ContextStore.SetAndPublishFactContext(ctx, k, v)
}

type result struct {
	Time            string           `json:"time_utc"`
	Source          string           `json:"source_id"`
	Go              string           `json:"go"`
	OS              string           `json:"os"`
	Arch            string           `json:"arch"`
	CPUs            int              `json:"cpus"`
	Procs           int              `json:"gomaxprocs"`
	Mode            string           `json:"mode"`
	Logging         string           `json:"logging"`
	RedisVersion    string           `json:"redis_version,omitempty"`
	Fixture         fixture          `json:"fixture"`
	RulesSHA256     string           `json:"rules_sha256"`
	BytecodeSHA256  string           `json:"bytecode_sha256"`
	Run             int              `json:"run"`
	Events          int              `json:"events"`
	Warmup          int              `json:"warmup"`
	ElapsedNS       int64            `json:"elapsed_ns"`
	EventsPerSecond float64          `json:"events_per_second"`
	P50NS           int64            `json:"p50_ns"`
	P95NS           int64            `json:"p95_ns"`
	P99NS           int64            `json:"p99_ns"`
	MaxNS           int64            `json:"max_ns"`
	AllocsPerEvent  float64          `json:"allocs_per_event"`
	BytesPerEvent   float64          `json:"bytes_per_event"`
	HeapBefore      uint64           `json:"heap_before_bytes"`
	HeapAfter       uint64           `json:"heap_after_bytes"`
	RetainedDelta   int64            `json:"retained_delta_bytes"`
	FactsRetained   int              `json:"facts_retained"`
	Calls           counts           `json:"store_calls"`
	RedisCalls      map[string]int64 `json:"redis_commands,omitempty"`
	Churn           int              `json:"churn"`
}

type options struct {
	mode, address, level, source string
	events, warmup, runs, churn  int
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func commandCounts(ctx context.Context, c *redis.Client) (map[string]int64, error) {
	info, err := c.Info(ctx, "commandstats").Result()
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for _, line := range strings.Split(info, "\n") {
		for _, name := range []string{"get", "mget", "set", "publish"} {
			prefix := "cmdstat_" + name + ":calls="
			if strings.HasPrefix(line, prefix) {
				var n int64
				if _, err := fmt.Sscanf(strings.TrimPrefix(line, prefix), "%d", &n); err != nil {
					return nil, err
				}
				counts[name] = n
			}
		}
	}
	return counts, nil
}

func measure(ctx context.Context, f fixture, o options, run int) (result, error) {
	r := result{Time: time.Now().UTC().Format(time.RFC3339), Source: o.source, Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), Procs: runtime.GOMAXPROCS(0), Mode: o.mode, Logging: o.level + "-discard", Fixture: f, Run: run, Events: o.events, Warmup: o.warmup, Churn: o.churn}
	rules, facts, inputs := f.generate()
	encoded, err := json.Marshal(rules)
	if err != nil {
		return r, err
	}
	r.RulesSHA256 = digest(encoded)
	// Exercise the production parser, writer, and validating file loader.
	parsed, err := compiler.Parse(encoded)
	if err != nil {
		return r, err
	}
	artifact, err := compiler.GenerateBytecode(parsed)
	if err != nil {
		return r, err
	}
	dir, err := os.MkdirTemp("", "rex-baseline-")
	if err != nil {
		return r, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "rules.bytecode")
	if err := compiler.WriteBytecodeToFile(path, artifact); err != nil {
		return r, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	r.BytecodeSHA256 = digest(data)
	var backend store.ContextStore = memoryStore{}
	var control *redis.Client
	if o.mode == "redis" {
		control = redis.NewClient(&redis.Options{Addr: o.address})
		defer control.Close()
		info, err := control.Info(ctx, "server").Result()
		if err != nil {
			return r, err
		}
		for _, line := range strings.Split(info, "\n") {
			if strings.HasPrefix(line, "redis_version:") {
				r.RedisVersion = strings.TrimSpace(strings.TrimPrefix(line, "redis_version:"))
			}
		}
		// Only this tool's namespace is cleared; use a dedicated server regardless.
		keys, err := control.Keys(ctx, "m0:*").Result()
		if err != nil {
			return r, err
		}
		if len(keys) > 0 {
			if err := control.Del(ctx, keys...).Err(); err != nil {
				return r, err
			}
		}
		backend = store.NewRedisStore(o.address, "", 0)
	}
	defer backend.Close()
	for key, value := range facts {
		if err := backend.SetFactContext(ctx, key, value); err != nil {
			return r, err
		}
	}
	s := &countedStore{ContextStore: backend}
	engine, err := rex.NewEngineFromFile(path, s, 0)
	if err != nil {
		return r, err
	}
	defer engine.Shutdown()
	process := func(i int) error {
		for _, key := range inputs {
			if err := engine.ProcessFactUpdateContext(ctx, key, float64(i%100)+0.5); err != nil {
				return err
			}
		}
		return nil
	}
	for i := 0; i < o.warmup; i++ {
		if err := process(i); err != nil {
			return r, err
		}
	}
	s.Calls = counts{}
	var beforeCommands map[string]int64
	if control != nil {
		beforeCommands, err = commandCounts(ctx, control)
		if err != nil {
			return r, err
		}
	}
	samples := make([]int64, o.events)
	runtime.GC()
	var before, after, retained runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	for i := range samples {
		t := time.Now()
		if o.churn > 0 {
			// Churn keys are intentionally not in the rule dependencies.
			err = engine.ProcessFactUpdateContext(ctx, fmt.Sprintf("m0:churn:%08d", i%o.churn), float64(i))
		} else {
			err = process(i)
		}
		if err != nil {
			return r, err
		}
		samples[i] = time.Since(t).Nanoseconds()
	}
	r.ElapsedNS = time.Since(start).Nanoseconds()
	runtime.ReadMemStats(&after)
	r.Calls = s.Calls
	eligible := f.Affected
	if f.MissingEvery > 0 {
		eligible -= (f.Affected + f.MissingEvery - 1) / f.MissingEvery
	}
	expected := o.events * f.Batch * eligible * f.Actions * f.MatchPercent / 100
	if o.churn > 0 {
		expected = 0
	}
	if r.Calls.SetPublish != expected {
		return r, fmt.Errorf("invalid measurement: expected %d actions, observed %d", expected, r.Calls.SetPublish)
	}
	if control != nil {
		r.RedisCalls, err = commandCounts(ctx, control)
		if err != nil {
			return r, err
		}
		for key, value := range beforeCommands {
			r.RedisCalls[key] -= value
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&retained)
	r.HeapBefore, r.HeapAfter = before.HeapAlloc, retained.HeapAlloc
	r.RetainedDelta = int64(retained.HeapAlloc) - int64(before.HeapAlloc)
	r.FactsRetained = len(engine.Facts)
	runtime.KeepAlive(engine)
	r.AllocsPerEvent = float64(after.Mallocs-before.Mallocs) / float64(o.events)
	r.BytesPerEvent = float64(after.TotalAlloc-before.TotalAlloc) / float64(o.events)
	r.EventsPerSecond = float64(o.events) * 1e9 / float64(r.ElapsedNS)
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	r.P50NS, r.P95NS, r.P99NS, r.MaxNS = samples[(o.events-1)*50/100], samples[(o.events-1)*95/100], samples[(o.events-1)*99/100], samples[o.events-1]
	return r, nil
}

func run() error {
	var o options
	flag.StringVar(&o.mode, "mode", "memory", "memory or redis (synchronous engine API, no ingress)")
	flag.StringVar(&o.address, "redis", "127.0.0.1:16379", "dedicated disposable Redis address; m0:* keys are reset")
	flag.StringVar(&o.level, "log-level", "disabled", "disabled or info; both write to io.Discard")
	flag.StringVar(&o.source, "source-id", "", "required revision plus source fingerprint")
	flag.IntVar(&o.events, "events", 1000, "measured batches per run (multiple of 100)")
	flag.IntVar(&o.warmup, "warmup", 100, "warmup batches per run")
	flag.IntVar(&o.runs, "runs", 5, "independent engine instances per fixture")
	flag.IntVar(&o.churn, "churn", 0, "replace measured batches with unrelated keys cycling over this cardinality")
	fixtureFile := flag.String("fixtures", "scripts/baseline/fixtures.json", "fixture matrix")
	selected := flag.String("fixture", "", "one fixture name; empty runs all")
	flag.Parse()
	if (o.mode != "memory" && o.mode != "redis") || (o.level != "disabled" && o.level != "info") || o.events < 100 || o.events%100 != 0 || o.warmup < 0 || o.runs < 1 || o.churn < 0 || o.source == "" {
		return fmt.Errorf("invalid options; source-id is required, events must be a positive multiple of 100")
	}
	level, _ := zerolog.ParseLevel(o.level)
	zerolog.SetGlobalLevel(level)
	logging.Logger = zerolog.New(io.Discard).With().Timestamp().Logger()
	log.SetOutput(io.Discard)
	data, err := os.ReadFile(*fixtureFile)
	if err != nil {
		return err
	}
	var fixtures []fixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		return err
	}
	seen := map[string]bool{}
	matched := false
	for _, f := range fixtures {
		if err := f.validate(); err != nil {
			return err
		}
		if seen[f.Name] {
			return fmt.Errorf("duplicate fixture %q", f.Name)
		}
		seen[f.Name] = true
		if *selected != "" && f.Name != *selected {
			continue
		}
		matched = true
		for i := 1; i <= o.runs; i++ {
			r, err := measure(context.Background(), f, o, i)
			if err != nil {
				return fmt.Errorf("%s run %d: %w", f.Name, i, err)
			}
			if err := json.NewEncoder(os.Stdout).Encode(r); err != nil {
				return err
			}
		}
	}
	if !matched {
		return fmt.Errorf("no matching fixtures")
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
