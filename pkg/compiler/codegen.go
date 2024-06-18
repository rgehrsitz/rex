// rex/pkg/compiler/codegen.go

package compiler

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// Instruction represents a bytecode instruction
type Instruction struct {
	Opcode   Opcode
	Operands []byte
}

// Size returns the size of the instruction in bytes, including its operands.
func (instr *Instruction) Size() int {
	return 1 + len(instr.Operands) // 1 byte for the opcode + length of operands
}

// ReplaceLabels replaces labels with the corresponding byte offsets.
func ReplaceLabels(instructions []Instruction, offsets map[string]int, labelPositions map[string]int) []Instruction {
	log.Info().Msg("Replacing labels")
	finalInstructions := []Instruction{}

	for i, instr := range instructions {
		switch instr.Opcode {
		case JUMP_IF_FALSE, JUMP_IF_TRUE:
			parts := strings.Split(string(instr.Operands), " ")
			label := parts[3]
			if _, ok := labelPositions[label]; ok {
				// Calculate the relative offset from the current instruction to the target instruction
				offset := offsets[fmt.Sprintf("%v %v", instr.Opcode, instr.Operands)] - (offsets[fmt.Sprintf("%v %v", instr.Opcode, instr.Operands)] + instr.Size())
				// Convert the offset to a uint32 and overwrite the label bytes
				offsetBytes := make([]byte, 4)
				binary.LittleEndian.PutUint32(offsetBytes, uint32(offset))
				copy(instr.Operands[len(instr.Operands)-4:], offsetBytes)
				log.Info().Msgf("Replaced label %s with offset %d in instruction %d", label, offset, i)
			}
		}
		finalInstructions = append(finalInstructions, instr)
	}

	// Log final instructions with their offsets
	for i, instr := range finalInstructions {
		position := offsets[fmt.Sprintf("%v %v", instr.Opcode, instr.Operands)]
		log.Info().Msgf("Final Instruction %d: Opcode %v, Operands %v, Position %d", i, instr.Opcode, instr.Operands, position)
	}

	return finalInstructions
}

// RemoveLabels removes any remaining label instructions.
func RemoveLabels(instructions []Instruction) []Instruction {
	finalInstructions := []Instruction{}
	for _, instr := range instructions {
		if instr.Opcode != LABEL {
			finalInstructions = append(finalInstructions, instr)
		}
	}
	return finalInstructions
}

