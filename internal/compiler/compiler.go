package compiler

import (
	"fmt"
	"rgehrsitz/rex/internal/bytecode"
	"rgehrsitz/rex/internal/rule"
)

func CompileRuleSet(rules []rule.Rule) ([]bytecode.Instruction, error) {
	AnalyzeRuleDependencies(rules)
	dependencyGraph := BuildDependencyGraph(rules)
	orderedRuleNames, err := BuildEvaluationOrder(rules, dependencyGraph)
	if err != nil {
		fmt.Println("Error determining evaluation order:", err)
		return nil, err
	}

	// Now orderedRuleNames is used to compile the ruleset to bytecode in the correct order.
	program, err := CompileRulesetToBytecode(rules, orderedRuleNames)
	if err != nil {
		fmt.Println("Error compiling ruleset to bytecode:", err)
		return nil, err
	}

	return program, nil
}

// CompileRulesetToBytecode compiles the ruleset to bytecode.
//
// It takes in a slice of rule.Rule and a slice of string as parameters.
// It returns a slice of bytecode.Instruction and an error.
func CompileRulesetToBytecode(rules []rule.Rule, orderedRuleNames []string) ([]bytecode.Instruction, error) {
	var program []bytecode.Instruction
	ruleMap := make(map[string]rule.Rule)

	// Create a map for quick lookup.
	for _, r := range rules {
		ruleMap[r.Name] = r
	}

	// Compile rules in the order specified by orderedRuleNames.
	for _, name := range orderedRuleNames {
		r, exists := ruleMap[name]
		if !exists {
			return nil, fmt.Errorf("rule '%s' not found in ruleset", name)
		}

		instrs, err := TranslateRuleToBytecode(r)
		if err != nil {
			return nil, fmt.Errorf("error compiling rule '%s': %w", r.Name, err)
		}
		program = append(program, instrs...)
	}

	return program, nil
}

// func SaveCompiledRules(compiledRules []CompiledRule, filePath string) error {
// 	data, err := json.Marshal(compiledRules)
// 	if err != nil {
// 		return err
// 	}
// 	return os.WriteFile(filePath, data, 0644)
// }

// AnalyzeRuleDependencies updates each rule with its consumed and produced facts.
func AnalyzeRuleDependencies(rules []rule.Rule) {
	for i, r := range rules {
		var consumed, produced []string

		// Analyze conditions to determine consumed facts.
		for _, cond := range append(r.Conditions.All, r.Conditions.Any...) {
			consumed = append(consumed, cond.Fact)
		}

		// Analyze actions to determine produced facts.
		// This example assumes that an action might produce a fact, adjust based on your rule actions' specifics.
		for _, action := range r.Event.Actions {
			// Example: If action.Type indicates a fact modification, add it to produced.
			// Adjust this logic based on how your actions are structured.
			produced = append(produced, action.Target)
		}

		// Update the rule with consumed and produced facts.
		rules[i].ConsumedFacts = uniqueStrings(consumed)
		rules[i].ProducedFacts = uniqueStrings(produced)
	}
}

// Removes duplicates from a slice of strings.
func uniqueStrings(input []string) []string {
	unique := make(map[string]bool)
	var list []string
	for _, item := range input {
		if _, value := unique[item]; !value {
			unique[item] = true
			list = append(list, item)
		}
	}
	return list
}

type DependencyGraph struct {
	Edges map[string][]string // Maps a fact to rules that are dependent on it
}

// BuildDependencyGraph constructs the dependency graph from the given rules.
func BuildDependencyGraph(rules []rule.Rule) DependencyGraph {
	graph := DependencyGraph{Edges: make(map[string][]string)}
	for _, r := range rules {
		for _, fact := range r.ConsumedFacts {
			graph.Edges[fact] = append(graph.Edges[fact], r.Name)
		}
	}
	return graph
}

