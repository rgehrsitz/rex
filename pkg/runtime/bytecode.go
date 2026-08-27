package runtime

import (
	"encoding/binary"
	"fmt"

	"rgehrsitz/rex/pkg/compiler"
)

type bytecodeHeader struct {
	version             uint32
	checksum            uint32
	constPoolSize       uint32
	numRules            uint32
	ruleExecIndexOffset uint32
	factRuleIndexOffset uint32
	factDepIndexOffset  uint32
}

// validateBytecode verifies that a compiled artifact can be decoded safely by
// the runtime before any execution or unchecked slice access occurs.
func validateBytecode(data []byte) error {
	header, err := readBytecodeHeader(data)
	if err != nil {
		return err
	}

	if header.version != compiler.Version {
		return fmt.Errorf("unsupported bytecode version %d", header.version)
	}
	if header.constPoolSize != compiler.ConstPoolSize {
		return fmt.Errorf("unsupported constant pool size %d", header.constPoolSize)
	}

	ruleExecOffset := int(header.ruleExecIndexOffset)
	factRuleOffset := int(header.factRuleIndexOffset)
	factDepOffset := int(header.factDepIndexOffset)
	if ruleExecOffset < compiler.HeaderSize || ruleExecOffset > len(data) ||
		factRuleOffset < ruleExecOffset || factRuleOffset > len(data) ||
		factDepOffset < factRuleOffset || factDepOffset > len(data) {
		return fmt.Errorf("invalid bytecode section offsets")
	}

	ruleStarts, err := validateInstructions(data[compiler.HeaderSize:ruleExecOffset])
	if err != nil {
		return err
	}
	if len(ruleStarts) != int(header.numRules) {
		return fmt.Errorf("header declares %d rules, found %d rule starts", header.numRules, len(ruleStarts))
	}

	ruleNames, err := validateRuleExecutionIndex(data[ruleExecOffset:factRuleOffset], header.numRules, ruleStarts)
	if err != nil {
		return err
	}
	if err := validateFactRuleIndex(data[factRuleOffset:factDepOffset], ruleNames); err != nil {
		return err
	}
	if err := validateFactDependencyIndex(data[factDepOffset:], ruleNames); err != nil {
		return err
	}

	return nil
}

func readBytecodeHeader(data []byte) (bytecodeHeader, error) {
	if len(data) < compiler.HeaderSize {
		return bytecodeHeader{}, fmt.Errorf("bytecode file is too short for the header")
	}

	return bytecodeHeader{
		version:             binary.LittleEndian.Uint32(data[0:4]),
		checksum:            binary.LittleEndian.Uint32(data[4:8]),
		constPoolSize:       binary.LittleEndian.Uint32(data[8:12]),
		numRules:            binary.LittleEndian.Uint32(data[12:16]),
		ruleExecIndexOffset: binary.LittleEndian.Uint32(data[16:20]),
		factRuleIndexOffset: binary.LittleEndian.Uint32(data[20:24]),
		factDepIndexOffset:  binary.LittleEndian.Uint32(data[24:28]),
	}, nil
}

func validateInstructions(data []byte) (map[int]string, error) {
	ruleStarts := make(map[int]string)
	for offset := 0; offset < len(data); {
		start := offset
		opcode := compiler.Opcode(data[offset])
		offset++

		var err error
		switch opcode {
		case compiler.RULE_START:
			var name string
			name, offset, err = readByteString(data, offset)
			if err == nil {
				if _, exists := ruleStarts[start]; exists {
					err = fmt.Errorf("duplicate rule start at instruction offset %d", start)
				} else {
					ruleStarts[start] = name
				}
			}

		case compiler.PRIORITY, compiler.JUMP_IF_TRUE, compiler.JUMP_IF_FALSE, compiler.LABEL:
			offset, err = consumeFixed(data, offset, 4)

		case compiler.LOAD_CONST_FLOAT, compiler.ACTION_VALUE_FLOAT:
			offset, err = consumeFixed(data, offset, 8)

		case compiler.LOAD_CONST_BOOL, compiler.ACTION_VALUE_BOOL:
			offset, err = consumeFixed(data, offset, 1)

		case compiler.LOAD_FACT_FLOAT, compiler.LOAD_FACT_STRING, compiler.LOAD_FACT_BOOL,
			compiler.LOAD_CONST_STRING, compiler.ACTION_TYPE, compiler.ACTION_TARGET, compiler.ACTION_VALUE_STRING:
			_, offset, err = readByteString(data, offset)

		case compiler.SCRIPT_DEF:
			offset, err = validateScriptDefinition(data, offset)

		case compiler.SCRIPT_CALL:
			offset, err = validateScriptCall(data, offset)

		case compiler.EQ_FLOAT, compiler.NEQ_FLOAT, compiler.LT_FLOAT, compiler.LTE_FLOAT,
			compiler.GT_FLOAT, compiler.GTE_FLOAT, compiler.EQ_STRING, compiler.NEQ_STRING,
			compiler.CONTAINS_STRING, compiler.NOT_CONTAINS_STRING, compiler.EQ_BOOL,
			compiler.NEQ_BOOL, compiler.ACTION_START, compiler.ACTION_END, compiler.RULE_END:
			// These instructions do not have operands.

		default:
			return nil, fmt.Errorf("unsupported opcode %d at instruction offset %d", opcode, start)
		}
		if err != nil {
			return nil, fmt.Errorf("invalid opcode %d at instruction offset %d: %w", opcode, start, err)
		}
	}

	return ruleStarts, nil
}

