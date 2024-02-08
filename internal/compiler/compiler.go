package compiler

import (
	"fmt"
	"net"
	"rgehrsitz/rex/internal/bytecode"
	"rgehrsitz/rex/internal/rule"
)

type CompiledRule struct {
	Name               string
	Instructions       []bytecode.Instruction
	RuleDependencies   []string // Names of dependent rules
	SensorDependencies []string // Names of sensors (facts) required for the rule
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

func CompileRule(r *rule.Rule) ([]bytecode.Instruction, []string, error) {
	var instructions []bytecode.Instruction
	sensorDependenciesMap := make(map[string]struct{}) // Use map to avoid duplicates

	// Function to recursively compile conditions and collect sensor dependencies
	var compileConditions func([]rule.Condition)
	compileConditions = func(conditions []rule.Condition) {
		for _, cond := range conditions {
			if cond.Fact != "" {
				sensorDependenciesMap[cond.Fact] = struct{}{} // Add fact as a sensor dependency
			}
			// Recursively compile nested 'All' conditions
			if len(cond.All) > 0 {
				compileConditions(cond.All)
			}
			// Recursively compile nested 'Any' conditions
			if len(cond.Any) > 0 {
				compileConditions(cond.Any)
			}
			// Compile the current condition
			compiled, err := compileCondition(cond)
			if err != nil {
				continue // Handle error appropriately
			}
			instructions = append(instructions, compiled...)
		}
	}

	// Compile 'All' and 'Any' conditions using the recursive function
	compileConditions(r.Conditions.All)
	compileConditions(r.Conditions.Any)

	// Compile Actions
	for _, action := range r.Event.Actions {
		compiledAction, err := compileAction(action)
		if err != nil {
			return nil, nil, err
		}
		instructions = append(instructions, compiledAction...)
		// This section is simplified since actions don't contribute to sensorDependencies in this context
	}

	// Convert sensorDependenciesMap to slice
	var sensorDependencies []string
	for sensor := range sensorDependenciesMap {
		sensorDependencies = append(sensorDependencies, sensor)
	}

	// Conditionally append event trigger instruction if an event type is defined
	if r.Event.EventType != "" {
		eventOperands := []interface{}{r.Event.EventType}
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

	return instructions, sensorDependencies, nil
}

func compileCondition(cond rule.Condition, sensorDependencies *map[string]struct{}) ([]bytecode.Instruction, error) {
	var instructions []bytecode.Instruction

	if cond.Fact != "" {
		(*sensorDependencies)[cond.Fact] = struct{}{}

		switch cond.Operator {
		case "equal", "notEqual", "greaterThan", "lessThan", "greaterThanOrEqual", "lessThanOrEqual":
			compInstructions, err := compileComparisonCondition(cond)
			if err != nil {
				return nil, err
			}
			instructions = append(instructions, compInstructions...)
		case "contains", "notContains":
			containsInstructions, err := compileContainsCondition(cond)
			if err != nil {
				return nil, err
			}
			instructions = append(instructions, containsInstructions...)
		default:
			return nil, fmt.Errorf("unsupported operator: %s", cond.Operator)
		}
	}

	// Recursive handling for 'All' and 'Any' nested conditions
	for _, nestedCond := range cond.All {
		nestedInstructions, err := compileCondition(nestedCond, sensorDependencies)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, nestedInstructions...)
	}

	if len(cond.Any) > 0 {
		anyInstructions, err := compileAnyConditions(cond.Any, sensorDependencies)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, anyInstructions...)
	}

	return instructions, nil
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

		instructions, sensorDependencies, err := CompileRule(&r)
		if err != nil {
			return nil, fmt.Errorf("error compiling rule '%s': %w", r.Name, err)
		}

		// Include sensorDependencies in the CompiledRule
		compiledRules = append(compiledRules, CompiledRule{
			Name:               r.Name,
			Instructions:       instructions,
			RuleDependencies:   dependencyGraph.Dependencies(r.Name), // Rule dependencies
			SensorDependencies: sensorDependencies,                   // Sensor dependencies
		})
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