// Node represents a node in the dependency graph, which corresponds to a rule.
type Node struct {
	Name     string
	InDegree int
	OutEdges []string
}

// BuildEvaluationOrder returns an ordered list of rule names based on dependencies, using topological sorting.
func BuildEvaluationOrder(rules []rule.Rule, graph DependencyGraph) ([]string, error) {
	// Initialize nodes with in-degree counts.
	nodes := make(map[string]*Node)
	for _, r := range rules {
		nodes[r.Name] = &Node{Name: r.Name, InDegree: 0} // OutEdges will be populated next.
	}

	// Populate out edges and in-degree counts based on the dependency graph.
	for fact, dependentRules := range graph.Edges {
		for _, ruleName := range dependentRules {
			for _, r := range rules {
				if contains(r.ConsumedFacts, fact) && nodes[ruleName] != nil {
					nodes[ruleName].InDegree++
					nodes[r.Name].OutEdges = append(nodes[r.Name].OutEdges, ruleName)
				}
			}
		}
	}

	// Perform topological sorting.
	var order []string
	// Find all nodes with in-degree of 0.
	queue := []string{}
	for _, node := range nodes {
		if node.InDegree == 0 {
			queue = append(queue, node.Name)
		}
	}

	for len(queue) > 0 {
		// Dequeue a node with in-degree 0.
		current := queue[0]
		queue = queue[1:]
		order = append(order, current)

		// Decrease the in-degree of each successor.
		for _, successor := range nodes[current].OutEdges {
			nodes[successor].InDegree--
			if nodes[successor].InDegree == 0 {
				queue = append(queue, successor)
			}
		}
	}

	// Check for cycles (which indicate a deadlock in rule evaluation).
	if len(order) != len(rules) {
		return nil, fmt.Errorf("cycle detected in rule dependencies, indicating a deadlock")
	}

	return order, nil
}

// Utility function to check if a slice contains a string.
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

func TranslateRuleToBytecode(r rule.Rule) ([]bytecode.Instruction, error) {
	var instructions []bytecode.Instruction

	// Process "All" conditions.
	for _, cond := range r.Conditions.All {
		var condInstrs []bytecode.Instruction
		var err error
		var jumpNeeded bool

		if cond.Fact != "" || len(cond.All) > 0 || len(cond.Any) > 0 {
			// Direct condition or nested conditions
			condInstrs, jumpNeeded, err = translateConditionToBytecode(cond, false)
			if err != nil {
				return nil, err
			}
			instructions = append(instructions, condInstrs...)
			if jumpNeeded {
				// Update the jump offset for the last appended jump instruction
				updateLastJumpOffset(&instructions)
			}
		}
	}

	// Translate actions/events as a batch.
	actionInstructions, err := translateActionsToBytecode(r.Event.Actions)
	if err != nil {
		return nil, err
	}
	instructions = append(instructions, actionInstructions...)

	return instructions, nil
}

// updateLastJumpOffset finds the last jump instruction and updates its offset to point to the end of the instructions slice.
func updateLastJumpOffset(instructions *[]bytecode.Instruction) {
	// Iterate backwards to find the last jump instruction.
	for i := len(*instructions) - 1; i >= 0; i-- {
		instr := (*instructions)[i]
		if instr.Opcode == bytecode.OpJumpIfFalse || instr.Opcode == bytecode.OpJumpIfTrue {
			// Calculate the offset from this jump instruction to the end of the instructions slice.
			// This offset is the distance to the next instruction that would execute after the jump.
			offset := len(*instructions) - i - 1

			// Update the operand of the jump instruction with the correct offset.
			(*instructions)[i].Operands[0] = bytecode.JumpOffsetOperand{Offset: offset}
			break // Stop after updating the last jump instruction.
		}
	}
}

