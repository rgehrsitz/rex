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

func CompileRule(r *rule.Rule) ([]bytecode.Instruction, error) {
	var instructions []bytecode.Instruction

	// Compile 'All' Conditions directly since they don't require special jump logic
	for _, cond := range r.Conditions.All {
		compiled, err := compileCondition(cond)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, compiled...)
	}

	// Special handling for 'Any' Conditions to implement jump logic
	if len(r.Conditions.Any) > 0 {
		compiledAny, err := compileAnyConditions(r.Conditions.Any)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, compiledAny...)
	}

	// Compile Actions
	for _, action := range r.Event.Actions {
		compiledAction, err := compileAction(action)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, compiledAction...)
	}

	// Conditionally append event trigger instruction if an event type is defined
	if r.Event.EventType != "" {
		eventOperands := []interface{}{r.Event.EventType}
		// Include CustomProperty if it's not empty, otherwise append nil for consistency
		if r.Event.CustomProperty != "" {
			eventOperands = append(eventOperands, r.Event.CustomProperty)
		} else {
			eventOperands = append(eventOperands, nil)
		}

		instructions = append(instructions, bytecode.Instruction{
			Opcode:   bytecode.OpTriggerEvent,
			Operands: eventOperands,
		})
	}

	return instructions, nil
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
	// This slice stores the positions where the jump instructions will be patched.
	var jumpPlaceholders []int

	for i, cond := range conditions {
		compiled, err := compileCondition(cond)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, compiled...)

		// For all but the last condition, append a jump instruction placeholder
		if i < len(conditions)-1 {
			jumpPlaceholders = append(jumpPlaceholders, len(instructions))
			// Append a placeholder jump instruction; actual target set later
			instructions = append(instructions, bytecode.Instruction{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{0}})
		}
	}

	// Now, set the correct targets for the jump instructions
	for _, placeholder := range jumpPlaceholders {
		// Calculate how far we need to jump ahead to skip the remaining conditions
		// The target is the total length of instructions minus the position of the jump instruction itself
		instructions[placeholder].Operands[0] = len(instructions) - placeholder - 1
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

func CompileRuleSet(rules []rule.Rule) ([]CompiledRule, error) {
	var compiledRules []CompiledRule
	usedNames := make(map[string]struct{})

	PreprocessRulesFacts(rules)
	dependencyGraph := PreprocessDependencies(rules)

	// Assuming preprocessing of facts and dependencies has already been done,
	// and a filled dependencyGraph is passed as a parameter

	for _, r := range rules {
		if _, exists := usedNames[r.Name]; exists {
			return nil, fmt.Errorf("duplicate rule name detected: '%s'", r.Name)
		}
		usedNames[r.Name] = struct{}{}

		instructions, err := CompileRule(&r) // Note: Adjust CompileRule as needed
		if err != nil {
			return nil, fmt.Errorf("error compiling rule '%s': %w", r.Name, err)
		}

		// Append compiled rule
		compiledRule := CompiledRule{
			Name:         r.Name,
			Instructions: instructions,
			Dependencies: dependencyGraph.Dependencies(r.Name), // Set dependencies from the preprocessed graph
		}
		compiledRules = append(compiledRules, compiledRule)
	}

	// Circular dependency check might still be relevant but ensure it's done based on the preprocessed graph
	for _, rule := range compiledRules {
		if dependencyGraph.CheckCircularDependency(rule.Name) {
			return nil, fmt.Errorf("circular dependency detected involving rule '%s'", rule.Name)
		}
	}

	return compiledRules, nil
}

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

func PreprocessRulesFacts(rules []rule.Rule) {
	for i := range rules {
		rules[i].ConsumedFacts = getConsumedFacts(rules[i].Conditions)
		rules[i].ProducedFacts = getProducedFacts(rules[i].Event.Actions)
	}
}

func PreprocessDependencies(rules []rule.Rule) *DependencyGraph {
	graph := NewDependencyGraph()

	// Create a mapping of produced facts to rules that produce them for efficient lookups
	factToRule := make(map[string][]string)
	for _, r := range rules {
		for _, fact := range r.ProducedFacts {
			factToRule[fact] = append(factToRule[fact], r.Name)
		}
	}

	// Determine dependencies based on consumed facts
	for _, r := range rules {
		for _, fact := range r.ConsumedFacts {
			if producers, exists := factToRule[fact]; exists {
				for _, producer := range producers {
					if producer != r.Name { // Avoid self-dependency
						graph.AddDependency(r.Name, producer)
					}
				}
			}
		}
	}

	return graph
}