// GenerateBytecode generates the bytecode instructions from the ruleset.
func GenerateBytecode(ruleset *Ruleset) BytecodeFile {
	var bytecode []byte

	for _, rule := range ruleset.Rules {
		log.Debug().Msgf("Processing rule: %s", rule.Name)
		ruleBytecode := []byte{byte(RULE_START)}

		// Append the rule name as an operand
		ruleBytecode = append(ruleBytecode, byte(len(rule.Name)))
		ruleBytecode = append(ruleBytecode, []byte(rule.Name)...)

		// Convert the conditions to a Node structure
		conditionNode := convertConditionGroupToNode(rule.Conditions)

		// Generate instructions from the condition tree
		instructions := generateInstructions(conditionNode, "L")

		// Optimize the generated instructions
		instructions = OptimizeInstructions(instructions)
		instructions = CombineJIFJIT(instructions)
		instructions = RemoveUnusedLabels(instructions)

		// Append the optimized instructions to the rule's bytecode
		for _, instr := range instructions {
			// Handle JUMP_IF_FALSE and JUMP_IF_TRUE with comparison operations separately
			if instr.Opcode == JUMP_IF_FALSE || instr.Opcode == JUMP_IF_TRUE {
				condition := string(instr.Operands)
				parts := strings.Split(condition, " ")
				if len(parts) == 4 {
					fact := parts[0]
					operator := parts[1]
					value := parts[2]
					label := parts[3]

					log.Debug().Msgf("Processing condition: fact=%s, operator=%s, value=%s, label=%s", fact, operator, value, label)

					// Convert operator and value into appropriate opcodes and operands
					var valueOpcode Opcode
					var factOpcode Opcode
					var valueBytes []byte
					var comparisonOpcode Opcode

					if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
						factOpcode = LOAD_FACT_FLOAT
						valueOpcode = LOAD_CONST_FLOAT
						valueBytes = floatToBytes(floatValue)
					} else if boolValue, err := strconv.ParseBool(value); err == nil {
						factOpcode = LOAD_FACT_BOOL
						valueOpcode = LOAD_CONST_BOOL
						valueBytes = boolToBytes(boolValue)
					} else {
						factOpcode = LOAD_FACT_STRING
						valueOpcode = LOAD_CONST_STRING
						valueBytes = []byte(value)
					}

					switch operator {
					case "GT":
						switch factOpcode {
						case LOAD_FACT_FLOAT:
							comparisonOpcode = GT_FLOAT
						}
					case "EQ":
						switch factOpcode {
						case LOAD_FACT_FLOAT:
							comparisonOpcode = EQ_FLOAT
						case LOAD_FACT_STRING:
							comparisonOpcode = EQ_STRING
						case LOAD_FACT_BOOL:
							comparisonOpcode = EQ_BOOL
						}
					case "NEQ":
						switch factOpcode {
						case LOAD_FACT_FLOAT:
							comparisonOpcode = NEQ_FLOAT
						case LOAD_FACT_STRING:
							comparisonOpcode = NEQ_STRING
						case LOAD_FACT_BOOL:
							comparisonOpcode = NEQ_BOOL
						}
					case "LT":
						switch factOpcode {
						case LOAD_FACT_FLOAT:
							comparisonOpcode = LT_FLOAT
						}
					case "LTE":
						switch factOpcode {
						case LOAD_FACT_FLOAT:
							comparisonOpcode = LTE_FLOAT
						}
					case "GTE":
						switch factOpcode {
						case LOAD_FACT_FLOAT:
							comparisonOpcode = GTE_FLOAT
						}
					case "CONTAINS":
						if factOpcode == LOAD_FACT_STRING {
							comparisonOpcode = CONTAINS_STRING
						}
					case "NOT_CONTAINS":
						if factOpcode == LOAD_FACT_STRING {
							comparisonOpcode = NOT_CONTAINS_STRING
						}
					}

					// Append the separated instructions
					ruleBytecode = append(ruleBytecode, byte(factOpcode))
					ruleBytecode = append(ruleBytecode, byte(len(fact)))
					ruleBytecode = append(ruleBytecode, []byte(fact)...)

					ruleBytecode = append(ruleBytecode, byte(valueOpcode))
					if valueOpcode == LOAD_CONST_STRING {
						ruleBytecode = append(ruleBytecode, byte(len(value)))
					}
					ruleBytecode = append(ruleBytecode, valueBytes...)

					ruleBytecode = append(ruleBytecode, byte(comparisonOpcode))

					ruleBytecode = append(ruleBytecode, byte(instr.Opcode))

					ruleBytecode = append(ruleBytecode, []byte(label)...)

					log.Debug().Msgf("Appended separated instructions for condition: factOpcode=%v, valueOpcode=%v, comparisonOpcode=%v", factOpcode, valueOpcode, comparisonOpcode)
					continue
				}
			}

			// Append the instruction as usual
			ruleBytecode = append(ruleBytecode, byte(instr.Opcode))
			ruleBytecode = append(ruleBytecode, instr.Operands...)
			log.Debug().Msgf("Appended instruction: Opcode=%v, Operands=%v", instr.Opcode, instr.Operands)
		}

		// Generate bytecode for actions
		actionBytecode := []byte{}
		for _, action := range rule.Actions {
			log.Debug().Msgf("Processing action: %s", action.Type)
			actionBytecode = append(actionBytecode, byte(ACTION_START))

			// Append the action type
			actionBytecode = append(actionBytecode, byte(ACTION_TYPE))
			actionBytecode = append(actionBytecode, byte(len(action.Type)))
			actionBytecode = append(actionBytecode, []byte(action.Type)...)

			// Append the action target
			actionBytecode = append(actionBytecode, byte(ACTION_TARGET))
			actionBytecode = append(actionBytecode, byte(len(action.Target)))
			actionBytecode = append(actionBytecode, []byte(action.Target)...)

			// Append the action value based on its type
			switch v := action.Value.(type) {
			case float64:
				actionBytecode = append(actionBytecode, byte(ACTION_VALUE_FLOAT))
				floatBytes := make([]byte, 8)
				binary.Write(bytes.NewBuffer(floatBytes), binary.LittleEndian, v)
				actionBytecode = append(actionBytecode, floatBytes...)
			case string:
				actionBytecode = append(actionBytecode, byte(ACTION_VALUE_STRING))
				actionBytecode = append(actionBytecode, byte(len(v)))
				actionBytecode = append(actionBytecode, []byte(v)...)
			case bool:
				actionBytecode = append(actionBytecode, byte(ACTION_VALUE_BOOL))
				if v {
					actionBytecode = append(actionBytecode, byte(1))
				} else {
					actionBytecode = append(actionBytecode, byte(0))
				}
			default:
				log.Error().Msgf("Unsupported action value type: %T", v)
				continue
			}

			actionBytecode = append(actionBytecode, byte(ACTION_END))
		}

		var lastInstructionStart int
		for i := len(ruleBytecode) - 1; i >= 0; i-- {
			if ruleBytecode[i] == byte(LABEL) && i+1 < len(ruleBytecode) && ruleBytecode[i+1] == 'L' {
				lastInstructionStart = i
				break
			}
		}

		log.Debug().Msgf("Last instruction start: %v", lastInstructionStart)

		lastInstruction := make([]byte, len(ruleBytecode)-lastInstructionStart)
		copy(lastInstruction, ruleBytecode[lastInstructionStart:])

		log.Debug().Msgf("Last instruction: %v", lastInstruction)
		tempBytecode := ruleBytecode[:len(ruleBytecode)-len(lastInstruction)]
		log.Debug().Msgf("Temp bytecode: %v", tempBytecode)
		tempBytecode = append(tempBytecode, actionBytecode...)
		log.Debug().Msgf("Temp bytecode after appending actions: %v", tempBytecode)
		tempBytecode = append(tempBytecode, lastInstruction...)
		log.Debug().Msgf("Temp bytecode after appending last instruction: %v", tempBytecode)
		ruleBytecode = tempBytecode

		ruleBytecode = append(ruleBytecode, byte(RULE_END))

		ruleBytecode = ReplaceLabelOffsets(ruleBytecode)
		bytecode = append(bytecode, ruleBytecode...)
	}

	// Generate indices
	ruleExecIndex, factRuleIndex, factDepIndex := GenerateIndices(bytecode)

	// Return BytecodeFile structure
	return BytecodeFile{
		Header: Header{
			Version:       Version,
			Checksum:      Checksum,
			ConstPoolSize: ConstPoolSize,
			NumRules:      uint32(len(ruleset.Rules)),
		},
		Instructions:        bytecode,
		RuleExecIndex:       ruleExecIndex,
		FactRuleLookupIndex: factRuleIndex,
		FactDependencyIndex: factDepIndex,
	}
}