// translateConditionToBytecode handles both simple and nested conditions, including "all" and "any" structures.
// It now returns both the bytecode instructions and a boolean indicating if a jump instruction was appended to skip actions.
func translateConditionToBytecode(cond rule.Condition, isNested bool) ([]bytecode.Instruction, bool, error) {
	var instructions []bytecode.Instruction
	jumpAppended := false

	// Handle direct condition.
	if cond.Fact != "" {
		instr, err := translateSimpleCondition(cond)
		if err != nil {
			return nil, false, err
		}
		instructions = append(instructions, instr...)
	}

	// Handle "All" conditions with recursive logic.
	if len(cond.All) > 0 {
		allInstrs, jumpNeeded, err := translateAllConditions(cond.All)
		if err != nil {
			return nil, false, err
		}
		instructions = append(instructions, allInstrs...)
		if jumpNeeded && !isNested {
			// Append jump to skip actions if in the top-level "all" condition.
			instructions = append(instructions, bytecode.Instruction{Opcode: bytecode.OpJumpIfFalse, Operands: []bytecode.Operand{bytecode.JumpOffsetOperand{Offset: 0}}}) // Placeholder for offset.
			jumpAppended = true
		}
	}

	// Handle "Any" conditions with recursive logic.
	if len(cond.Any) > 0 {
		anyInstrs, jumpNeeded, err := translateAnyConditions(cond.Any)
		if err != nil {
			return nil, false, err
		}
		instructions = append(instructions, anyInstrs...)
		if jumpNeeded && !isNested {
			// Append jump to end of "any" structure if true, only if not nested.
			instructions = append(instructions, bytecode.Instruction{Opcode: bytecode.OpJumpIfTrue, Operands: []bytecode.Operand{bytecode.JumpOffsetOperand{Offset: 0}}}) // Placeholder for offset.
			jumpAppended = true
		}
	}

	return instructions, jumpAppended, nil
}

// translateSimpleCondition translates a single condition into bytecode.
func translateSimpleCondition(cond rule.Condition) ([]bytecode.Instruction, error) {
	// Initialize a slice to hold the instructions.
	var instructions []bytecode.Instruction

	// First, load the fact involved in the condition.
	loadFactInstr := bytecode.Instruction{
		Opcode: bytecode.OpLoadFact,
		Operands: []bytecode.Operand{
			bytecode.FactOperand{FactName: cond.Fact},
		},
	}
	instructions = append(instructions, loadFactInstr)

	// Determine the appropriate opcode based on the condition's operator.
	opcode, err := getOpcodeForOperator(cond.Operator)
	if err != nil {
		return nil, err // Return an error if the operator is not supported.
	}

	// Construct the comparison instruction with the condition's value.
	compareInstr := bytecode.Instruction{
		Opcode: opcode,
		Operands: []bytecode.Operand{
			bytecode.ValueOperand{Value: cond.Value},
		},
	}
	instructions = append(instructions, compareInstr)

	return instructions, nil
}

// translateAllConditions translates a list of "all" conditions into bytecode, indicating if a jump is needed.
func translateAllConditions(conds []rule.Condition) ([]bytecode.Instruction, bool, error) {
	var instructions []bytecode.Instruction
	jumpNeeded := false // Tracks if we appended a jump instruction that needs its offset updated.

	for i, cond := range conds {
		var condInstructions []bytecode.Instruction
		var err error

		// Check if the condition is a simple condition or has nested conditions.
		if cond.Fact != "" {
			// It's a simple condition.
			condInstructions, err = translateSimpleCondition(cond)
			if err != nil {
				return nil, false, err
			}
		} else {
			// The condition might have nested "all" or "any" structures.
			if len(cond.All) > 0 {
				// Nested "all" conditions.
				condInstructions, _, err = translateAllConditions(cond.All) // We ignore the jumpNeeded return for nested "all" as we handle it below.
			} else if len(cond.Any) > 0 {
				// Nested "any" conditions.
				condInstructions, _, err = translateAnyConditions(cond.Any) // Similarly, jumpNeeded handling for "any" is managed separately.
			}

			if err != nil {
				return nil, false, err
			}
		}

		instructions = append(instructions, condInstructions...)

		// For "all" conditions, after each condition, we append a jump instruction to skip the rest if the condition is false.
		// This jump's offset will be updated later, once the entire set of conditions (and potential actions) is known.
		if i < len(conds)-1 || len(cond.All) > 0 || len(cond.Any) > 0 {
			instructions = append(instructions, bytecode.Instruction{
				Opcode:   bytecode.OpJumpIfFalse,
				Operands: []bytecode.Operand{bytecode.JumpOffsetOperand{Offset: 0}}, // Placeholder offset.
			})
			jumpNeeded = true // Indicate that we've appended a jump instruction that needs its offset updated.
		}
	}

	return instructions, jumpNeeded, nil
}

