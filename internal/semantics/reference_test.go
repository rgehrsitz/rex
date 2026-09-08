// Package semantics defines the current v3 contract independently of bytecode.
package semantics

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"
)

type reference struct {
	rules []compiler.Rule
	facts map[string]interface{}
	store store.ContextStore
	limit int
}

// No compiler traversal, indices, opcodes, or runtime comparisons are used here.
func dependencies(nodes []*compiler.ConditionOrGroup, out map[string]bool) {
	for _, n := range nodes {
		if n.Fact != "" {
			out[n.Fact] = true
		}
		dependencies(n.All, out)
		dependencies(n.Any, out)
	}
}
func matches(nodes []*compiler.ConditionOrGroup, all bool, facts map[string]interface{}) bool {
	for _, n := range nodes {
		var yes bool
		switch {
		case len(n.All) > 0:
			yes = matches(n.All, true, facts)
		case len(n.Any) > 0:
			yes = matches(n.Any, false, facts)
		default:
			yes = leaf(facts[n.Fact], n.Value, n.Operator)
		}
		if all && !yes {
			return false
		}
		if !all && yes {
			return true
		}
	}
	return all
}
func leaf(actual, expected interface{}, op string) bool {
	switch want := expected.(type) {
	case float64:
		got, ok := actual.(float64)
		if !ok {
			return false
		}
		switch op {
		case "EQ":
			return got == want
		case "NEQ":
			return got != want
		case "LT":
			return got < want
		case "LTE":
			return got <= want
		case "GT":
			return got > want
		case "GTE":
			return got >= want
		}
	case string:
		got, ok := actual.(string)
		if !ok {
			return false
		}
		switch op {
		case "EQ":
			return got == want
		case "NEQ":
			return got != want
		case "CONTAINS":
			return strings.Contains(got, want)
		case "NOT_CONTAINS":
			return !strings.Contains(got, want)
		}
	case bool:
		got, ok := actual.(bool)
		if !ok {
			return false
		}
		if op == "EQ" {
			return got == want
		}
		if op == "NEQ" {
			return got != want
		}
	}
	return false
}
func (r *reference) ProcessFactUpdateContext(ctx context.Context, key string, value interface{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch n := value.(type) {
	case int:
		value = float64(n)
	case float32:
		value = float64(n)
	}
	r.facts[key] = value
	candidates := []compiler.Rule{}
	deps := map[string]map[string]bool{}
	query := map[string]bool{}
	for _, rule := range r.rules {
		d := map[string]bool{}
		dependencies(rule.Conditions.All, d)
		dependencies(rule.Conditions.Any, d)
		if !d[key] {
			continue
		}
		candidates = append(candidates, rule)
		deps[rule.Name] = d
		for fact := range d {
			if fact != key {
				query[fact] = true
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Priority < candidates[j].Priority })
	keys := []string{}
	for fact := range query {
		keys = append(keys, fact)
	}
	sort.Strings(keys)
	missing := map[string]bool{}
	if len(keys) > 0 {
		values, err := r.store.MGetFactsContext(ctx, keys...)
		if err != nil {
			return err
		}
		for fact, v := range values {
			if v == nil {
				delete(r.facts, fact)
				missing[fact] = true
			} else {
				r.facts[fact] = v
			}
		}
	}
	for _, rule := range candidates {
		skip := false
		for fact := range deps[rule.Name] {
			if missing[fact] {
				skip = true
			}
		}
		if skip {
			continue
		}
		yes := matches(rule.Conditions.All, true, r.facts)
		if len(rule.Conditions.Any) > 0 {
			yes = matches(rule.Conditions.Any, false, r.facts)
		}
		if !yes {
			continue
		}
		for i, a := range rule.Actions {
			if i >= r.limit {
				return fmt.Errorf("rule %q exceeded action limit of %d", rule.Name, r.limit)
			}
			r.facts[a.Target] = a.Value
			if err := r.store.SetAndPublishFactContext(ctx, a.Target, a.Value); err != nil {
				return err
			}
		}
	}
	return nil
}
