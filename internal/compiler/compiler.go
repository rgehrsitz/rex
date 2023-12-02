package compiler

import (
	"fmt"
	"rgehrsitz/rex/pkg/bytecode"
	"rgehrsitz/rex/pkg/rule"
)

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

		// Add a jump instruction after each condition except the last one
		if i < len(conditions)-1 {
			jumpPlaceholder := len(instructions)
			jumpPlaceholders = append(jumpPlaceholders, jumpPlaceholder)
			// Append placeholder jump, actual destination set later
			instructions = append(instructions, bytecode.Instruction{Opcode: bytecode.OpJumpIfTrue, Operands: []interface{}{0}})
		}
	}

	// Correctly set jump destinations
	endOfAnyBlock := len(instructions) + 1 // Adjusted to account for the jump instruction itself
	for _, placeholder := range jumpPlaceholders {
		instructions[placeholder].Operands[0] = endOfAnyBlock
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