// translateAnyConditions translates a list of "any" conditions into bytecode, indicating if a jump is needed.
func translateAnyConditions(conds []rule.Condition) ([]bytecode.Instruction, bool, error) {
	var instructions []bytecode.Instruction
	var jumpIndexes []int // To keep track of the indexes where jump offsets need to be updated.

	for i, cond := range conds {
		var condInstructions []bytecode.Instruction
		var err error

		// Check if the condition is simple or contains nested structures.
		if cond.Fact != "" {
			// It's a simple condition.
			condInstructions, err = translateSimpleCondition(cond)
			if err != nil {
				return nil, false, err
			}
		} else {
			// The condition has nested "all" or "any" structures.
			if len(cond.All) > 0 {
				// Nested "all" conditions.
				condInstructions, _, err = translateAllConditions(cond.All)
			} else if len(cond.Any) > 0 {
				// Nested "any" conditions.
				condInstructions, _, err = translateAnyConditions(cond.Any)
			}

			if err != nil {
				return nil, false, err
			}
		}

		instructions = append(instructions, condInstructions...)

		// For "any" conditions, if a condition is true, jump to the end of the "any" structure.
		if i < len(conds)-1 {
			jumpInstr := bytecode.Instruction{
				Opcode:   bytecode.OpJumpIfTrue,
				Operands: []bytecode.Operand{bytecode.JumpOffsetOperand{Offset: 0}}, // Placeholder offset.
			}
			instructions = append(instructions, jumpInstr)
			jumpIndexes = append(jumpIndexes, len(instructions)-1) // Store the index for later offset update.
		}
	}

	// After evaluating all conditions, append a jump to skip the end of "any" structure if none were true.
	// This jump is not needed if we're already handling the last condition, as execution will naturally progress.
	if len(conds) > 0 {
		instructions = append(instructions, bytecode.Instruction{
			Opcode:   bytecode.OpJumpIfFalse,
			Operands: []bytecode.Operand{bytecode.JumpOffsetOperand{Offset: 0}}, // Placeholder, to be updated.
		})
		jumpIndexes = append(jumpIndexes, len(instructions)-1) // Update for the last condition's false jump.
	}

	// Update the jump offsets based on the positions now that we have the full structure.
	for _, index := range jumpIndexes {
		// Calculate the offset from this jump to the end of the "any" structure.
		offset := len(instructions) - index // Adjust this calculation based on your bytecode execution logic.
		instructions[index].Operands[0] = bytecode.JumpOffsetOperand{Offset: offset}
	}

	return instructions, len(jumpIndexes) > 0, nil
}

// getOpcodeForOperator maps a string operator to its corresponding bytecode opcode.
func getOpcodeForOperator(operator string) (bytecode.Opcode, error) {
	switch operator {
	case "equal":
		return bytecode.OpEqual, nil
	case "notEqual":
		return bytecode.OpNotEqual, nil
	case "greaterThan":
		return bytecode.OpGreaterThan, nil
	case "greaterThanOrEqual":
		return bytecode.OpGreaterThanOrEqual, nil
	case "lessThan":
		return bytecode.OpLessThan, nil
	case "lessThanOrEqual":
		return bytecode.OpLessThanOrEqual, nil
	case "contains":
		return bytecode.OpContains, nil
	case "notContains":
		return bytecode.OpNotContains, nil
	default:
		return 0, fmt.Errorf("unsupported operator: %s", operator)
	}
}

