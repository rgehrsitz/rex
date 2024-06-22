// rex/pkg/compiler/bytecode.go

package compiler

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"

	"rgehrsitz/rex/pkg/logging"
)

const (
	Version       = 1
	Checksum      = 0
	ConstPoolSize = 0
	HeaderSize    = 28
)

// Opcode represents the type of a bytecode instruction.
type Opcode byte

// Bytecode instructions
const (
	// Comparison instructions
	EQ_FLOAT Opcode = iota
	NEQ_FLOAT
	LT_FLOAT
	LTE_FLOAT
	GT_FLOAT
	GTE_FLOAT
	EQ_STRING
	NEQ_STRING
	CONTAINS_STRING
	NOT_CONTAINS_STRING
	EQ_BOOL
	NEQ_BOOL

	// Logical instructions
	AND
	OR
	NOT

	// Fact instructions
	LOAD_FACT_FLOAT
	LOAD_FACT_STRING
	LOAD_FACT_BOOL
	STORE_FACT

	// Value instructions
	LOAD_CONST_FLOAT
	LOAD_CONST_STRING
	LOAD_CONST_BOOL
	LOAD_VAR

	// Control flow instructions
	JUMP
	JUMP_IF_TRUE
	JUMP_IF_FALSE

	// Action instructions
	TRIGGER_ACTION
	UPDATE_FACT
	SEND_MESSAGE

	// Miscellaneous instructions
	NOP
	HALT
	ERROR

	// Optimization instructions
	INC
	DEC
	COMPARE_AND_JUMP

	// Label instruction
	LABEL

	RULE_START
	RULE_END

	COND_START
	COND_END

	// Action instructions
	ACTION_START
	ACTION_END
	ACTION_TYPE
	ACTION_TARGET
	ACTION_VALUE_FLOAT
	ACTION_VALUE_STRING
	ACTION_VALUE_BOOL
	ACTION_VALUE_ARRAY
	ACTION_VALUE_OBJECT
	ACTION_COMMAND

	HEADER_START
	HEADER_END
	CHECKSUM
	VERSION
	NUM_RULES
	CONST_POOL_SIZE
)

// hasOperands returns true if the opcode requires operands.
func (op Opcode) HasOperands() bool {
	switch op {
	case LOAD_CONST_FLOAT, LOAD_CONST_STRING, LOAD_CONST_BOOL,
		LOAD_FACT_FLOAT, LOAD_FACT_STRING, LOAD_FACT_BOOL,
		JUMP, JUMP_IF_TRUE, JUMP_IF_FALSE, LABEL,
		SEND_MESSAGE, TRIGGER_ACTION, UPDATE_FACT,
		ACTION_START, RULE_START:
		return true
	default:
		return false
	}
}

// String returns a human-readable representation of an instruction.
func (instr Instruction) String() string {
	opcodeName := instr.Opcode.String()
	operands := ""
	if instr.Opcode.HasOperands() {
		operands = fmt.Sprintf(" %s", formatOperands(instr.Operands))
	}
	return fmt.Sprintf("%s%s", opcodeName, operands)
}

// String returns the string representation of an opcode.
func (op Opcode) String() string {
	names := [...]string{
		"EQ_FLOAT", "NEQ_FLOAT", "LT_FLOAT", "LTE_FLOAT", "GT_FLOAT", "GTE_FLOAT",
		"EQ_STRING", "NEQ_STRING", "CONTAINS_STRING", "NOT_CONTAINS_STRING",
		"EQ_BOOL", "NEQ_BOOL",
		"AND", "OR", "NOT",
		"LOAD_FACT_FLOAT", "LOAD_FACT_STRING", "LOAD_FACT_BOOL", "STORE_FACT",
		"LOAD_CONST_FLOAT", "LOAD_CONST_STRING", "LOAD_CONST_BOOL", "LOAD_VAR",
		"JUMP", "JUMP_IF_TRUE", "JUMP_IF_FALSE",
		"TRIGGER_ACTION", "UPDATE_FACT", "SEND_MESSAGE",
		"NOP", "HALT", "ERROR",
		"INC", "DEC", "COMPARE_AND_JUMP",
		"LABEL",
		"RULE_START", "RULE_END",
		"COND_START", "COND_END",
		"ACTION_START", "ACTION_END",
		"ACTION_TYPE", "ACTION_TARGET", "ACTION_VALUE_FLOAT", "ACTION_VALUE_STRING", "ACTION_VALUE_BOOL", "ACTION_VALUE_ARRAY", "ACTION_VALUE_OBJECT", "ACTION_COMMAND",
		"HEADER_START", "HEADER_END", "CHECKSUM", "VERSION", "NUM_RULES", "CONST_POOL_SIZE",
	}
	if op < EQ_FLOAT || op >= Opcode(len(names)) {
		logging.Logger.Warn().Uint8("opcode", uint8(op)).Msg("Unknown opcode")
		return fmt.Sprintf("Opcode(%d)", op)
	}
	return names[op]
}

