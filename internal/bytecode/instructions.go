package bytecode

import (
	"fmt"
)

// Opcode represents the bytecode instruction set.
type Opcode byte

const (
	OpLoadFact Opcode = iota
	OpEqual
	OpNotEqual
	OpGreaterThan
	OpGreaterThanOrEqual
	OpLessThan
	OpLessThanOrEqual
	OpJumpIfTrue
	OpJumpIfFalse
	OpContains
	OpNotContains
	OpTriggerEvent
	OpUpdateStore
	OpSendMessage
	OpEqualAny
	// Add additional opcodes as needed.
)

const (
	OpPushTrue   Opcode = iota + 100 // Push a true value onto the stack
	OpPushFalse                      // Push a false value onto the stack
	OpLogicalAnd                     // Apply logical AND to the top two stack items
	OpLogicalOr                      // Apply logical OR to the top two stack items
)

// Operand is the interface for instruction operands, allowing for different operand types.
type Operand interface {
	OperandType() string
}

// FactOperand represents an operand that is a fact name.
type FactOperand struct {
	FactName string
}

func (f FactOperand) OperandType() string { return "Fact" }

// ValueOperand represents an operand that is a constant value.
type ValueOperand struct {
	Value interface{}
}

func (v ValueOperand) OperandType() string { return "Value" }

// JumpOffsetOperand represents an operand that is a jump offset for conditional jumps.
type JumpOffsetOperand struct {
	Offset int
}

func (j JumpOffsetOperand) OperandType() string { return "JumpOffset" }

// Instruction represents a single bytecode instruction, consisting of an opcode and operands.
type Instruction struct {
	Opcode   Opcode
	Operands []Operand
}

// ExecutionContext maintains the state during rule evaluation, including loaded facts and intermediate values.
type ExecutionContext struct {
	Facts map[string]interface{}
	Stack []interface{}
}

func NewExecutionContext() *ExecutionContext {
	return &ExecutionContext{
		Facts: make(map[string]interface{}),
		Stack: make([]interface{}, 0),
	}
}

// LoadFact loads a fact value into the execution context.
func (ctx *ExecutionContext) LoadFact(factName string) {
	// Example: Load fact value from context's fact map.
	value, exists := ctx.Facts[factName]
	if exists {
		ctx.Stack = append(ctx.Stack, value)
	} else {
		// Handle fact not found scenario.
		ctx.Stack = append(ctx.Stack, nil)
	}
}

// ExecuteInstruction executes a single bytecode instruction within the given execution context.
func ExecuteInstruction(instruction Instruction, ctx *ExecutionContext) error {
	switch instruction.Opcode {
	case OpLoadFact:
		factOperand := instruction.Operands[0].(FactOperand)
		ctx.LoadFact(factOperand.FactName)
		// Implement other opcodes...
	default:
		return fmt.Errorf("unknown opcode: %v", instruction.Opcode)
	}
	return nil
}

// ExecuteInstructions executes a slice of instructions in the given execution context.
func ExecuteInstructions(instructions []Instruction, ctx *ExecutionContext) error {
	for _, instr := range instructions {
		if err := ExecuteInstruction(instr, ctx); err != nil {
			return err
		}
	}
	return nil
}
