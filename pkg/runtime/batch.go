package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"
)

type Truth string

const (
	True          Truth = "true"
	False         Truth = "false"
	Indeterminate Truth = "unknown"
)

// Program contains a private, immutable copy of a validated v4 artifact.
type Program struct {
	rules        []compiler.Rule
	dependents   map[string][]int
	dependencies [][]string
}

func LoadProgram(data []byte) (*Program, error) {
	rules, err := compiler.DecodeBatch(data)
	if err != nil {
		return nil, err
	}
	p := &Program{rules: rules.Rules, dependents: map[string][]int{}, dependencies: make([][]string, len(rules.Rules))}
	nodes := 0
	unique := map[string]bool{}
	var visit func([]*compiler.ConditionOrGroup, map[string]bool) error
	visit = func(ns []*compiler.ConditionOrGroup, facts map[string]bool) error {
		for _, n := range ns {
			nodes++
			if nodes > compiler.MaxConditionNodes {
				return fmt.Errorf("program exceeds 100000 condition nodes")
			}
			if n.Fact != "" {
				facts[n.Fact] = true
				unique[n.Fact] = true
			}
			if err := visit(n.All, facts); err != nil {
				return err
			}
			if err := visit(n.Any, facts); err != nil {
				return err
			}
		}
		return nil
	}
	for i, r := range p.rules {
		facts := map[string]bool{}
		if err := visit(r.Conditions.All, facts); err != nil {
			return nil, err
		}
		if err := visit(r.Conditions.Any, facts); err != nil {
			return nil, err
		}
		for fact := range facts {
			p.dependencies[i] = append(p.dependencies[i], fact)
			p.dependents[fact] = append(p.dependents[fact], i)
		}
		sort.Strings(p.dependencies[i])
	}
	if len(unique) > compiler.MaxDependencies {
		return nil, fmt.Errorf("program exceeds 65536 dependencies")
	}
	return p, nil
}

type Limits struct {
	EventBytes      int `json:"event_bytes"`
	EventFacts      int `json:"event_facts"`
	SnapshotBytes   int `json:"snapshot_bytes"`
	ActionsPerRule  int `json:"actions_per_rule"`
	ActionsPerRound int `json:"actions_per_round"`
	ChainActions    int `json:"chain_actions"`
	ChainWork       int `json:"chain_work"`
	Rounds          int `json:"rounds"`
	StagedBytes     int `json:"staged_bytes"`
}

func DefaultLimits() Limits {
	return Limits{EventBytes: 1 << 20, EventFacts: 256, SnapshotBytes: 4 << 20, ActionsPerRule: 32, ActionsPerRound: 256, ChainActions: 1024, ChainWork: 100000, Rounds: 16, StagedBytes: 1 << 20}
}
func (l Limits) Validate() error {
	max := Limits{EventBytes: 1 << 20, EventFacts: 4096, SnapshotBytes: 4 << 20, ActionsPerRule: 1024, ActionsPerRound: 4096, ChainActions: 8192, ChainWork: 1000000, Rounds: 64, StagedBytes: 4 << 20}
	a, b := []int{l.EventBytes, l.EventFacts, l.SnapshotBytes, l.ActionsPerRule, l.ActionsPerRound, l.ChainActions, l.ChainWork, l.Rounds, l.StagedBytes}, []int{max.EventBytes, max.EventFacts, max.SnapshotBytes, max.ActionsPerRule, max.ActionsPerRound, max.ChainActions, max.ChainWork, max.Rounds, max.StagedBytes}
	names := []string{"event_bytes", "event_facts", "snapshot_bytes", "actions_per_rule", "actions_per_round", "chain_actions", "chain_work", "rounds", "staged_bytes"}
	for i, v := range a {
		if v <= 0 || v > b[i] {
			return fmt.Errorf("batch limit %s must be between 1 and %d", names[i], b[i])
		}
	}
	return nil
}

type Budget struct{ Actions, Work int }
type ActionProposal struct {
	Rule   string      `json:"rule"`
	Target string      `json:"target"`
	Value  interface{} `json:"value"`
}
type ConditionResult struct {
	Rule   string          `json:"rule"`
	Fact   string          `json:"fact"`
	State  store.FactState `json:"state"`
	Result Truth           `json:"result"`
}
type RuleResult struct {
	Rule   string `json:"rule"`
	Result Truth  `json:"result"`
}
type Evaluation struct {
	RuleResults []RuleResult      `json:"rule_results,omitempty"`
	Rules       []string          `json:"rules"`
	Actions     []ActionProposal  `json:"actions"`
	Writes      []store.Write     `json:"writes"`
	Conditions  []ConditionResult `json:"conditions,omitempty"`
}

