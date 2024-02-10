package dependencygraph

import (
	"fmt"
	"rgehrsitz/rex/internal/rule"
)

// DependencyGraph represents the graph of rule dependencies.
type DependencyGraph struct {
	edges   map[string][]string
	visited map[string]bool
}

// New creates a new instance of a DependencyGraph.
func New() *DependencyGraph {
	return &DependencyGraph{
		edges:   make(map[string][]string),
		visited: make(map[string]bool),
	}
}

func BuildDependencyGraph(rules []rule.Rule) *DependencyGraph {
	graph := New()

	// Mapping from produced facts to rules that produce them
	factToRule := make(map[string][]string)

	for _, r := range rules {
		for _, fact := range r.ProducedFacts {
			factToRule[fact] = append(factToRule[fact], r.Name)
		}
	}

	for _, r := range rules {
		for _, fact := range r.ConsumedFacts {
			if producers, exists := factToRule[fact]; exists {
				for _, producer := range producers {
					graph.Add(producer, r.Name)
				}
			}
		}
	}

	return graph
}

// Add adds a dependency from one rule to another in the graph.
func (g *DependencyGraph) Add(from, to string) {
	g.edges[from] = append(g.edges[from], to)
}

// DependenciesOf returns the dependencies for a given rule.
func (g *DependencyGraph) DependenciesOf(ruleName string) []string {
	return g.edges[ruleName]
}

// TopologicalSort returns a sorted slice of rule names based on their dependencies.
func (g *DependencyGraph) TopologicalSort() ([]string, error) {
	// Initialize a map to count incoming edges for each node
	incomingEdges := make(map[string]int)
	for node := range g.edges {
		incomingEdges[node] = 0 // Initialize with zero incoming edges
	}

	// Populate incoming edges count
	for _, dependencies := range g.edges {
		for _, dep := range dependencies {
			incomingEdges[dep]++
		}
	}

	// Find all nodes with no incoming edges
	var queue []string
	for node, count := range incomingEdges {
		if count == 0 {
			queue = append(queue, node)
		}
	}

	var sorted []string
	// While there are nodes with no incoming edges
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)

		// Decrease the incoming edge count for dependent nodes
		for _, dep := range g.edges[node] {
			incomingEdges[dep]--
			// If a node has no more incoming edges, add it to the queue
			if incomingEdges[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	// Check for a circular dependency (if there are edges remaining)
	if len(sorted) < len(g.edges) {
		return nil, fmt.Errorf("circular dependency detected")
	}

	return sorted, nil
}
