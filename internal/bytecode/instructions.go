package bytecode

// Opcode represents the bytecode instruction.
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
	// Add any other opcodes as needed
)

// Instruction represents a single bytecode instruction.
type Instruction struct {
	Opcode   Opcode
	Operands []interface{}
}