func fieldBytes(key string, value interface{}, limit int) (int, error) {
	if len(key) > 255 || key == "" || !store.Scalar(value) {
		return 0, fmt.Errorf("invalid scalar fact %q", key)
	}
	if s, ok := value.(string); ok && len(s) > limit {
		return 0, fmt.Errorf("fact %q exceeds byte limit", key)
	}
	b, err := json.Marshal(map[string]interface{}{key: value})
	if err != nil {
		return 0, err
	}
	return len(b), nil
}
func validateEvent(event map[string]interface{}, l Limits) error {
	if len(event) == 0 || len(event) > l.EventFacts {
		return fmt.Errorf("event fact count must be 1..%d", l.EventFacts)
	}
	size := 0
	for k, v := range event {
		n, err := fieldBytes(k, v, l.EventBytes)
		if err != nil {
			return err
		}
		size += n
		if size > l.EventBytes {
			return fmt.Errorf("event exceeds byte limit")
		}
	}
	return nil
}
func (p *Program) candidates(event map[string]interface{}) ([]int, []string) {
	selected := map[int]bool{}
	for key := range event {
		for _, i := range p.dependents[key] {
			selected[i] = true
		}
	}
	ids := make([]int, 0, len(selected))
	needed := map[string]bool{}
	for i := range selected {
		ids = append(ids, i)
		for _, key := range p.dependencies[i] {
			if _, ok := event[key]; !ok {
				needed[key] = true
			}
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := p.rules[ids[i]], p.rules[ids[j]]
		if a.Priority == b.Priority {
			return ids[i] < ids[j]
		}
		return a.Priority < b.Priority
	})
	keys := make([]string, 0, len(needed))
	for key := range needed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return ids, keys
}
func compareV4(f store.Fact, constant interface{}, op string) Truth {
	if f.State != store.Present {
		return Indeterminate
	}
	yes := false
	switch want := constant.(type) {
	case float64:
		got, ok := f.Value.(float64)
		if !ok {
			return Indeterminate
		}
		switch op {
		case "EQ":
			yes = got == want
		case "NEQ":
			yes = got != want
		case "LT":
			yes = got < want
		case "LTE":
			yes = got <= want
		case "GT":
			yes = got > want
		case "GTE":
			yes = got >= want
		}
	case string:
		got, ok := f.Value.(string)
		if !ok {
			return Indeterminate
		}
		switch op {
		case "EQ":
			yes = got == want
		case "NEQ":
			yes = got != want
		case "CONTAINS":
			yes = strings.Contains(got, want)
		case "NOT_CONTAINS":
			yes = !strings.Contains(got, want)
		}
	case bool:
		got, ok := f.Value.(bool)
		if !ok {
			return Indeterminate
		}
		if op == "EQ" {
			yes = got == want
		} else {
			yes = got != want
		}
	default:
		return Indeterminate
	}
	if yes {
		return True
	}
	return False
}

