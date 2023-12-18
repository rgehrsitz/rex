package compiler

import (
	"fmt"
	"rgehrsitz/rex/internal/bytecode"
	"rgehrsitz/rex/internal/rule"
)

type CompiledRule struct {
	Instructions []bytecode.Instruction
	Dependencies []string // Names of dependent rules
}

func CompileRule(r rule.Rule) ([]bytecode.Instruction, error) {
	var instructions []bytecode.Instruction

	// Compile 'All' conditions
	for _, cond := range r.Conditions.All {
		compiled, err := compileCondition(cond)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, compiled...)
	}

	// Compile 'Any' conditions
	if len(r.Conditions.Any) > 0 {
		anyInstructions, err := compileAnyConditions(r.Conditions.Any)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, anyInstructions...)
	}

	// Compile action
	if r.Event.Action.Type != "" {
		actionInstructions, err := compileAction(r.Event.Action)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, actionInstructions...)
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
	case int, float64, string:
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

	// Compile all rules
	for i, r := range rules {
		instructions, err := CompileRule(r)
		if err != nil {
			return nil, err
		}
		compiledRules[i] = CompiledRule{
			Instructions: instructions,
			Dependencies: []string{}, // Initialize with empty slice
		}
	}

	// Analyze dependencies
	for i, r := range rules {
		writesFacts := getWritesFacts(r)
		for j, otherRule := range rules {
			if i != j && isDependent(writesFacts, otherRule) {
				compiledRules[i].Dependencies = append(compiledRules[i].Dependencies, otherRule.Name)
			}
		}
	}

	return compiledRules, nil
}

func isDependent(writesFacts []string, r rule.Rule) bool {
	readsFacts := getReadsFacts(r)
	for _, wf := range writesFacts {
		for _, rf := range readsFacts {
			if wf == rf {
				return true
			}
		}
	}
	return false
}

func getWritesFacts(r rule.Rule) []string {
	var writes []string
	if r.Event.Action.Type == "updateStore" {
		writes = append(writes, r.Event.Action.Target)
	}
	return writes
}

func getReadsFacts(r rule.Rule) []string {
	return getFactsFromConditions(r.Conditions)
}

func getFactsFromConditions(conditions rule.Conditions) []string {
	var facts []string
	for _, cond := range conditions.All {
		facts = append(facts, cond.Fact)
		facts = append(facts, getFactsFromConditions(rule.Conditions{All: cond.All, Any: cond.Any})...)
	}
	for _, cond := range conditions.Any {
		facts = append(facts, cond.Fact)
		facts = append(facts, getFactsFromConditions(rule.Conditions{All: cond.All, Any: cond.Any})...)
	}
	return facts
}

func compileAction(action rule.Action) ([]bytecode.Instruction, error) {
	var instructions []bytecode.Instruction

	switch action.Type {
	case "updateStore":
		// Compile update store action into bytecode
		updateInstruction := bytecode.Instruction{
			Opcode:   bytecode.OpUpdateStore,
			Operands: []interface{}{action.Target, action.Value},
		}
		instructions = append(instructions, updateInstruction)

	case "sendMessage":
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
