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

// CalculateOffsets calculates the byte offsets of each instruction.
// func CalculateOffsets(instructions []Instruction) map[string]int {
// 	log.Info().Msg("Calculating offsets")
// 	offsets := make(map[string]int)
// 	currentOffset := 0

// 	for i, instr := range instructions {
// 		offsets[fmt.Sprintf("%v %v", instr.Opcode, instr.Operands)] = currentOffset
// 		log.Info().Msgf("Instruction %d: Opcode %v, Operands %v, Size %d, Offset %d", i, instr.Opcode, instr.Operands, instr.Size(), currentOffset)
// 		currentOffset += instr.Size()
// 	}

// 	return offsets
// }

// // MapLabels maps labels to their corresponding positions.
// func MapLabels(instructions []Instruction) map[string]int {
// 	labelPositions := make(map[string]int)
// 	for i, instr := range instructions {
// 		if instr.Opcode == LABEL {
// 			label := string(instr.Operands)
// 			if strings.HasPrefix(label, "L") {
// 				labelPositions[label] = i
// 				log.Info().Msgf("Label %s at position %d", label, i)
// 			}
// 		}
// 	}
// 	return labelPositions
// }

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
func GenerateBytecode(ruleset *Ruleset) []byte {
	var bytecode []byte

	// Generate rules bytecode
	for _, rule := range ruleset.Rules {
		log.Debug().Msgf("Processing rule: %s", rule.Name)
		bytecode = append(bytecode, byte(RULE_START))
		// Append the rule name as an operand
		bytecode = append(bytecode, byte(len(rule.Name)))
		bytecode = append(bytecode, []byte(rule.Name)...)

		// Convert the conditions to a Node structure
		conditionNode := convertConditionGroupToNode(rule.Conditions)

		// Generate instructions from the condition tree
		instructions := generateInstructions(conditionNode, "L")

		// Optimize the generated instructions
		instructions = OptimizeInstructions(instructions)
		instructions = CombineJIFJIT(instructions)
		instructions = RemoveUnusedLabels(instructions)

		// Append the optimized instructions to the bytecode
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
					// var factBytes []byte
					var valueBytes []byte
					var comparisonOpcode Opcode

					// figure out which factOpcode to use and the corresponding operand of type string
					if intValue, err := strconv.Atoi(value); err == nil {
						factOpcode = LOAD_FACT_INT
						// factBytes = stringToBytes(fact)
						valueOpcode = LOAD_CONST_INT
						valueBytes = intToBytes(intValue)
					} else if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
						factOpcode = LOAD_FACT_FLOAT
						// factBytes = stringToBytes(fact)
						valueOpcode = LOAD_CONST_FLOAT
						valueBytes = floatToBytes(floatValue)
					} else if boolValue, err := strconv.ParseBool(value); err == nil {
						factOpcode = LOAD_FACT_BOOL
						// factBytes = stringToBytes(fact)
						valueOpcode = LOAD_CONST_BOOL
						valueBytes = boolToBytes(boolValue)
					} else {
						factOpcode = LOAD_FACT_STRING
						// factBytes = stringToBytes(fact)
						valueOpcode = LOAD_CONST_STRING
						valueBytes = []byte(value)
					}

					switch operator {
					case "GT":
						switch factOpcode {
						case LOAD_FACT_INT:
							comparisonOpcode = GT_INT
						case LOAD_FACT_FLOAT:
							comparisonOpcode = GT_FLOAT
						}
					case "EQ":
						switch factOpcode {
						case LOAD_FACT_INT:
							comparisonOpcode = EQ_INT
						case LOAD_FACT_FLOAT:
							comparisonOpcode = EQ_FLOAT
						case LOAD_FACT_STRING:
							comparisonOpcode = EQ_STRING
						case LOAD_FACT_BOOL:
							comparisonOpcode = EQ_BOOL
						}
					case "NEQ":
						switch factOpcode {
						case LOAD_FACT_INT:
							comparisonOpcode = NEQ_INT
						case LOAD_FACT_FLOAT:
							comparisonOpcode = NEQ_FLOAT
						case LOAD_FACT_STRING:
							comparisonOpcode = NEQ_STRING
						case LOAD_FACT_BOOL:
							comparisonOpcode = NEQ_BOOL
						}
					case "LT":
						switch factOpcode {
						case LOAD_FACT_INT:
							comparisonOpcode = LT_INT
						case LOAD_FACT_FLOAT:
							comparisonOpcode = LT_FLOAT
						}
					case "LTE":
						switch factOpcode {
						case LOAD_FACT_INT:
							comparisonOpcode = LTE_INT
						case LOAD_FACT_FLOAT:
							comparisonOpcode = LTE_FLOAT
						}
					case "GTE":
						switch factOpcode {
						case LOAD_FACT_INT:
							comparisonOpcode = GTE_INT
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
					bytecode = append(bytecode, byte(factOpcode))
					bytecode = append(bytecode, byte(len(fact)))
					bytecode = append(bytecode, []byte(fact)...)

					bytecode = append(bytecode, byte(valueOpcode))
					if valueOpcode == LOAD_CONST_STRING {
						bytecode = append(bytecode, byte(len(value)))
					}
					bytecode = append(bytecode, valueBytes...)

					bytecode = append(bytecode, byte(comparisonOpcode))

					bytecode = append(bytecode, byte(instr.Opcode))

					//bytecode = append(bytecode, byte(len(label)))
					bytecode = append(bytecode, []byte(label)...)

					log.Debug().Msgf("Appended separated instructions for condition: factOpcode=%v, valueOpcode=%v, comparisonOpcode=%v", factOpcode, valueOpcode, comparisonOpcode)
					continue
				}
			}

			// Append the instruction as usual
			bytecode = append(bytecode, byte(instr.Opcode))
			bytecode = append(bytecode, instr.Operands...)
			log.Debug().Msgf("Appended instruction: Opcode=%v, Operands=%v", instr.Opcode, instr.Operands)
		}

		actionBytecode := []byte{}
		// Generate bytecode for actions
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
			case int:
				actionBytecode = append(actionBytecode, byte(ACTION_VALUE_INT))
				intBytes := make([]byte, 8)
				binary.Write(bytes.NewBuffer(intBytes), binary.LittleEndian, int64(v))
				actionBytecode = append(actionBytecode, intBytes...)
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
		for i := len(bytecode) - 1; i >= 0; i-- {
			if bytecode[i] == byte(LABEL) && i+1 < len(bytecode) && bytecode[i+1] == 'L' {
				lastInstructionStart = i
				break
			}
		}

		log.Debug().Msgf("Last instruction start: %v", lastInstructionStart)

		lastInstruction := make([]byte, len(bytecode)-lastInstructionStart)
		copy(lastInstruction, bytecode[lastInstructionStart:])

		log.Debug().Msgf("Last instruction: %v", lastInstruction)
		tempBytecode := bytecode[:len(bytecode)-len(lastInstruction)]
		log.Debug().Msgf("Temp bytecode: %v", tempBytecode)
		tempBytecode = append(tempBytecode, actionBytecode...)
		log.Debug().Msgf("Temp bytecode after appending actions: %v", tempBytecode)
		tempBytecode = append(tempBytecode, lastInstruction...)
		log.Debug().Msgf("Temp bytecode after appending last instruction: %v", tempBytecode)
		bytecode = tempBytecode

		bytecode = append(bytecode, byte(RULE_END))

		// // Generate offsets and label positions
		// parsedInstructions := parseInstructions(bytecode)
		// offsets := CalculateOffsets(parsedInstructions)
		// labelPositions := MapLabels(parsedInstructions)

		// // Replace labels with actual offsets
		// parsedInstructions = ReplaceLabels(parsedInstructions, offsets, labelPositions)

		// // Remove any remaining label instructions
		// parsedInstructions = RemoveLabels(parsedInstructions)

		// // Update the bytecode with the new instructions
		// bytecode = serializeInstructions(parsedInstructions)

		bytecode = ReplaceLabelOffsets(bytecode)
	}

	// Generate indices
	ruleExecIndex, factRuleIndex, factDepIndex := GenerateIndices(ruleset, bytecode)

	// Write indices to bytecode
	for _, idx := range ruleExecIndex {
		// NEW CODE: Append the rule name with length prefix
		bytecode = append(bytecode, byte(len(idx.RuleName)))
		bytecode = append(bytecode, []byte(idx.RuleName)...)

		offsetBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(offsetBytes, uint32(idx.ByteOffset))
		bytecode = append(bytecode, offsetBytes...)
	}

	for fact, rules := range factRuleIndex {
		// NEW CODE: Append the fact name with length prefix
		bytecode = append(bytecode, byte(len(fact)))
		bytecode = append(bytecode, []byte(fact)...)

		bytecode = append(bytecode, byte(len(rules)))
		for _, rule := range rules {
			// NEW CODE: Append the rule name with length prefix
			bytecode = append(bytecode, byte(len(rule)))
			bytecode = append(bytecode, []byte(rule)...)
		}
	}

	for _, idx := range factDepIndex {
		// NEW CODE: Append the rule name with length prefix
		bytecode = append(bytecode, byte(len(idx.RuleName)))
		bytecode = append(bytecode, []byte(idx.RuleName)...)

		bytecode = append(bytecode, byte(len(idx.Facts)))
		for _, fact := range idx.Facts {
			// NEW CODE: Append the fact name with length prefix
			bytecode = append(bytecode, byte(len(fact)))
			bytecode = append(bytecode, []byte(fact)...)
		}
	}

	log.Debug().Msg("Bytecode generation complete")
	log.Debug().Msgf("Bytecode length: %v", len(bytecode))
	log.Debug().Msgf("Bytecode: %v", bytecode)
	return bytecode
}

