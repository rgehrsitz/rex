package compiler

import (
	"fmt"
	"net"
	"rgehrsitz/rex/internal/bytecode"
	"rgehrsitz/rex/internal/rule"
)

type CompiledRule struct {
	Name         string
	Instructions []bytecode.Instruction
	Dependencies []string // Names of dependent rules
}

// DependencyGraph represents the graph of rule dependencies using rule names
type DependencyGraph struct {
	edges   map[string][]string
	visited map[string]bool
}

// Dependencies returns the dependencies for the given rule name.
func (g *DependencyGraph) Dependencies(ruleName string) []string {
	return g.edges[ruleName]
}

func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		edges:   make(map[string][]string),
		visited: make(map[string]bool),
	}
}

func (g *DependencyGraph) AddDependency(from, to string) {
	g.edges[from] = append(g.edges[from], to)
}

func (g *DependencyGraph) CheckCircularDependency(node string) bool {
	if g.visited[node] {
		return true
	}
	g.visited[node] = true
	defer func() { g.visited[node] = false }()

	for _, dep := range g.edges[node] {
		if g.CheckCircularDependency(dep) {
			return true
		}
	}
	return false
}

func CompileRule(r *rule.Rule, graph *DependencyGraph, allRules []rule.Rule) ([]bytecode.Instruction, error) {
	var instructions []bytecode.Instruction

	// Analyze and Populate ConsumedFacts and ProducedFacts
	r.ConsumedFacts = getConsumedFacts(r.Conditions)
	r.ProducedFacts = getProducedFacts(r.Event.Actions)

	// Compile 'All' Conditions
	for _, cond := range r.Conditions.All {
		compiled, err := compileCondition(cond)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, compiled...)
	}

	// Compile 'Any' Conditions
	for _, cond := range r.Conditions.Any {
		compiled, err := compileCondition(cond)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, compiled...)
	}

	// Compile Actions
	for _, action := range r.Event.Actions {
		compiledAction, err := compileAction(action)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, compiledAction...)
	}

	// Update Dependency Graph
	for _, consumedFact := range r.ConsumedFacts {
		for _, otherRule := range allRules {
			if contains(otherRule.ProducedFacts, consumedFact) && otherRule.Name != r.Name {
				graph.AddDependency(r.Name, otherRule.Name)
			}
		}
	}

	// Check for Circular Dependencies
	if graph.CheckCircularDependency(r.Name) {
		return nil, fmt.Errorf("circular dependency detected in rule '%s'", r.Name)
	}

	return instructions, nil
}

// contains checks if a slice of strings contains a specific string.
func contains(slice []string, element string) bool {
	for _, item := range slice {
		if item == element {
			return true
		}
	}
	return false
}

func compileCondition(cond rule.Condition) ([]bytecode.Instruction, error) {

	if len(cond.All) > 0 || len(cond.Any) > 0 {
		// Handle nested conditions
		return compileNestedCondition(cond)
	}

	// Convert condition based on the operator
	switch cond.Operator {
	case "equal", "notEqual", "greaterThan", "lessThan", "greaterThanOrEqual", "lessThanOrEqual":
		return compileComparisonCondition(cond)
	case "contains", "notContains":
		return compileContainsCondition(cond)
	default:
		return nil, fmt.Errorf("unsupported operator: %s", cond.Operator)
	}
}

func compileAnyConditions(conditions []rule.Condition) ([]bytecode.Instruction, error) {
	var instructions []bytecode.Instruction
	var jumpPlaceholders []int

	for i, cond := range conditions {
		compiled, err := compileCondition(cond)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, compiled...)

		// For all but the last condition, add a jump instruction
		if i < len(conditions)-1 {
			jumpPlaceholder := len(instructions)
			jumpPlaceholders = append(jumpPlaceholders, jumpPlaceholder)
			instructions = append(instructions, bytecode.Instruction{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{0}})
		}
	}

	// Correctly set jump destinations
	endOfAnyBlock := len(instructions)
	for _, placeholder := range jumpPlaceholders {
		// Adjusting the destination to account for the position of the jump instruction itself
		instructions[placeholder].Operands[0] = endOfAnyBlock + 1
	}

	return instructions, nil
}

func compileNestedCondition(cond rule.Condition) ([]bytecode.Instruction, error) {
	var instructions []bytecode.Instruction
	var err error

	// Recursively compile 'All' conditions
	for _, c := range cond.All {
		nestedInstr, err := compileCondition(c)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, nestedInstr...)
	}

	// Recursively compile 'Any' conditions
	if len(cond.Any) > 0 {
		anyInstr, err := compileAnyConditions(cond.Any)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, anyInstr...)
	}

	return instructions, err
}

// compileComparisonCondition handles comparison operators
func compileComparisonCondition(cond rule.Condition) ([]bytecode.Instruction, error) {
	var instructions []bytecode.Instruction
	opcode, err := getOpcodeForComparison(cond.Operator)
	if err != nil {
		return nil, err
	}

	// Load fact
	instructions = append(instructions, bytecode.Instruction{Opcode: bytecode.OpLoadFact, Operands: []interface{}{cond.Fact}})

	// Prepare value operand based on type
	var valueOperand interface{}
	switch v := cond.Value.(type) {
	case int, float64, string, bool:
		valueOperand = v
	default:
		return nil, fmt.Errorf("unsupported value type: %T", v)
	}

	// Add comparison instruction with value
	instructions = append(instructions, bytecode.Instruction{Opcode: opcode, Operands: []interface{}{valueOperand}})
	return instructions, nil
}