func floatToBytes(f float64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, math.Float64bits(f))
	return b
}

func boolToBytes(b bool) []byte {
	if b {
		return []byte{1}
	}
	return []byte{0}
}

// GenerateIndices generates the indices for the bytecode
func GenerateIndices(bytecode []byte) ([]RuleExecutionIndex, map[string][]string, []FactDependencyIndex) {
	log.Debug().Msg("Starting GenerateIndices")
	ruleExecIndex := []RuleExecutionIndex{}
	factRuleIndex := make(map[string][]string)
	factDepIndex := []FactDependencyIndex{}

	i := 0
	for i < len(bytecode) {
		opcode := Opcode(bytecode[i])
		log.Debug().Int("index", i).Str("opcode", opcode.String()).Msg("Processing opcode")
		if opcode == RULE_START {
			ruleNameLength := int(bytecode[i+1])
			ruleName := string(bytecode[i+2 : i+2+ruleNameLength])
			ruleStartOffset := i
			log.Debug().Str("ruleName", ruleName).Int("startOffset", ruleStartOffset).Msg("Found RULE_START")

			// Find the end of the rule using a state machine approach
			for j := i + 2 + ruleNameLength; j < len(bytecode); j++ {
				if Opcode(bytecode[j]) == RULE_END {
					ruleEndOffset := j + 1 // Include the RULE_END byte
					log.Debug().Str("ruleName", ruleName).Int("endOffset", ruleEndOffset).Msg("Found RULE_END")

					// Add to rule execution index
					ruleExecIndex = append(ruleExecIndex, RuleExecutionIndex{
						RuleName:   ruleName,
						ByteOffset: ruleStartOffset,
					})
					log.Debug().Str("ruleName", ruleName).Int("byteOffset", ruleStartOffset).Msg("Added to ruleExecIndex")

					// Collect facts for dependency index
					facts := collectFactsFromBytecode(bytecode[ruleStartOffset:ruleEndOffset])
					factDepIndex = append(factDepIndex, FactDependencyIndex{
						RuleName: ruleName,
						Facts:    facts,
					})
					log.Debug().Str("ruleName", ruleName).Strs("facts", facts).Msg("Collected facts for dependency index")

					// Update fact rule lookup index
					for _, fact := range facts {
						factRuleIndex[fact] = append(factRuleIndex[fact], ruleName)
					}
					log.Debug().Str("ruleName", ruleName).Msg("Updated fact rule lookup index")

					// Move the outer loop index to the end of the current rule
					i = ruleEndOffset
					break
				}
			}
		} else {
			if opcode.HasOperands() {
				operandLength := determineOperandLength(opcode, bytecode[i+1:])
				log.Debug().Str("opcode", opcode.String()).Int("operandLength", operandLength).Msg("Opcode has operands")
				i += 1 + operandLength
			} else {
				i += 1
			}
		}
	}

	log.Debug().Msg("Completed GenerateIndices")
	return ruleExecIndex, factRuleIndex, factDepIndex
}