// Evaluate is pure with respect to state and transport. It never calls an
// adapter. Inputs must not be concurrently mutated by the caller. Budget is
// copied on entry; a failed evaluation does not consume the caller's budget.
func (p *Program) Evaluate(ctx context.Context, snapshot map[string]store.Fact, event map[string]interface{}, limits Limits, budget Budget, trace bool) (retEval Evaluation, retBudget Budget, retErr error) {
	empty := Evaluation{}
	result := Evaluation{}
	defer func() {
		if retErr != nil {
			retEval = Evaluation{Conditions: result.Conditions, RuleResults: result.RuleResults}
		}
	}()
	if err := limits.Validate(); err != nil {
		return empty, budget, err
	}
	if err := validateEvent(event, limits); err != nil {
		return empty, budget, err
	}
	if err := ctx.Err(); err != nil {
		return empty, budget, err
	}
	if budget.Actions < 0 || budget.Work < 0 || budget.Actions > limits.ChainActions || budget.Work > limits.ChainWork {
		return empty, budget, fmt.Errorf("invalid chain budget")
	}
	remaining := budget
	ids, keys := p.candidates(event)
	values := make(map[string]store.Fact, len(keys)+len(event))
	size := 0
	for _, k := range keys {
		f, ok := snapshot[k]
		if !ok {
			f = store.Fact{State: store.Missing}
		}
		switch f.State {
		case store.Present:
			if f.Value == nil || !store.Scalar(f.Value) {
				f = store.Fact{State: store.Invalid}
			}
		case store.Null, store.Missing, store.Invalid:
			f.Value = nil
		default:
			return empty, budget, fmt.Errorf("invalid snapshot state for %q", k)
		}
		n, err := fieldBytes(k, f.Value, limits.SnapshotBytes)
		if err != nil {
			return empty, budget, err
		}
		size += n
		if size > limits.SnapshotBytes {
			return empty, budget, fmt.Errorf("snapshot exceeds byte limit")
		}
		values[k] = f
	}
	for k, v := range event {
		state := store.Present
		if v == nil {
			state = store.Null
		}
		values[k] = store.Fact{State: state, Value: v}
	}
	result = Evaluation{Rules: []string{}, Actions: []ActionProposal{}, Writes: []store.Write{}}
	var group func(string, []*compiler.ConditionOrGroup, bool) (Truth, error)
	group = func(rule string, nodes []*compiler.ConditionOrGroup, all bool) (Truth, error) {
		unknown := false
		for _, node := range nodes {
			if err := ctx.Err(); err != nil {
				return Indeterminate, err
			}
			remaining.Work++
			if remaining.Work > limits.ChainWork {
				return Indeterminate, fmt.Errorf("chain work limit exceeded")
			}
			var value Truth
			var err error
			switch {
			case len(node.All) > 0:
				value, err = group(rule, node.All, true)
			case len(node.Any) > 0:
				value, err = group(rule, node.Any, false)
			default:
				fact := values[node.Fact]
				if fact.State == "" {
					fact.State = store.Missing
				}
				value = compareV4(fact, node.Value, node.Operator)
				if trace {
					result.Conditions = append(result.Conditions, ConditionResult{Rule: rule, Fact: node.Fact, State: fact.State, Result: value})
				}
			}
			if err != nil {
				return Indeterminate, err
			}
			if all && value == False {
				return False, nil
			}
			if !all && value == True {
				return True, nil
			}
			unknown = unknown || value == Indeterminate
		}
		if unknown {
			return Indeterminate, nil
		}
		if all {
			return True, nil
		}
		return False, nil
	}
	targets := map[string]interface{}{}
	staged := 0
	for _, i := range ids {
		remaining.Work++
		if remaining.Work > limits.ChainWork {
			return empty, budget, fmt.Errorf("chain work limit exceeded")
		}
		rule := p.rules[i]
		nodes, all := rule.Conditions.All, true
		if len(rule.Conditions.Any) > 0 {
			nodes, all = rule.Conditions.Any, false
		}
		matched, err := group(rule.Name, nodes, all)
		if err != nil {
			return empty, budget, err
		}
		if trace {
			result.RuleResults = append(result.RuleResults, RuleResult{Rule: rule.Name, Result: matched})
		}
		if matched != True {
			continue
		}
		result.Rules = append(result.Rules, rule.Name)
		if len(rule.Actions) > limits.ActionsPerRule {
			return empty, budget, fmt.Errorf("rule %q exceeded action limit", rule.Name)
		}
		for _, a := range rule.Actions {
			remaining.Actions++
			if remaining.Actions > limits.ChainActions || len(result.Actions) >= limits.ActionsPerRound {
				return empty, budget, fmt.Errorf("action budget exceeded")
			}
			n, err := fieldBytes(a.Target, a.Value, limits.StagedBytes)
			if err != nil {
				return empty, budget, err
			}
			staged += n
			if staged > limits.StagedBytes {
				return empty, budget, fmt.Errorf("staged byte budget exceeded")
			}
			result.Actions = append(result.Actions, ActionProposal{Rule: rule.Name, Target: a.Target, Value: a.Value})
			if old, ok := targets[a.Target]; ok {
				if old != a.Value {
					return empty, budget, fmt.Errorf("conflicting writes to %q", a.Target)
				}
				continue
			}
			targets[a.Target] = a.Value
			result.Writes = append(result.Writes, store.Write{Key: a.Target, Value: a.Value})
		}
	}
	if err := ctx.Err(); err != nil {
		return empty, budget, err
	}
	return result, remaining, nil
}

