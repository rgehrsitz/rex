package tooling

import (
	"fmt"
	"sort"

	"rgehrsitz/rex/pkg/compiler"
)

// PartitionGroup is a conservative connected component of rules and public
// facts. Even shared reads join components: mutable facts have one owner.
type PartitionGroup struct {
	ID    int      `json:"id"`
	Rules []string `json:"rules"`
	Facts []string `json:"facts"`
}

type PartitionReport struct {
	SchemaVersion      int              `json:"schema_version"`
	ArtifactSHA256     string           `json:"artifact_sha256"`
	ExecutionContract  uint32           `json:"execution_contract"`
	AdvisoryOnly       bool             `json:"advisory_only"`
	Groups             []PartitionGroup `json:"groups"`
	UnusedDeclarations []string         `json:"unused_declarations"`
}

// PlanPartitions reports potential ownership boundaries, not permission to run
// concurrent workers. Input routing, shared protocol keys, retained artifacts,
// temporal state migration and multi-group event atomicity require deployment
// policy that an artifact alone cannot establish.
func PlanPartitions(artifact []byte) (PartitionReport, error) {
	rules, err := compiler.DecodeBatch(artifact)
	if err != nil {
		return PartitionReport{}, fmt.Errorf("partition-plan requires a valid batch artifact: %w", err)
	}
	version, err := compiler.BatchArtifactVersion(artifact)
	if err != nil {
		return PartitionReport{}, err
	}
	out := PartitionReport{SchemaVersion: SchemaVersion, ArtifactSHA256: Digest(artifact), ExecutionContract: version, AdvisoryOnly: true, Groups: []PartitionGroup{}, UnusedDeclarations: []string{}}
	parent := make([]int, len(rules.Rules))
	for i := range parent {
		parent[i] = i
	}
	var root func(int) int
	root = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	owners := map[string]int{}
	for i, rule := range rules.Rules {
		facts := Dependencies(rule)
		for _, action := range rule.Actions {
			facts = append(facts, action.Target)
		}
		for _, fact := range facts {
			if old, ok := owners[fact]; ok {
				a, b := root(i), root(old)
				if a != b {
					if a > b {
						a, b = b, a
					}
					parent[b] = a
				}
			} else {
				owners[fact] = i
			}
		}
	}
	groups := map[int]int{}
	for i, rule := range rules.Rules {
		r := root(i)
		index, ok := groups[r]
		if !ok {
			index = len(out.Groups)
			groups[r] = index
			out.Groups = append(out.Groups, PartitionGroup{ID: index, Rules: []string{}, Facts: []string{}})
		}
		out.Groups[index].Rules = append(out.Groups[index].Rules, rule.Name)
	}
	for fact, owner := range owners {
		index := groups[root(owner)]
		out.Groups[index].Facts = append(out.Groups[index].Facts, fact)
	}
	for i := range out.Groups {
		sort.Strings(out.Groups[i].Facts)
	}
	for fact := range rules.Facts {
		if _, used := owners[fact]; !used {
			out.UnusedDeclarations = append(out.UnusedDeclarations, fact)
		}
	}
	sort.Strings(out.UnusedDeclarations)
	return out, nil
}
