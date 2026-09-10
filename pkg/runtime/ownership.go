package runtime

import (
	"fmt"
	"rgehrsitz/rex/pkg/store"
	"sort"
)

// OwnershipFacts includes every public read/write and every declaration, so
// retained artifacts cannot silently broaden the producer input namespace.
func (p *Program) OwnershipFacts() []string { return append([]string(nil), p.ownershipFacts...) }
func (p *Program) collectOwnershipFacts() []string {
	set := map[string]bool{}
	for _, deps := range p.dependencies {
		for _, key := range deps {
			if !store.IsInternalKey(key) {
				set[key] = true
			}
		}
	}
	for _, rule := range p.rules {
		for _, action := range rule.Actions {
			set[action.Target] = true
		}
	}
	for key := range p.declarations {
		set[key] = true
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func (e *Engine) ValidateFactOwnership(names []string) error {
	policy, err := store.NewFactOwnership(names)
	if err != nil {
		return err
	}
	if policy == nil {
		return nil
	}
	if e.coordinator == nil {
		return fmt.Errorf("fact ownership requires a batch artifact")
	}
	for _, key := range e.coordinator.program.OwnershipFacts() {
		if err := policy.Validate(key); err != nil {
			return err
		}
	}
	return nil
}
