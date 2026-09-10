package runtime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"
)

type Truth string

const (
	True          Truth = "true"
	False         Truth = "false"
	Indeterminate Truth = "unknown"
)

// Program contains a private, immutable copy of a validated batch artifact.
type Program struct {
	ownershipFacts        []string
	rules                 []compiler.Rule
	dependents            map[string][]int
	dependencies          [][]string
	declarations          map[string]compiler.FactDeclaration
	version               uint32
	temporal              map[*compiler.ConditionOrGroup]temporalCondition
	hasTemporal           map[*compiler.ConditionOrGroup]bool
	hasTemporalConditions bool
	changeTargets         []map[string]bool
	changeTargetSet       map[string]bool
}

type temporalCondition struct {
	duration time.Duration
	key      string
}

func LoadProgram(data []byte) (*Program, error) {
	version, err := compiler.BatchArtifactVersion(data)
	if err != nil {
		return nil, err
	}
	rules, err := compiler.DecodeBatch(data)
	if err != nil {
		return nil, err
	}
	p := &Program{rules: rules.Rules, declarations: rules.Facts, version: version, dependents: map[string][]int{}, dependencies: make([][]string, len(rules.Rules)), changeTargets: make([]map[string]bool, len(rules.Rules))}
	if compiler.HasTemporalConditions(rules) {
		p.temporal = map[*compiler.ConditionOrGroup]temporalCondition{}
		p.hasTemporal = map[*compiler.ConditionOrGroup]bool{}
		p.hasTemporalConditions = true
	}
	programDigest := sha256.Sum256(data)
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
		// Only condition facts select rules; output targets are snapshot-only dependencies.
		for fact := range facts {
			p.dependents[fact] = append(p.dependents[fact], i)
		}
		if r.Emit == compiler.EmitOnChange {
			if p.changeTargetSet == nil {
				p.changeTargetSet = map[string]bool{}
			}
			p.changeTargets[i] = map[string]bool{}
			for _, action := range r.Actions {
				facts[action.Target] = true
				p.changeTargets[i][action.Target] = true
				p.changeTargetSet[action.Target] = true
				unique[action.Target] = true
			}
		}
		for fact := range facts {
			p.dependencies[i] = append(p.dependencies[i], fact)
		}
		if !p.hasTemporalConditions {
			sort.Strings(p.dependencies[i])
			continue
		}
		var temporal func([]*compiler.ConditionOrGroup, string) (bool, error)
		temporal = func(ns []*compiler.ConditionOrGroup, path string) (bool, error) {
			containsTemporal := false
			for nodeIndex, node := range ns {
				nodePath := fmt.Sprintf("%s/%d", path, nodeIndex)
				nodeContainsTemporal := false
				if node.For != "" {
					duration, err := time.ParseDuration(node.For)
					if err != nil {
						return false, err
					}
					identity := sha256.Sum256([]byte(fmt.Sprintf("%x/%d/%s", programDigest, i, nodePath)))
					key := fmt.Sprintf("%s%x", store.InternalStatePrefix, identity)
					p.temporal[node] = temporalCondition{duration: duration, key: key}
					p.dependencies[i] = append(p.dependencies[i], key)
					nodeContainsTemporal = true
				}
				allContainsTemporal, err := temporal(node.All, nodePath+"/all")
				if err != nil {
					return false, err
				}
				anyContainsTemporal, err := temporal(node.Any, nodePath+"/any")
				if err != nil {
					return false, err
				}
				nodeContainsTemporal = nodeContainsTemporal || allContainsTemporal || anyContainsTemporal
				p.hasTemporal[node] = nodeContainsTemporal
				containsTemporal = containsTemporal || nodeContainsTemporal
			}
			return containsTemporal, nil
		}
		if _, err := temporal(r.Conditions.All, "all"); err != nil {
			return nil, err
		}
		if _, err := temporal(r.Conditions.Any, "any"); err != nil {
			return nil, err
		}
		sort.Strings(p.dependencies[i])
	}
	if len(unique) > compiler.MaxDependencies {
		return nil, fmt.Errorf("program exceeds 65536 dependencies")
	}
	p.ownershipFacts = p.collectOwnershipFacts()
	return p, nil
}

func (p *Program) Version() uint32 { return p.version }

// HasTemporal reports whether the program contains processing-time conditions.
func (p *Program) HasTemporal() bool { return p.hasTemporalConditions }

// HasChangeOnly reports whether any rule opts into persisted-value suppression.
func (p *Program) HasChangeOnly() bool {
	return len(p.changeTargetSet) > 0
}