// Helper functions for converting values to bytes
func intToBytes(n int) []byte {
	b := make([]byte, 8)                        // Change to 8 bytes for int64
	binary.LittleEndian.PutUint64(b, uint64(n)) // Change to PutUint64
	return b
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
func GenerateIndices(ruleset *Ruleset, bytecode []byte) ([]RuleExecutionIndex, map[string][]string, []FactDependencyIndex) {
	ruleExecIndex := make([]RuleExecutionIndex, len(ruleset.Rules))
	factRuleIndex := make(map[string][]string)
	factDepIndex := make([]FactDependencyIndex, len(ruleset.Rules))

	offset := len(bytecode)

	for i, rule := range ruleset.Rules {
		ruleExecIndex[i] = RuleExecutionIndex{
			RuleName:   rule.Name,
			ByteOffset: offset,
		}
		offset += len(rule.Name) + 2 // Rule name length + opcode bytes

		// Collect facts for dependency index
		facts := collectFacts(rule.Conditions)
		factDepIndex[i] = FactDependencyIndex{
			RuleName: rule.Name,
			Facts:    facts,
		}

		// Update fact rule lookup index
		for _, fact := range facts {
			if _, ok := factRuleIndex[fact]; !ok {
				factRuleIndex[fact] = []string{rule.Name}
			} else {
				factRuleIndex[fact] = append(factRuleIndex[fact], rule.Name)
			}
		}
	}

	return ruleExecIndex, factRuleIndex, factDepIndex
}

func collectFacts(conditions ConditionGroup) []string {
	facts := []string{}
	for _, condOrGroup := range conditions.All {
		if condOrGroup.Fact != "" {
			facts = append(facts, condOrGroup.Fact)
		}
		facts = append(facts, collectFactsFromGroup(condOrGroup)...)
	}
	return facts
}

func collectFactsFromGroup(condOrGroup *ConditionOrGroup) []string {
	facts := []string{}
	if condOrGroup.Fact != "" {
		facts = append(facts, condOrGroup.Fact)
	}
	if condOrGroup.All != nil {
		for _, subCondOrGroup := range condOrGroup.All {
			facts = append(facts, collectFactsFromGroup(subCondOrGroup)...)
		}
	}
	if condOrGroup.Any != nil {
		for _, subCondOrGroup := range condOrGroup.Any {
			facts = append(facts, collectFactsFromGroup(subCondOrGroup)...)
		}
	}
	return facts
}

// func parseInstructions(bytecode []byte) []Instruction {
// 	log.Debug().Msg("Parsing bytecode")
// 	var instructions []Instruction
// 	for i := 0; i < len(bytecode); {
// 		opcode := Opcode(bytecode[i])
// 		log.Debug().Msgf("Opcode: %v", opcode)
// 		//i++
// 		var operands []byte
// 		switch opcode {
// 		case LOAD_FACT_STRING, LOAD_CONST_STRING, JUMP_IF_FALSE, JUMP_IF_TRUE, LOAD_FACT_INT, LOAD_FACT_FLOAT, LOAD_FACT_BOOL:
// 			i++
// 			length := int(bytecode[i])
// 			i++
// 			operands = bytecode[i : i+length]
// 			i += length
// 		case LOAD_CONST_INT:
// 			i++
// 			operands = bytecode[i : i+8]
// 			i += 8
// 		case LOAD_CONST_FLOAT:
// 			i++
// 			operands = bytecode[i : i+8]
// 			i += 8
// 		case LOAD_CONST_BOOL:
// 			i++
// 			operands = []byte{bytecode[i]}
// 			i++
// 		case RULE_START, ACTION_START:
// 			i++
// 			length := int(bytecode[i])
// 			i++
// 			operands = bytecode[i : i+length]
// 			i += length
// 		case GTE_FLOAT, GTE_INT, GT_FLOAT, GT_INT, LTE_FLOAT, LTE_INT, LT_FLOAT, LT_INT,
// 			NEQ_FLOAT, NEQ_INT, EQ_FLOAT, EQ_INT, CONTAINS_STRING, NOT_CONTAINS_STRING, EQ_STRING, NEQ_STRING,
// 			EQ_BOOL, NEQ_BOOL, AND, OR, NOT:
// 			i++
// 		case LABEL:
// 			operands = bytecode[i : i+4]
// 			i += 4
// 		default:
// 			for ; i < len(bytecode) && bytecode[i] != byte(RULE_END) && bytecode[i] != byte(ACTION_END); i++ {
// 				operands = append(operands, bytecode[i])
// 			}
// 		}
// 		instructions = append(instructions, Instruction{Opcode: opcode, Operands: operands})
// 		if opcode == RULE_END || opcode == ACTION_END {
// 			i++
// 		}
// 		log.Debug().Msgf("Instruction: %v %v", opcode, operands)
// 	}
// 	return instructions
// }

// func serializeInstructions(instructions []Instruction) []byte {
// 	var bytecode []byte
// 	for _, instr := range instructions {
// 		bytecode = append(bytecode, byte(instr.Opcode))
// 		switch instr.Opcode {
// 		case LOAD_FACT_STRING, LOAD_CONST_STRING, LABEL, JUMP_IF_FALSE, JUMP_IF_TRUE:
// 			bytecode = append(bytecode, byte(len(instr.Operands)))
// 			bytecode = append(bytecode, instr.Operands...)
// 		case LOAD_FACT_INT, LOAD_FACT_FLOAT, LOAD_FACT_BOOL, LOAD_CONST_INT, LOAD_CONST_FLOAT, LOAD_CONST_BOOL:
// 			bytecode = append(bytecode, instr.Operands...)
// 		case RULE_START, ACTION_START:
// 			bytecode = append(bytecode, byte(len(instr.Operands)))
// 			bytecode = append(bytecode, instr.Operands...)
// 		default:
// 			bytecode = append(bytecode, instr.Operands...)
// 		}
// 	}
// 	return bytecode
// }

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