func validateScriptDefinition(data []byte, offset int) (int, error) {
	_, offset, err := readByteString(data, offset)
	if err != nil {
		return offset, err
	}
	if offset >= len(data) {
		return offset, fmt.Errorf("missing script parameter count")
	}
	params := int(data[offset])
	offset++
	for i := 0; i < params; i++ {
		_, offset, err = readByteString(data, offset)
		if err != nil {
			return offset, err
		}
	}
	_, offset, err = readByteString(data, offset)
	return offset, err
}

func validateScriptCall(data []byte, offset int) (int, error) {
	_, offset, err := readByteString(data, offset)
	if err != nil {
		return offset, err
	}
	if offset >= len(data) {
		return offset, fmt.Errorf("missing script argument count")
	}
	params := int(data[offset])
	offset++
	for i := 0; i < params; i++ {
		_, offset, err = readByteString(data, offset)
		if err != nil {
			return offset, err
		}
	}
	return offset, nil
}

func validateRuleExecutionIndex(data []byte, count uint32, ruleStarts map[int]string) (map[string]struct{}, error) {
	offset := 0
	ruleNames := make(map[string]struct{}, count)
	for i := uint32(0); i < count; i++ {
		name, nextOffset, err := readIndexString(data, offset)
		if err != nil {
			return nil, fmt.Errorf("invalid rule execution index entry %d: %w", i, err)
		}
		offset = nextOffset
		if offset+4 > len(data) {
			return nil, fmt.Errorf("truncated rule execution index offset for %q", name)
		}
		instructionOffset := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		offset += 4

		actualName, ok := ruleStarts[instructionOffset]
		if !ok || actualName != name {
			return nil, fmt.Errorf("rule execution index for %q points to invalid instruction offset %d", name, instructionOffset)
		}
		if _, exists := ruleNames[name]; exists {
			return nil, fmt.Errorf("duplicate rule execution index entry for %q", name)
		}
		ruleNames[name] = struct{}{}
	}
	if offset != len(data) {
		return nil, fmt.Errorf("rule execution index has trailing bytes")
	}
	return ruleNames, nil
}

func validateFactRuleIndex(data []byte, ruleNames map[string]struct{}) error {
	for offset := 0; offset < len(data); {
		fact, nextOffset, err := readIndexString(data, offset)
		if err != nil {
			return fmt.Errorf("invalid fact rule index fact: %w", err)
		}
		offset = nextOffset
		if offset+4 > len(data) {
			return fmt.Errorf("truncated rule count for fact %q", fact)
		}
		count := binary.LittleEndian.Uint32(data[offset : offset+4])
		offset += 4
		for i := uint32(0); i < count; i++ {
			ruleName, nextOffset, err := readIndexString(data, offset)
			if err != nil {
				return fmt.Errorf("invalid rule reference for fact %q: %w", fact, err)
			}
			offset = nextOffset
			if _, ok := ruleNames[ruleName]; !ok {
				return fmt.Errorf("fact %q references unknown rule %q", fact, ruleName)
			}
		}
	}
	return nil
}

func validateFactDependencyIndex(data []byte, ruleNames map[string]struct{}) error {
	for offset := 0; offset < len(data); {
		ruleName, nextOffset, err := readIndexString(data, offset)
		if err != nil {
			return fmt.Errorf("invalid fact dependency index rule: %w", err)
		}
		offset = nextOffset
		if _, ok := ruleNames[ruleName]; !ok {
			return fmt.Errorf("fact dependency index references unknown rule %q", ruleName)
		}
		if offset+4 > len(data) {
			return fmt.Errorf("truncated fact count for rule %q", ruleName)
		}
		count := binary.LittleEndian.Uint32(data[offset : offset+4])
		offset += 4
		for i := uint32(0); i < count; i++ {
			_, nextOffset, err := readIndexString(data, offset)
			if err != nil {
				return fmt.Errorf("invalid fact dependency for rule %q: %w", ruleName, err)
			}
			offset = nextOffset
		}
	}
	return nil
}

func readByteString(data []byte, offset int) (string, int, error) {
	if offset >= len(data) {
		return "", offset, fmt.Errorf("missing byte string length")
	}
	length := int(data[offset])
	offset++
	if length > len(data)-offset {
		return "", offset, fmt.Errorf("byte string length %d exceeds remaining data", length)
	}
	return string(data[offset : offset+length]), offset + length, nil
}

func readIndexString(data []byte, offset int) (string, int, error) {
	if offset+4 > len(data) {
		return "", offset, fmt.Errorf("missing index string length")
	}
	length := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4
	if uint64(length) > uint64(len(data)-offset) {
		return "", offset, fmt.Errorf("index string length %d exceeds remaining data", length)
	}
	end := offset + int(length)
	return string(data[offset:end]), end, nil
}

func consumeFixed(data []byte, offset, length int) (int, error) {
	if length > len(data)-offset {
		return offset, fmt.Errorf("requires %d bytes, only %d remain", length, len(data)-offset)
	}
	return offset + length, nil
}