// formatOperands returns a formatted string of the operands.
func formatOperands(operands []byte) string {
	var sb strings.Builder
	for _, b := range operands {
		if b >= ' ' && b <= '~' {
			sb.WriteByte(b)
		} else {
			sb.WriteString(fmt.Sprintf("\\x%02x", b))
		}
	}
	return sb.String()
}

type BytecodeFile struct {
	Header              Header
	Instructions        []byte
	RuleExecIndex       []RuleExecutionIndex
	FactRuleLookupIndex map[string][]string
	FactDependencyIndex []FactDependencyIndex
}

func WriteBytecodeToFile(filename string, bytecodeFile BytecodeFile) error {
	buf := new(bytes.Buffer)

	// Write header
	if err := binary.Write(buf, binary.LittleEndian, bytecodeFile.Header.Version); err != nil {
		return err
	}
	if err := binary.Write(buf, binary.LittleEndian, bytecodeFile.Header.Checksum); err != nil {
		return err
	}
	if err := binary.Write(buf, binary.LittleEndian, bytecodeFile.Header.ConstPoolSize); err != nil {
		return err
	}
	if err := binary.Write(buf, binary.LittleEndian, bytecodeFile.Header.NumRules); err != nil {
		return err
	}
	if err := binary.Write(buf, binary.LittleEndian, bytecodeFile.Header.RuleExecIndexOffset); err != nil {
		return err
	}
	if err := binary.Write(buf, binary.LittleEndian, bytecodeFile.Header.FactRuleIndexOffset); err != nil {
		return err
	}
	if err := binary.Write(buf, binary.LittleEndian, bytecodeFile.Header.FactDepIndexOffset); err != nil {
		return err
	}

	// Write bytecode instructions
	if _, err := buf.Write(bytecodeFile.Instructions); err != nil {
		return err
	}

	// Calculate and write the Rule Execution Index
	bytecodeFile.Header.RuleExecIndexOffset = uint32(buf.Len())
	for _, idx := range bytecodeFile.RuleExecIndex {
		if err := writeString(buf, idx.RuleName); err != nil {
			return err
		}
		if err := binary.Write(buf, binary.LittleEndian, uint32(idx.ByteOffset)); err != nil {
			return err
		}
	}

	// Calculate and write the Fact Rule Lookup Index
	bytecodeFile.Header.FactRuleIndexOffset = uint32(buf.Len())
	for factName, rules := range bytecodeFile.FactRuleLookupIndex {
		if err := writeString(buf, factName); err != nil {
			return err
		}
		rulesCount := uint32(len(rules))
		if err := binary.Write(buf, binary.LittleEndian, rulesCount); err != nil {
			return err
		}
		for _, ruleName := range rules {
			if err := writeString(buf, ruleName); err != nil {
				return err
			}
		}
	}

	// Calculate and write the Fact Dependency Index
	bytecodeFile.Header.FactDepIndexOffset = uint32(buf.Len())
	for _, idx := range bytecodeFile.FactDependencyIndex {
		if err := writeString(buf, idx.RuleName); err != nil {
			return err
		}
		factsCount := uint32(len(idx.Facts))
		if err := binary.Write(buf, binary.LittleEndian, factsCount); err != nil {
			return err
		}
		for _, factName := range idx.Facts {
			if err := writeString(buf, factName); err != nil {
				return err
			}
		}
	}

	// Write the updated header with correct index offsets
	headerBytes := new(bytes.Buffer)
	if err := binary.Write(headerBytes, binary.LittleEndian, bytecodeFile.Header.Version); err != nil {
		return err
	}
	if err := binary.Write(headerBytes, binary.LittleEndian, bytecodeFile.Header.Checksum); err != nil {
		return err
	}
	if err := binary.Write(headerBytes, binary.LittleEndian, bytecodeFile.Header.ConstPoolSize); err != nil {
		return err
	}
	if err := binary.Write(headerBytes, binary.LittleEndian, bytecodeFile.Header.NumRules); err != nil {
		return err
	}
	if err := binary.Write(headerBytes, binary.LittleEndian, bytecodeFile.Header.RuleExecIndexOffset); err != nil {
		return err
	}
	if err := binary.Write(headerBytes, binary.LittleEndian, bytecodeFile.Header.FactRuleIndexOffset); err != nil {
		return err
	}
	if err := binary.Write(headerBytes, binary.LittleEndian, bytecodeFile.Header.FactDepIndexOffset); err != nil {
		return err
	}

	// Update the header in the buffer
	copy(buf.Bytes()[:HeaderSize], headerBytes.Bytes())

	// Write buffer to file
	if err := os.WriteFile(filename, buf.Bytes(), 0644); err != nil {
		return err
	}

	logging.Logger.Info().Msgf("Successfully wrote bytecode file: %s", filename)
	return nil
}

func writeString(buf *bytes.Buffer, s string) error {
	length := uint32(len(s))
	if err := binary.Write(buf, binary.LittleEndian, length); err != nil {
		logging.Logger.Error().Err(err).Msg("Error writing string length")
		return err
	}
	if _, err := buf.WriteString(s); err != nil {
		logging.Logger.Error().Err(err).Msg("Error writing string")
		return err
	}
	logging.Logger.Debug().Str("string", s).Uint32("length", length).Msg("Writing string")
	return nil
}