// ChangeOnlyTargets returns the sorted public facts used for suppression.
func (p *Program) ChangeOnlyTargets() []string {
	targets := make([]string, 0, len(p.changeTargetSet))
	for target := range p.changeTargetSet {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return targets
}

func (p *Program) FactDeclarations() map[string]compiler.FactDeclaration {
	if p.declarations == nil {
		return nil
	}
	out := make(map[string]compiler.FactDeclaration, len(p.declarations))
	for name, declaration := range p.declarations {
		out[name] = declaration
	}
	return out
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

type Budget struct {
	Actions int `json:"actions"`
	Work    int `json:"work"`
}
type ActionProposal struct {
	Rule        string      `json:"rule"`
	ActionIndex int         `json:"action_index,omitempty"`
	Target      string      `json:"target"`
	Value       interface{} `json:"value"`
	Suppressed  bool        `json:"suppressed,omitempty"`
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

func identicalScalar(fact store.Fact, value interface{}) bool {
	if fact.State != store.Present {
		return false
	}
	switch want := value.(type) {
	case float64:
		got, ok := fact.Value.(float64)
		return ok && got == want
	case string:
		got, ok := fact.Value.(string)
		return ok && got == want
	case bool:
		got, ok := fact.Value.(bool)
		return ok && got == want
	default:
		return false
	}
}
func validateEvent(event map[string]interface{}, l Limits) error {
	if len(event) == 0 || len(event) > l.EventFacts {
		return fmt.Errorf("event fact count must be 1..%d", l.EventFacts)
	}
	size := 0
	for k, v := range event {
		if store.IsInternalKey(k) {
			return fmt.Errorf("fact %q uses reserved internal state prefix", k)
		}
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

func (p *Program) validateEvent(event map[string]interface{}, limits Limits) error {
	if err := validateEvent(event, limits); err != nil {
		return err
	}
	return p.validateFactTypes(event)
}

func (p *Program) validateFactTypes(facts map[string]interface{}) error {
	for name := range facts {
		if store.IsInternalKey(name) {
			return fmt.Errorf("fact %q uses reserved internal state prefix", name)
		}
	}
	if p.declarations == nil {
		return nil
	}
	for name, value := range facts {
		declaration, ok := p.declarations[name]
		if !ok {
			return fmt.Errorf("typed fact contract: fact %q is undeclared", name)
		}
		if !declaration.Accepts(value) {
			if value == nil {
				return fmt.Errorf("typed fact contract: fact %q is null; expected %s", name, declaration.Type)
			}
			return fmt.Errorf("typed fact contract: fact %q must be %s", name, declaration.Type)
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
			_, inEvent := event[key]
			if !inEvent || p.changeTargets[i][key] {
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
func (p *Program) Evaluate(ctx context.Context, snapshot map[string]store.Fact, event map[string]interface{}, limits Limits, budget Budget, trace bool) (Evaluation, Budget, error) {
	return p.EvaluateAt(ctx, snapshot, event, limits, budget, trace, time.Time{})
}

func (p *Program) EvaluateAt(ctx context.Context, snapshot map[string]store.Fact, event map[string]interface{}, limits Limits, budget Budget, trace bool, at time.Time) (retEval Evaluation, retBudget Budget, retErr error) {
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
	if err := p.validateEvent(event, limits); err != nil {
		return empty, budget, err
	}
	if err := ctx.Err(); err != nil {
		return empty, budget, err
	}
	if len(p.temporal) > 0 && at.IsZero() {
		return empty, budget, fmt.Errorf("temporal evaluation requires an injected processing time")
	}
	at = at.UTC()
	if budget.Actions < 0 || budget.Work < 0 || budget.Actions > limits.ChainActions || budget.Work > limits.ChainWork {
		return empty, budget, fmt.Errorf("invalid chain budget")
	}
	remaining := budget
	ids, keys := p.candidates(event)
	values := make(map[string]store.Fact, len(keys)+len(event))
	var persisted map[string]store.Fact
	if len(p.changeTargetSet) > 0 {
		persisted = make(map[string]store.Fact, min(len(keys), len(p.changeTargetSet)))
	}
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
		if declaration, ok := p.declarations[k]; ok {
			if (f.State == store.Present && !declaration.Accepts(f.Value)) || (f.State == store.Null && !declaration.Nullable) {
				f = store.Fact{State: store.Invalid}
			}
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
		if p.changeTargetSet[k] {
			persisted[k] = f
		}
	}
	for k, v := range event {
		state := store.Present
		if v == nil {
			state = store.Null
		}
		values[k] = store.Fact{State: state, Value: v}
	}
	result = Evaluation{Rules: []string{}, Actions: []ActionProposal{}, Writes: []store.Write{}}
	staged := 0
	stageInternal := func(key string, value interface{}, deleteState bool) error {
		n, err := fieldBytes(key, value, limits.StagedBytes)
		if err != nil {
			return err
		}
		staged += n
		if staged > limits.StagedBytes {
			return fmt.Errorf("staged byte budget exceeded")
		}
		if len(result.Writes) >= store.MaxCommitWrites {
			return fmt.Errorf("commit exceeds write limit")
		}
		result.Writes = append(result.Writes, store.Write{Key: key, Value: value, Internal: true, Delete: deleteState})
		return nil
	}
	var group func(string, []*compiler.ConditionOrGroup, bool) (Truth, error)
	group = func(rule string, nodes []*compiler.ConditionOrGroup, all bool) (Truth, error) {
		unknown := false
		decisive := false
		maintainTemporal := p.hasTemporalConditions
		if maintainTemporal {
			maintainTemporal = false
			for _, node := range nodes {
				maintainTemporal = maintainTemporal || p.hasTemporal[node]
			}
		}
		for _, node := range nodes {
			if decisive && !p.hasTemporal[node] {
				continue
			}
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
				if temporal, ok := p.temporal[node]; ok {
					tracker := values[temporal.key]
					if value == True {
						switch tracker.State {
						case "", store.Missing, store.Null:
							started := at.Format(time.RFC3339Nano)
							if err := stageInternal(temporal.key, started, false); err != nil {
								return Indeterminate, err
							}
							value = Indeterminate
						case store.Present:
							startedText, ok := tracker.Value.(string)
							if !ok {
								return Indeterminate, fmt.Errorf("invalid temporal state for %q", node.Fact)
							}
							started, parseErr := time.Parse(time.RFC3339Nano, startedText)
							if parseErr != nil {
								return Indeterminate, fmt.Errorf("invalid temporal state for %q", node.Fact)
							}
							if at.Before(started) {
								if err := stageInternal(temporal.key, at.Format(time.RFC3339Nano), false); err != nil {
									return Indeterminate, err
								}
								value = Indeterminate
								break
							}
							if at.Sub(started) < temporal.duration {
								value = Indeterminate
							}
						default:
							return Indeterminate, fmt.Errorf("invalid temporal state for %q", node.Fact)
						}
					} else if tracker.State == store.Present {
						if err := stageInternal(temporal.key, nil, true); err != nil {
							return Indeterminate, err
						}
					}
				}
				if trace {
					result.Conditions = append(result.Conditions, ConditionResult{Rule: rule, Fact: node.Fact, State: fact.State, Result: value})
				}
			}
			if err != nil {
				return Indeterminate, err
			}
			if all && value == False {
				decisive = true
				if !maintainTemporal {
					return False, nil
				}
			}
			if !all && value == True {
				decisive = true
				if !maintainTemporal {
					return True, nil
				}
			}
			unknown = unknown || value == Indeterminate
		}
		if decisive {
			if all {
				return False, nil
			}
			return True, nil
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
	targetChangeOnly := map[string]bool{}
	targetSeen := map[string]bool{}
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
		for actionIndex, a := range rule.Actions {
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
			result.Actions = append(result.Actions, ActionProposal{Rule: rule.Name, ActionIndex: actionIndex, Target: a.Target, Value: a.Value})
			if len(p.changeTargetSet) > 0 {
				changeOnly := rule.Emit == compiler.EmitOnChange
				if targetSeen[a.Target] {
					targetChangeOnly[a.Target] = targetChangeOnly[a.Target] && changeOnly
				} else {
					targetSeen[a.Target] = true
					targetChangeOnly[a.Target] = changeOnly
				}
			}
			if old, ok := targets[a.Target]; ok {
				// Validated artifact actions contain only JSON-normalized, comparable scalars.
				if old != a.Value {
					return empty, budget, fmt.Errorf("conflicting writes to %q", a.Target)
				}
				continue
			}
			targets[a.Target] = a.Value
			if len(result.Writes) >= store.MaxCommitWrites {
				return empty, budget, fmt.Errorf("commit exceeds write limit")
			}
			result.Writes = append(result.Writes, store.Write{Key: a.Target, Value: a.Value})
		}
	}
	if len(p.changeTargetSet) > 0 {
		writes := result.Writes[:0]
		for _, write := range result.Writes {
			if !write.Internal && targetChangeOnly[write.Key] && identicalScalar(persisted[write.Key], write.Value) {
				continue
			}
			writes = append(writes, write)
		}
		result.Writes = writes
		for i := range result.Actions {
			target := result.Actions[i].Target
			result.Actions[i].Suppressed = targetChangeOnly[target] && identicalScalar(persisted[target], targets[target])
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
	clock     Clock
}

type Clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func NewCoordinator(program *Program, reader store.SnapshotReader, committer store.Committer, limits Limits) (*Coordinator, error) {
	if program == nil || reader == nil || committer == nil {
		return nil, fmt.Errorf("program, snapshot reader, and committer required")
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	return &Coordinator{program: program, reader: reader, committer: committer, limits: limits, trace: true, clock: systemClock{}}, nil
}

func (c *Coordinator) SetClock(clock Clock) error {
	if clock == nil {
		return fmt.Errorf("clock is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock = clock
	return nil
}

func (c *Coordinator) processingTime() (time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	at := c.clock.Now().UTC()
	if at.IsZero() {
		return time.Time{}, fmt.Errorf("clock returned zero processing time")
	}
	return at, nil
}

func (c *Coordinator) Process(ctx context.Context, chainID string, event map[string]interface{}) (ChainResult, error) {
	return c.process(ctx, chainID, event, time.Time{})
}

// ProcessAt evaluates a chain at an already pinned processing time. Durable
// recovery uses it so retry timing cannot change temporal results.
func (c *Coordinator) ProcessAt(ctx context.Context, chainID string, event map[string]interface{}, at time.Time) (ChainResult, error) {
	if at.IsZero() {
		return ChainResult{ChainID: chainID, Rounds: []RoundResult{}}, fmt.Errorf("processing time is required")
	}
	return c.process(ctx, chainID, event, at.UTC())
}

func (c *Coordinator) process(ctx context.Context, chainID string, event map[string]interface{}, evaluationTime time.Time) (ChainResult, error) {
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
	if err := c.program.validateEvent(event, c.limits); err != nil {
		return result, err
	}
	if evaluationTime.IsZero() {
		evaluationTime = c.clock.Now().UTC()
		if evaluationTime.IsZero() {
			return result, fmt.Errorf("clock returned zero processing time")
		}
	}
	for round := 0; len(event) > 0; round++ {
		if round >= c.limits.Rounds {
			return result, fmt.Errorf("chain round limit exceeded; prior rounds remain committed")
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := c.program.validateEvent(event, c.limits); err != nil {
			return result, err
		}
		roundCtx := store.WithEvaluationRound(ctx, round)
		_, keys := c.program.candidates(event)
		snapshot := map[string]store.Fact{}
		if len(keys) > 0 {
			var err error
			snapshot, err = c.reader.ReadSnapshot(roundCtx, keys)
			if err != nil {
				return result, fmt.Errorf("snapshot: %w", err)
			}
		}
		eval, budget, err := c.program.EvaluateAt(roundCtx, snapshot, event, c.limits, result.Budget, c.trace, evaluationTime)
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
			if !w.Internal {
				next[w.Key] = w.Value
			}
		}
		if len(next) > 0 {
			if err := c.program.validateEvent(next, c.limits); err != nil {
				return result, fmt.Errorf("derived event: %w", err)
			}
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		actions := make([]store.ActionIdentity, 0, len(eval.Actions))
		for _, action := range eval.Actions {
			if !action.Suppressed {
				actions = append(actions, store.ActionIdentity{Rule: action.Rule, Index: action.ActionIndex, Target: action.Target})
			}
		}
		committed, err := c.committer.Commit(roundCtx, store.CommitRequest{ChainID: result.ChainID, Round: round, Writes: eval.Writes, Actions: actions})
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
		case store.Unknown:
			if resolver, ok := c.committer.(store.UnknownOutcomeResolver); ok && resolver.ResolvesUnknownOnRetry() {
				return result, fmt.Errorf("commit outcome unknown; retry the same durable event: %w", err)
			}
			c.halted = true
			return result, fmt.Errorf("commit %s (%v): %w", committed.Outcome, err, ErrReconciliationRequired)
		case store.Partial:
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

// ValidateEvent checks a proposed input against batch scalar and payload limits.
func ValidateEvent(event map[string]interface{}, limits Limits) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	return validateEvent(event, limits)
}

// ValidateProgramEvent applies both generic v4/v5 bounds and any v5 typed fact
// declarations. Missing declarations are a closed-world input error.
func ValidateProgramEvent(program *Program, event map[string]interface{}, limits Limits) error {
	if program == nil {
		return fmt.Errorf("program is required")
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	return program.validateEvent(event, limits)
}

// ValidateProgramFacts applies a v5 declaration contract to an arbitrary fact
// set such as replay initial state. Empty sets are valid because missing is a
// first-class fact state.
func ValidateProgramFacts(program *Program, facts map[string]interface{}) error {
	if program == nil {
		return fmt.Errorf("program is required")
	}
	return program.validateFactTypes(facts)
}
