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
	OpEqualAny

	// New opcodes for actions
	OpUpdateStore // Opcode for updating a value in the store
	OpSendMessage // Opcode for sending a message

)

// Instruction represents a single bytecode instruction.
type Instruction struct {
	Opcode   Opcode
	Operands []interface{} // Operands can include facts, values, targets, etc.
}
