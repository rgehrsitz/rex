package store

import (
	"fmt"
	"sort"
	"strings"
)

// FactOwnership is an immutable exact-name allowlist. Nil means legacy mode.
type FactOwnership struct {
	facts map[string]struct{}
	names []string
}

func NewFactOwnership(names []string) (*FactOwnership, error) {
	if names == nil {
		return nil, nil
	}
	if len(names) == 0 || len(names) > 100000 {
		return nil, fmt.Errorf("ownership requires 1-100000 exact fact names")
	}
	p := &FactOwnership{facts: map[string]struct{}{}, names: append([]string(nil), names...)}
	for _, name := range names {
		if name == "" || len(name) > 1024 || IsInternalKey(name) || strings.HasPrefix(name, "rex:durable:") {
			return nil, fmt.Errorf("invalid owned fact %q", name)
		}
		if _, ok := p.facts[name]; ok {
			return nil, fmt.Errorf("duplicate owned fact %q", name)
		}
		p.facts[name] = struct{}{}
	}
	sort.Strings(p.names)
	return p, nil
}
func (p *FactOwnership) Validate(name string) error {
	if p == nil {
		return nil
	}
	if _, ok := p.facts[name]; !ok {
		return fmt.Errorf("fact %q is outside partition ownership", name)
	}
	return nil
}
func (p *FactOwnership) Names() []string {
	if p == nil {
		return nil
	}
	return append([]string(nil), p.names...)
}