// translateActionsToBytecode translates rule actions into bytecode instructions.
func translateActionsToBytecode(actions []rule.Action) ([]bytecode.Instruction, error) {
	var instructions []bytecode.Instruction

	for _, action := range actions {
		switch action.Type {
		case "updateStore":
			instr, err := translateUpdateStoreActionToBytecode(action)
			if err != nil {
				return nil, fmt.Errorf("error translating updateStore action: %v", err)
			}
			instructions = append(instructions, instr)
		case "sendMessage":
			instr, err := translateSendMessageActionToBytecode(action)
			if err != nil {
				return nil, fmt.Errorf("error translating sendMessage action: %v", err)
			}
			instructions = append(instructions, instr)
		default:
			return nil, fmt.Errorf("unsupported action type: %s", action.Type)
		}
	}

	return instructions, nil
}

// translateUpdateStoreActionToBytecode translates an updateStore action into bytecode.
func translateUpdateStoreActionToBytecode(action rule.Action) (bytecode.Instruction, error) {
	// Assume action.Target specifies the store key, and action.Value specifies the value to set.
	return bytecode.Instruction{
		Opcode: bytecode.OpUpdateStore,
		Operands: []bytecode.Operand{
			bytecode.ValueOperand{Value: action.Target}, // Target store key
			bytecode.ValueOperand{Value: action.Value},  // Value to set
		},
	}, nil
}

// translateSendMessageActionToBytecode translates a sendMessage action into bytecode.
func translateSendMessageActionToBytecode(action rule.Action) (bytecode.Instruction, error) {
	// Assume action.Target specifies the message destination, and action.Value specifies the message content.
	return bytecode.Instruction{
		Opcode: bytecode.OpSendMessage,
		Operands: []bytecode.Operand{
			bytecode.ValueOperand{Value: action.Target}, // Message destination
			bytecode.ValueOperand{Value: action.Value},  // Message content
		},
	}, nil
}

// translateAnyConditionsToBytecode translates "Any" conditions into bytecode,
// including dynamic jump offset calculation and action translation.
// func translateAnyConditionsToBytecode(conditions []rule.Condition, actions []rule.Action) ([]bytecode.Instruction, error) {
// 	if len(conditions) == 0 {
// 		return nil, fmt.Errorf("translateAnyConditionsToBytecode: no conditions provided")
// 	}

// 	var instructions []bytecode.Instruction

// 	// For each condition, generate instructions and a conditional jump to skip the action if false.
// 	// The final jump instruction for each condition will be updated later to jump to the correct offset.
// 	for _, cond := range conditions {
// 		condInstructions, err := translateConditionToBytecode(cond)
// 		if err != nil {
// 			return nil, err
// 		}

// 		instructions = append(instructions, condInstructions...)

// 		// Add a placeholder for jump if the condition is true (jump to actions).
// 		// The actual offset is calculated after knowing the position of action instructions.
// 		instructions = append(instructions, bytecode.Instruction{
// 			Opcode:   bytecode.OpJumpIfTrue,
// 			Operands: []bytecode.Operand{bytecode.JumpOffsetOperand{Offset: 0}}, // Placeholder
// 		})
// 	}

// 	// Calculate the start index for action instructions.
// 	actionStartIndex := len(instructions)

// 	// Translate actions into bytecode.
// 	actionInstructions, err := translateActionsToBytecode(actions)
// 	if err != nil {
// 		return nil, err
// 	}
// 	instructions = append(instructions, actionInstructions...)