// Helper function to determine the length of operands for a given opcode
func determineOperandLength(opcode Opcode, operands []byte) int {
	switch opcode {
	case LOAD_CONST_FLOAT, LOAD_FACT_FLOAT:
		log.Debug().Str("opcode", opcode.String()).Msg("Returning operand length: 8")
		return 8 // 8 bytes for int64 or float64
	case LOAD_CONST_STRING, LOAD_FACT_STRING, LABEL, SEND_MESSAGE, TRIGGER_ACTION, UPDATE_FACT, ACTION_START:
		if len(operands) > 0 {
			length := 1 + int(operands[0]) // 1 byte for length + length of the string
			log.Debug().Str("opcode", opcode.String()).Int("length", length).Msg("Returning operand length")
			return length
		}
	case LOAD_CONST_BOOL, LOAD_FACT_BOOL:
		log.Debug().Str("opcode", opcode.String()).Msg("Returning operand length: 1")
		return 1 // 1 byte for bool
	case JUMP, JUMP_IF_TRUE, JUMP_IF_FALSE:
		log.Debug().Str("opcode", opcode.String()).Msg("Returning operand length: 4")
		return 4 // 4 bytes for the jump offset
	default:
		log.Debug().Str("opcode", opcode.String()).Msg("Returning operand length: 0")
		return 0
	}
	return 0
}

// collectFactsFromBytecode scans the bytecode to collect facts used in conditions
func collectFactsFromBytecode(bytecode []byte) []string {
	log.Debug().Msg("Starting collectFactsFromBytecode")
	facts := make(map[string]struct{})
	for i := 0; i < len(bytecode); {
		opcode := Opcode(bytecode[i])
		switch opcode {
		case LOAD_FACT_FLOAT, LOAD_FACT_STRING, LOAD_FACT_BOOL:
			factLength := int(bytecode[i+1])
			fact := string(bytecode[i+2 : i+2+factLength])
			facts[fact] = struct{}{}
			log.Debug().Str("fact", fact).Msg("Collected fact")
			i += 2 + factLength
		default:
			if opcode.HasOperands() {
				operandLength := determineOperandLength(opcode, bytecode[i+1:])
				log.Debug().Str("opcode", opcode.String()).Int("operandLength", operandLength).Msg("Opcode has operands")
				i += 1 + operandLength
			} else {
				i += 1
			}
		}
	}

	factList := make([]string, 0, len(facts))
	for fact := range facts {
		factList = append(factList, fact)
	}
	log.Debug().Int("factCount", len(factList)).Msg("Completed collectFactsFromBytecode")
	return factList
}

func ReplaceLabelOffsets(bytecode []byte) []byte {
	log.Debug().Msg("Replacing label offsets")

	for i := 0; i < len(bytecode); {
		opcode := Opcode(bytecode[i])
		if (opcode == JUMP_IF_FALSE || opcode == JUMP_IF_TRUE) && i+5 < len(bytecode) {
			// Check if the next 4 bytes form a label 'Lxyz'
			labelStart := i + 1
			label := string(bytecode[labelStart : labelStart+4])
			if label[0] == 'L' && isDigit(label[1]) && isDigit(label[2]) && isDigit(label[3]) {
				labelOffset := -1
				// Scan forward to find the label definition
				for j := 0; j < len(bytecode); j++ {
					if bytecode[j] == byte(LABEL) && string(bytecode[j+1:j+5]) == label {
						labelOffset = j
						break
					}
				}

				if labelOffset != -1 {
					// Calculate the relative offset from the current instruction to the label
					relativeOffset := labelOffset - i
					offsetBytes := make([]byte, 4)
					binary.LittleEndian.PutUint32(offsetBytes, uint32(relativeOffset))
					// Replace the label with the offset bytes
					copy(bytecode[labelStart:], offsetBytes)
					log.Debug().Str("label", label).Int("position", i).Int("offset", relativeOffset).Msg("Replaced label with offset")
				} else {
					log.Warn().Str("label", label).Msg("Label not found for jump instruction")
				}
				i += 5 // Move past the JUMP instruction and the 4-byte label
			} else {
				i += 1 // Move to the next byte
			}
		} else {
			i += 1 // Move to the next byte
		}
	}

	log.Debug().Msg("Label offsets replacement completed")
	return bytecode
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
