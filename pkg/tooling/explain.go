package tooling

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"
)

type RuleInfo struct {
	Name         string                  `json:"name"`
	SourceIndex  int                     `json:"source_index"`
	Priority     int                     `json:"priority"`
	Dependencies []string                `json:"dependencies"`
	Conditions   compiler.ConditionGroup `json:"conditions"`
	Actions      []compiler.Action       `json:"actions"`
}
type Explanation struct {
	SchemaVersion     int        `json:"schema_version"`
	ArtifactSHA256    string     `json:"artifact_sha256"`
	ExecutionContract uint32     `json:"execution_contract"`
	Representation    string     `json:"representation"`
	Rules             []RuleInfo `json:"rules"`
}

func Dependencies(r compiler.Rule) []string {
	set := map[string]bool{}
	var walk func([]*compiler.ConditionOrGroup)
	walk = func(nodes []*compiler.ConditionOrGroup) {
		for _, n := range nodes {
			if n.Fact != "" {
				set[n.Fact] = true
			}
			walk(n.All)
			walk(n.Any)
		}
	}
	walk(r.Conditions.All)
	walk(r.Conditions.Any)
	keys := []string{}
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func Explain(artifact []byte) (Explanation, error) {
	rules, err := compiler.DecodeBatch(artifact)
	if err != nil {
		return Explanation{}, fmt.Errorf("explain requires a valid v4 artifact: %w", err)
	}
	out := Explanation{SchemaVersion: SchemaVersion, ArtifactSHA256: Digest(artifact), ExecutionContract: compiler.BatchVersion, Representation: "structured condition IR; v4 has no jump instructions", Rules: []RuleInfo{}}
	for i, r := range rules.Rules {
		out.Rules = append(out.Rules, RuleInfo{r.Name, i, r.Priority, Dependencies(r), r.Conditions, r.Actions})
	}
	sort.SliceStable(out.Rules, func(i, j int) bool { return out.Rules[i].Priority < out.Rules[j].Priority })
	return out, nil
}

type Diagnostic struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Rule     string `json:"rule,omitempty"`
	Message  string `json:"message"`
}
type LintReport struct {
	SchemaVersion int          `json:"schema_version"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
}

func Lint(source []byte, channels []string) LintReport {
	out := LintReport{SchemaVersion: SchemaVersion, Diagnostics: []Diagnostic{}}
	add := func(id, severity, rule, message string) {
		out.Diagnostics = append(out.Diagnostics, Diagnostic{id, severity, rule, message})
	}
	// Legacy parser recognizes script definitions, enabling a useful diagnostic
	// before the v4 capability validator rejects them.
	parsed, err := compiler.Parse(source)
	if err != nil {
		add("REX-L001", "error", "", err.Error())
		return out
	}
	undefinedScript := false
	for _, r := range parsed.Rules {
		for _, a := range r.Actions {
			if v, ok := a.Value.(string); ok && strings.HasPrefix(v, "{") && strings.HasSuffix(v, "}") {
				name := strings.TrimSuffix(strings.TrimPrefix(v, "{"), "}")
				if _, ok := r.Scripts[name]; !ok {
					undefinedScript = true
					add("REX-L004", "error", r.Name, "undefined script reference: "+name)
				}
			}
		}
	}
	if _, err := compiler.ParseBatch(source); err != nil {
		if undefinedScript && strings.Contains(err.Error(), "script calls unavailable") {
			return out
		}
		add("REX-L001", "error", "", err.Error())
		return out
	}
	type writer struct {
		rule  string
		value interface{}
	}
	writers := map[string]writer{}
	for _, r := range parsed.Rules {
		for _, a := range r.Actions {
			if old, ok := writers[a.Target]; ok && old.value != a.Value {
				add("REX-L002", "warning", r.Name, fmt.Sprintf("target %q also has a different writer in %q; simultaneous matches reject the round", a.Target, old.rule))
			} else if !ok {
				writers[a.Target] = writer{r.Name, a.Value}
			}
			// This simple cycle is provable: a sole EQ condition and a matching
			// self-write always recreate the triggering condition in the next round.
			nodes := r.Conditions.All
			if len(r.Conditions.Any) > 0 {
				nodes = r.Conditions.Any
			}
			if len(nodes) == 1 && nodes[0].Fact == a.Target && nodes[0].Operator == "EQ" && nodes[0].Value == a.Value {
				add("REX-L003", "warning", r.Name, "self-write reproduces the sole EQ condition; if reached without conflicts, it repeats until a chain budget stops it")
			}
		}
	}
	for _, channel := range channels {
		if channel == store.ResultsChannel {
			add("REX-L005", "warning", "", "rex_results carries committed notifications, not new inputs; v4 ignores them")
		}
		if channel == "" {
			add("REX-L005", "error", "", "empty input channel")
		}
	}
	return out
}
func HasLintErrors(r LintReport) bool {
	for _, d := range r.Diagnostics {
		if d.Severity == "error" {
			return true
		}
	}
	return false
}

// JSON writes deterministic map ordering, explicit schema versions and no
// timestamps. Source bytes remain a string inside replay bundles.
func JSON(v interface{}) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(b) > MaxDocumentBytes {
		return nil, fmt.Errorf("output exceeds document byte limit")
	}
	return append(b, '\n'), nil
}