func getOpcodeForComparison(operator string) (bytecode.Opcode, error) {
	switch operator {
	case "equal":
		return bytecode.OpEqual, nil
	case "notEqual":
		return bytecode.OpNotEqual, nil
	case "greaterThan":
		return bytecode.OpGreaterThan, nil
	case "lessThan":
		return bytecode.OpLessThan, nil
	case "greaterThanOrEqual":
		return bytecode.OpGreaterThanOrEqual, nil
	case "lessThanOrEqual":
		return bytecode.OpLessThanOrEqual, nil
	default:
		return 0, fmt.Errorf("unknown comparison operator: %s", operator)
	}
}

func compileContainsCondition(cond rule.Condition) ([]bytecode.Instruction, error) {
	var instructions []bytecode.Instruction
	var opcode bytecode.Opcode

	switch cond.Operator {
	case "contains":
		opcode = bytecode.OpContains
	case "notContains":
		opcode = bytecode.OpNotContains
	default:
		return nil, fmt.Errorf("unsupported contains operator: %s", cond.Operator)
	}

	instructions = append(instructions, bytecode.Instruction{Opcode: opcode, Operands: []interface{}{cond.Fact, cond.Value}})
	return instructions, nil
}

func CompileRulesWithDependencies(rules []rule.Rule) ([]CompiledRule, error) {
	compiledRules := make([]CompiledRule, len(rules))
	dependencyGraph := NewDependencyGraph()

	// Compile each rule and update the dependency graph
	for i, r := range rules {
		instructions, err := CompileRule(&r, dependencyGraph, rules)
		if err != nil {
			return nil, fmt.Errorf("error compiling rule '%s': %w", r.Name, err)
		}
		compiledRules[i] = CompiledRule{
			Name:         r.Name,
			Instructions: instructions,
			// Dependencies will be inferred directly from the dependency graph
		}
	}

	// Update dependencies for each compiled rule
	for i, r := range rules {
		compiledRules[i].Dependencies = append(compiledRules[i].Dependencies, dependencyGraph.edges[r.Name]...)
	}

	return compiledRules, nil
}

// func getReadFacts(conditions []rule.Condition) []string {
// 	var facts []string
// 	for _, cond := range conditions {
// 		facts = append(facts, cond.Fact)
// 		facts = append(facts, getReadFacts(cond.All)...)
// 		facts = append(facts, getReadFacts(cond.Any)...)
// 	}
// 	return facts
// }

// func getWriteFacts(event rule.Event) []string {
// 	var writeFacts []string

// 	// Iterate over all actions in the event
// 	for _, action := range event.Actions {
// 		// Check if the action type is 'updateStore', which changes a fact
// 		if action.Type == "updateStore" {
// 			// Add the target of the action to the list of written facts
// 			writeFacts = append(writeFacts, action.Target)
// 		}
// 	}

// 	return writeFacts
// }

func compileAction(action rule.Action) ([]bytecode.Instruction, error) {
	var instructions []bytecode.Instruction

	switch action.Type {
	case "updateStore":
		// Add validation for the target of the action
		if !isValidStoreKey(action.Target) {
			return nil, fmt.Errorf("invalid store key: %s", action.Target)
		}
		// Compile update store action into bytecode
		updateInstruction := bytecode.Instruction{
			Opcode:   bytecode.OpUpdateStore,
			Operands: []interface{}{action.Target, action.Value},
		}
		instructions = append(instructions, updateInstruction)

	case "sendMessage":
		// Add validation for the target of the action
		if !isValidAddress(action.Target) {
			return nil, fmt.Errorf("invalid address: %s", action.Target)
		}
		// Compile send message action into bytecode
		messageInstruction := bytecode.Instruction{
			Opcode:   bytecode.OpSendMessage,
			Operands: []interface{}{action.Target, action.Value},
		}
		instructions = append(instructions, messageInstruction)

	default:
		return nil, fmt.Errorf("unsupported action type: %s", action.Type)
	}

	return instructions, nil
}

func isValidStoreKey(key string) bool {
	// Check if the key is non-empty
	if key == "" {
		return false
	}

	// Optionally, add more specific checks here, like length or character set
	// Example: Check if the key length is within a specific range
	if len(key) < 3 || len(key) > 100 {
		return false
	}

	// Example: Check for allowed characters (alphanumeric and underscores)
	for _, ch := range key {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
			return false
		}
	}

	return true
}

func isValidAddress(address string) bool {
	// Check if the address is a valid IP address
	if net.ParseIP(address) != nil {
		return true
	}

	// Optionally, add more specific checks here, like checking for valid DNS names, ports, etc.
	// Example: Check if it's a valid host:port pair
	host, _, err := net.SplitHostPort(address)
	if err == nil && net.ParseIP(host) != nil {
		return true
	}

	return false
}

func getConsumedFacts(conditions rule.Conditions) []string {
	var consumedFacts []string
	for _, cond := range conditions.All {
		consumedFacts = append(consumedFacts, extractFactsFromCondition(cond)...)
	}
	for _, cond := range conditions.Any {
		consumedFacts = append(consumedFacts, extractFactsFromCondition(cond)...)
	}
	return consumedFacts
}

func extractFactsFromCondition(cond rule.Condition) []string {
	var facts []string
	if cond.Fact != "" {
		facts = append(facts, cond.Fact)
	}
	for _, subCond := range cond.All {
		facts = append(facts, extractFactsFromCondition(subCond)...)
	}
	for _, subCond := range cond.Any {
		facts = append(facts, extractFactsFromCondition(subCond)...)
	}
	return facts
}

func getProducedFacts(actions []rule.Action) []string {
	var producedFacts []string
	for _, action := range actions {
		if action.Type == "updateStore" {
			producedFacts = append(producedFacts, action.Target)
		}
		// Additional logic for other action types that produce facts, if any.
	}
	return producedFacts
}