type RoundResult struct {
	EvaluationError string             `json:"evaluation_error,omitempty"`
	Round           int                `json:"round"`
	Evaluation      Evaluation         `json:"evaluation"`
	Commit          store.CommitResult `json:"commit"`
	CommitError     string             `json:"commit_error,omitempty"`
}
type ChainResult struct {
	ChainID string        `json:"chain_id"`
	Rounds  []RoundResult `json:"rounds"`
	Budget  Budget        `json:"budget"`
}

var ErrReconciliationRequired = errors.New("coordinator halted: reconcile partial or unknown commit before restarting")

type Coordinator struct {
	mu        sync.Mutex
	program   *Program
	reader    store.SnapshotReader
	committer store.Committer
	limits    Limits
	trace     bool
	halted    bool
}

func NewCoordinator(program *Program, reader store.SnapshotReader, committer store.Committer, limits Limits) (*Coordinator, error) {
	if program == nil || reader == nil || committer == nil {
		return nil, fmt.Errorf("program, snapshot reader, and committer required")
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	return &Coordinator{program: program, reader: reader, committer: committer, limits: limits, trace: true}, nil
}
func (c *Coordinator) Process(ctx context.Context, chainID string, event map[string]interface{}) (ChainResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := ChainResult{ChainID: chainID, Rounds: []RoundResult{}}
	if c.halted {
		return result, ErrReconciliationRequired
	}
	if len(chainID) > 255 {
		return result, fmt.Errorf("chain id exceeds byte limit")
	}
	if chainID == "" {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return result, err
		}
		result.ChainID = hex.EncodeToString(id[:])
	}
	if err := validateEvent(event, c.limits); err != nil {
		return result, err
	}
	for round := 0; len(event) > 0; round++ {
		if round >= c.limits.Rounds {
			return result, fmt.Errorf("chain round limit exceeded; prior rounds remain committed")
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := validateEvent(event, c.limits); err != nil {
			return result, err
		}
		_, keys := c.program.candidates(event)
		snapshot := map[string]store.Fact{}
		if len(keys) > 0 {
			var err error
			snapshot, err = c.reader.ReadSnapshot(ctx, keys)
			if err != nil {
				return result, fmt.Errorf("snapshot: %w", err)
			}
		}
		eval, budget, err := c.program.Evaluate(ctx, snapshot, event, c.limits, result.Budget, c.trace)
		if err != nil {
			result.Rounds = append(result.Rounds, RoundResult{Round: round, Evaluation: eval, EvaluationError: err.Error(), Commit: store.CommitResult{Outcome: store.NotCommitted}})
			return result, fmt.Errorf("evaluate round %d: %w", round, err)
		}
		result.Budget = budget
		rr := RoundResult{Round: round, Evaluation: eval, Commit: store.CommitResult{Outcome: store.NotCommitted}}
		if len(eval.Writes) == 0 {
			result.Rounds = append(result.Rounds, rr)
			return result, nil
		}
		// Next-round fact and payload bounds are checked before these writes escape.
		next := map[string]interface{}{}
		for _, w := range eval.Writes {
			next[w.Key] = w.Value
		}
		if err := validateEvent(next, c.limits); err != nil {
			return result, fmt.Errorf("derived event: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		committed, err := c.committer.Commit(ctx, store.CommitRequest{ChainID: result.ChainID, Round: round, Writes: eval.Writes})
		rr.Commit = committed
		if err != nil {
			rr.CommitError = err.Error()
		}
		result.Rounds = append(result.Rounds, rr)
		switch committed.Outcome {
		case store.Committed:
			if err != nil {
				c.halted = true
				return result, fmt.Errorf("inconsistent committed outcome (%v): %w", err, ErrReconciliationRequired)
			}
		case store.NotCommitted:
			if err == nil {
				err = fmt.Errorf("adapter declined commit")
			}
			return result, fmt.Errorf("commit: %w", err)
		case store.Partial, store.Unknown:
			c.halted = true
			return result, fmt.Errorf("commit %s (%v): %w", committed.Outcome, err, ErrReconciliationRequired)
		default:
			c.halted = true
			return result, fmt.Errorf("invalid commit outcome: %w", ErrReconciliationRequired)
		}
		event = next
	}
	return result, nil
}

// ValidateEvent checks a proposed input against v4 scalar and payload limits.
func ValidateEvent(event map[string]interface{}, limits Limits) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	return validateEvent(event, limits)
}