// 	// Update the jump offsets for OpJumpIfTrue to point to the start of the action block.
// 	for i := range conditions {
// 		jumpIfTrueIndex := actionStartIndex - (len(conditions) - i)
// 		instructions[jumpIfTrueIndex].Operands[0] = bytecode.JumpOffsetOperand{Offset: len(actionInstructions)}
// 	}

// 	// Append a final jump instruction to skip all actions if none of the "Any" conditions are true.
// 	// This is only needed if there's a possibility of reaching this point without any condition being true.
// 	if len(conditions) > 1 {
// 		instructions = append(instructions, bytecode.Instruction{
// 			Opcode:   bytecode.OpJumpIfFalse,
// 			Operands: []bytecode.Operand{bytecode.JumpOffsetOperand{Offset: len(instructions) - actionStartIndex + len(actionInstructions)}},
// 		})
// 	}

// 	return instructions, nil
// }

// // updateJumpOffsets dynamically calculates and updates offsets for jump instructions.
// func updateJumpOffsets(instructions []bytecode.Instruction) {
// 	for i, instr := range instructions {
// 		switch instr.Opcode {
// 		case bytecode.OpJumpIfTrue, bytecode.OpJumpIfFalse:
// 			// Calculate offset for the jump.
// 			offset := calculateJumpOffset(i, instructions, instr.Opcode)
// 			// Update the operand with the correct offset.
// 			instr.Operands[0] = bytecode.JumpOffsetOperand{Offset: offset}
// 			instructions[i] = instr // Ensure the updated instruction is set back in the slice.
// 		}
// 	}
// }

// // calculateJumpOffset calculates the offset for a given jump instruction.
// func calculateJumpOffset(currentIndex int, instructions []bytecode.Instruction, opcode bytecode.Opcode) int {
// 	// For OpJumpIfTrue, find the nearest action start or next condition.
// 	// For OpJumpIfFalse, find the end of the condition block or action block.
// 	if opcode == bytecode.OpJumpIfTrue {
// 		return findNextActionOrCondition(currentIndex, instructions)
// 	} else if opcode == bytecode.OpJumpIfFalse {
// 		return findEndOfBlockOrAction(currentIndex, instructions)
// 	}
// 	return 0 // Default case, should not occur with proper usage.
// }

// // findNextActionOrCondition finds the offset to the next action or condition from the current index.
// func findNextActionOrCondition(currentIndex int, instructions []bytecode.Instruction) int {
// 	// Iterate over instructions to find the start of the action block or next condition.
// 	for i := currentIndex + 1; i < len(instructions); i++ {
// 		switch instructions[i].Opcode {
// 		case bytecode.OpUpdateStore, bytecode.OpSendMessage:
// 			// Found the start of the action block.
// 			return i - currentIndex
// 			// Add cases for condition opcodes if there's a specific need to jump to another condition.
// 		}
// 	}
// 	return len(instructions) - currentIndex // Default to the end if no specific target is found.
// }

// // findEndOfBlockOrAction finds the offset to the end of a block or action from the current index.
// func findEndOfBlockOrAction(currentIndex int, instructions []bytecode.Instruction) int {
// 	// Assuming actions mark the end of a block, find the end of the action block.
// 	// This function can be tailored based on specific needs and structure of the bytecode.
// 	for i := currentIndex + 1; i < len(instructions); i++ {
// 		if isEndOfBlockInstruction(instructions[i]) {
// 			// Found the end of a block or action.
// 			return i - currentIndex
// 		}
// 	}
// 	return len(instructions) - currentIndex // Default to the end if no specific end marker is found.
// }

// // isEndOfBlockInstruction determines if an instruction marks the end of a block.
// func isEndOfBlockInstruction(instr bytecode.Instruction) bool {
// 	// Define logic to identify if an instruction marks the end of a block.
// 	// This could depend on the specific opcodes marking the end of condition evaluations or action blocks.
// 	// Placeholder for actual logic.
// 	return false
// }
