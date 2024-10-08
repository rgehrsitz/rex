// rex/pkg/compiler/codegen.go

package compiler

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"rgehrsitz/rex/pkg/logging"
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
	logging.Logger.Debug().Msg("Replacing labels")
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
				logging.Logger.Debug().Msgf("Replaced label %s with offset %d in instruction %d", label, offset, i)
				releaseLabel(label) // Release the label after replacement
			}
		}
		finalInstructions = append(finalInstructions, instr)
	}

	// Log final instructions with their offsets
	for i, instr := range finalInstructions {
		position := offsets[fmt.Sprintf("%v %v", instr.Opcode, instr.Operands)]
		logging.Logger.Debug().Msgf("Final Instruction %d: Opcode %v, Operands %v, Position %d", i, instr.Opcode, instr.Operands, position)
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

func GenerateBytecode(ruleset *Ruleset) BytecodeFile {
	var bytecode []byte

	for _, rule := range ruleset.Rules {

		logging.Logger.Debug().
			Str("ruleName", rule.Name).
			Int("nameLength", len(rule.Name)).
			Int("bytecodeLength", len(bytecode)).
			Msg("Starting to generate bytecode for rule")

		ruleBytecode := []byte{byte(RULE_START)}
		nameLength := len(rule.Name)
		if nameLength > 255 {
			logging.Logger.Warn().
				Str("ruleName", rule.Name).
				Int("nameLength", nameLength).
				Msg("Rule name exceeds 255 characters")
			// Handle long names (e.g., use two bytes for length)
			ruleBytecode = append(ruleBytecode, byte(nameLength>>8), byte(nameLength&0xff))
		} else {
			ruleBytecode = append(ruleBytecode, byte(nameLength))
		}
		ruleBytecode = append(ruleBytecode, []byte(rule.Name)...)

		// Append the rule priority
		ruleBytecode = append(ruleBytecode, byte(PRIORITY))
		priorityBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(priorityBytes, uint32(rule.Priority))
		ruleBytecode = append(ruleBytecode, priorityBytes...)

		// Add script definitions to bytecode
		for scriptName, script := range rule.Scripts {
			ruleBytecode = append(ruleBytecode, byte(SCRIPT_DEF))
			ruleBytecode = append(ruleBytecode, byte(len(scriptName)))
			ruleBytecode = append(ruleBytecode, []byte(scriptName)...)
			ruleBytecode = append(ruleBytecode, byte(len(script.Params)))
			for _, param := range script.Params {
				ruleBytecode = append(ruleBytecode, byte(len(param)))
				ruleBytecode = append(ruleBytecode, []byte(param)...)
			}
			ruleBytecode = append(ruleBytecode, byte(len(script.Body)))
			ruleBytecode = append(ruleBytecode, []byte(script.Body)...)
		}

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

					logging.Logger.Debug().Msgf("Processing condition: fact=%s, operator=%s, value=%s, label=%s", fact, operator, value, label)

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

					// Check if the fact is actually a script call
					if script, ok := rule.Scripts[fact]; ok {
						ruleBytecode = append(ruleBytecode, byte(SCRIPT_CALL))
						ruleBytecode = append(ruleBytecode, byte(len(fact)))
						ruleBytecode = append(ruleBytecode, []byte(fact)...)
						ruleBytecode = append(ruleBytecode, byte(len(script.Params)))
						for _, param := range script.Params {
							ruleBytecode = append(ruleBytecode, byte(len(param)))
							ruleBytecode = append(ruleBytecode, []byte(param)...)
						}
					} else {
						// Append the separated instructions
						ruleBytecode = append(ruleBytecode, byte(factOpcode))
						ruleBytecode = append(ruleBytecode, byte(len(fact)))
						ruleBytecode = append(ruleBytecode, []byte(fact)...)
					}

					ruleBytecode = append(ruleBytecode, byte(valueOpcode))
					if valueOpcode == LOAD_CONST_STRING {
						ruleBytecode = append(ruleBytecode, byte(len(value)))
					}
					ruleBytecode = append(ruleBytecode, valueBytes...)

					ruleBytecode = append(ruleBytecode, byte(comparisonOpcode))

					ruleBytecode = append(ruleBytecode, byte(instr.Opcode))

					ruleBytecode = append(ruleBytecode, []byte(label)...)

					logging.Logger.Debug().Msgf("Appended separated instructions for condition: factOpcode=%v, valueOpcode=%v, comparisonOpcode=%v", factOpcode, valueOpcode, comparisonOpcode)
					continue
				}
			}

			// Append the instruction as usual
			ruleBytecode = append(ruleBytecode, byte(instr.Opcode))
			ruleBytecode = append(ruleBytecode, instr.Operands...)
			logging.Logger.Debug().Msgf("Appended instruction: Opcode=%v, Operands=%v", instr.Opcode, instr.Operands)
		}

		// Generate bytecode for actions
		actionBytecode := []byte{}
		for _, action := range rule.Actions {
			logging.Logger.Debug().Msgf("Processing action: %s", action.Type)
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
				binary.LittleEndian.PutUint64(floatBytes, math.Float64bits(v))
				actionBytecode = append(actionBytecode, floatBytes...)
			case string:
				if strings.HasPrefix(v, "{") && strings.HasSuffix(v, "}") {
					// This is a script call
					scriptName := strings.Trim(v, "{}")
					actionBytecode = append(actionBytecode, byte(SCRIPT_CALL))
					actionBytecode = append(actionBytecode, byte(len(scriptName)))
					actionBytecode = append(actionBytecode, []byte(scriptName)...)

					// Add script parameters
					if script, ok := rule.Scripts[scriptName]; ok {
						actionBytecode = append(actionBytecode, byte(len(script.Params)))
						for _, param := range script.Params {
							actionBytecode = append(actionBytecode, byte(len(param)))
							actionBytecode = append(actionBytecode, []byte(param)...)
						}
					}
				} else {
					// This is a regular string value
					actionBytecode = append(actionBytecode, byte(ACTION_VALUE_STRING))
					actionBytecode = append(actionBytecode, byte(len(v)))
					actionBytecode = append(actionBytecode, []byte(v)...)
				}
			case bool:
				actionBytecode = append(actionBytecode, byte(ACTION_VALUE_BOOL))
				if v {
					actionBytecode = append(actionBytecode, byte(1))
				} else {
					actionBytecode = append(actionBytecode, byte(0))
				}
			default:
				logging.Logger.Error().Msgf("Unsupported action value type: %T", v)
				continue
			}

			actionBytecode = append(actionBytecode, byte(ACTION_END))
		}

		var lastInstructionStart int
		// Find the start of the last instruction
		for i := len(ruleBytecode) - 1; i >= 0; i-- {
			if ruleBytecode[i] == byte(LABEL) && i+1 < len(ruleBytecode) && ruleBytecode[i+1] == 'L' {
				lastInstructionStart = i
				break
			}
		}

		logging.Logger.Debug().Msgf("Last instruction start: %v", lastInstructionStart)

		lastInstruction := make([]byte, len(ruleBytecode)-lastInstructionStart)
		copy(lastInstruction, ruleBytecode[lastInstructionStart:])

		logging.Logger.Debug().Msgf("Last instruction: %v", lastInstruction)
		tempBytecode := ruleBytecode[:len(ruleBytecode)-len(lastInstruction)]
		logging.Logger.Debug().Msgf("Temp bytecode: %v", tempBytecode)
		tempBytecode = append(tempBytecode, actionBytecode...)
		logging.Logger.Debug().Msgf("Temp bytecode after appending actions: %v", tempBytecode)
		tempBytecode = append(tempBytecode, lastInstruction...)
		logging.Logger.Debug().Msgf("Temp bytecode after appending last instruction: %v", tempBytecode)
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

// floatToBytes converts a float64 value to a byte slice.
// It uses binary.LittleEndian to convert the float64 value to its binary representation.
// The resulting byte slice has a length of 8 bytes.
func floatToBytes(f float64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, math.Float64bits(f))
	return b
}

// boolToBytes converts a boolean value to a byte slice.
// If the input boolean value is true, it returns a byte slice containing a single byte with value 1.
// If the input boolean value is false, it returns a byte slice containing a single byte with value 0.
func boolToBytes(b bool) []byte {
	if b {
		return []byte{1}
	}
	return []byte{0}
}

func GenerateIndices(bytecode []byte) ([]RuleExecutionIndex, map[string][]string, []FactDependencyIndex) {
	logging.Logger.Debug().Msg("Starting GenerateIndices")
	ruleExecIndex := []RuleExecutionIndex{}
	factRuleIndex := make(map[string][]string)
	factDepIndex := []FactDependencyIndex{}

	opcode := Opcode(bytecode[0])
	if opcode != RULE_START {
		logging.Logger.Error().Msg("Expected RULE_START as the first opcode")
		return nil, nil, nil
	}

	i := 0
	for i < len(bytecode) {
		opcode := Opcode(bytecode[i])
		logging.Logger.Debug().Int("index", i).Str("opcode", opcode.String()).Msg("Processing opcode")
		if opcode == RULE_START {

			ruleNameLength := int(bytecode[i+1])
			ruleName := string(bytecode[i+2 : i+2+ruleNameLength])
			ruleStartOffset := i
			logging.Logger.Debug().Str("ruleName", ruleName).Int("startOffset", ruleStartOffset).Msg("Found RULE_START")

			// Find the priority of the rule
			priority := 0
			for j := i + 2 + ruleNameLength; j < len(bytecode); j++ {
				if Opcode(bytecode[j]) == PRIORITY {
					priority = int(binary.LittleEndian.Uint32(bytecode[j+1 : j+5]))
					break
				}
				if Opcode(bytecode[j]) == RULE_END {
					break
				}
			}

			// Find the end of the rule
			for j := i + 2 + ruleNameLength; j < len(bytecode); j++ {
				if Opcode(bytecode[j]) == RULE_END {
					// Check that the next opcode is either another RULE_START or the end of the bytecode
					if j+1 < len(bytecode) && Opcode(bytecode[j+1]) != RULE_START {
						continue
					}

					ruleEndOffset := j + 1 // Include the RULE_END byte
					logging.Logger.Debug().Str("ruleName", ruleName).Int("endOffset", ruleEndOffset).Msg("Found RULE_END")

					// Add to rule execution index
					ruleExecIndex = append(ruleExecIndex, RuleExecutionIndex{
						RuleNameLength: uint32(ruleNameLength),
						RuleName:       ruleName,
						ByteOffset:     ruleStartOffset,
						Priority:       priority,
					})
					logging.Logger.Debug().Str("ruleName", ruleName).Int("byteOffset", ruleStartOffset).Int("priority", priority).Msg("Added to ruleExecIndex")

					// Collect facts for dependency index
					facts := collectFactsFromBytecode(bytecode[ruleStartOffset:ruleEndOffset])

					// Remove duplicates
					uniqueFacts := make(map[string]struct{})
					for _, fact := range facts {
						uniqueFacts[fact] = struct{}{}
					}

					factList := make([]string, 0, len(uniqueFacts))
					for fact := range uniqueFacts {
						factList = append(factList, fact)
					}

					factDepIndex = append(factDepIndex, FactDependencyIndex{
						RuleNameLength: uint32(ruleNameLength),
						RuleName:       ruleName,
						Facts:          factList,
					})
					logging.Logger.Debug().Str("ruleName", ruleName).Strs("facts", facts).Msg("Collected facts for dependency index")

					// Update fact rule lookup index
					for _, fact := range factList {
						factRuleIndex[fact] = append(factRuleIndex[fact], ruleName)
					}
					logging.Logger.Debug().Str("ruleName", ruleName).Msg("Updated fact rule lookup index")

					// Move the outer loop index to the end of the current rule
					i = ruleEndOffset
					break
				}
			}
		} else {
			if opcode.HasOperands() {
				operandLength := determineOperandLength(opcode, bytecode[i+1:])
				logging.Logger.Debug().Str("opcode", opcode.String()).Int("operandLength", operandLength).Msg("Opcode has operands")
				i += 1 + operandLength
			} else {
				i += 1
			}
		}
	}

	logging.Logger.Debug().Msg("Completed GenerateIndices")
	return ruleExecIndex, factRuleIndex, factDepIndex
}

// Helper function to determine the length of operands for a given opcode
func determineOperandLength(opcode Opcode, operands []byte) int {
	switch opcode {
	case LOAD_CONST_FLOAT, LOAD_FACT_FLOAT:
		logging.Logger.Debug().Str("opcode", opcode.String()).Msg("Returning operand length: 8")
		return 8 // 8 bytes for int64 or float64
	case LOAD_CONST_STRING, LOAD_FACT_STRING, SEND_MESSAGE, TRIGGER_ACTION, UPDATE_FACT, ACTION_START, RULE_START:
		if len(operands) > 0 {
			length := 1 + int(operands[0]) // 1 byte for length + length of the string
			logging.Logger.Debug().Str("opcode", opcode.String()).Int("length", length).Msg("Returning operand length")
			return length
		}
	case LOAD_CONST_BOOL, LOAD_FACT_BOOL:
		logging.Logger.Debug().Str("opcode", opcode.String()).Msg("Returning operand length: 1")
		return 1 // 1 byte for bool
	case JUMP, JUMP_IF_TRUE, JUMP_IF_FALSE:
		logging.Logger.Debug().Str("opcode", opcode.String()).Msg("Returning operand length: 4")
		return 4 // 4 bytes for the jump offset
	case LABEL:
		logging.Logger.Debug().Str("opcode", opcode.String()).Msg("Returning operand length: 4")
		return 4 // 4 bytes for the fixed label widths
	case PRIORITY:
		logging.Logger.Debug().Str("opcode", opcode.String()).Msg("Returning operand length: 4")
		return 4 // 4 bytes for the priority
	default:
		logging.Logger.Debug().Str("opcode", opcode.String()).Msg("Returning operand length: 0")
		return 0
	}
	return 0
}

// collectFactsFromBytecode scans the bytecode to collect facts used in conditions
func collectFactsFromBytecode(bytecode []byte) []string {
	logging.Logger.Debug().Msg("Starting collectFactsFromBytecode")
	facts := make(map[string]struct{})
	maxFactNameLength := 64 // Define a maximum length for fact names

	for i := 0; i < len(bytecode); {
		opcode := Opcode(bytecode[i])

		if opcode == LOAD_FACT_FLOAT || opcode == LOAD_FACT_STRING || opcode == LOAD_FACT_BOOL {
			if i+1 >= len(bytecode) {
				break
			}
			factLength := int(bytecode[i+1])

			// Check if the fact name length is within the allowed limit
			if factLength <= 0 || factLength > maxFactNameLength {
				i++
				continue
			}

			if i+2+factLength > len(bytecode) {
				break
			}
			factName := string(bytecode[i+2 : i+2+factLength])

			// Verify if the fact name consists of allowed characters
			if !isValidFactName(factName) {
				i += 2 + factLength
				continue
			}

			// Check if the subsequent opcodes form a valid sequence
			if i+2+factLength < len(bytecode) {
				nextOpcode := Opcode(bytecode[i+2+factLength])
				if !isValidFactLoadingSequence(nextOpcode) {
					i += 2 + factLength
					continue
				}
			}

			facts[factName] = struct{}{}
			logging.Logger.Debug().Str("fact", factName).Msg("Collected fact")
			i += 2 + factLength
		} else if opcode == SCRIPT_DEF {
			// Process SCRIPT_DEF to collect script parameter facts
			if i+1 >= len(bytecode) {
				break
			}
			scriptNameLength := int(bytecode[i+1])
			i += 2 + scriptNameLength

			if i >= len(bytecode) {
				break
			}
			paramsCount := int(bytecode[i])
			i++

			for j := 0; j < paramsCount; j++ {
				if i+1 >= len(bytecode) {
					break
				}
				paramLength := int(bytecode[i])
				i++

				if i+paramLength > len(bytecode) {
					break
				}
				paramName := string(bytecode[i : i+paramLength])
				facts[paramName] = struct{}{}
				logging.Logger.Debug().Str("scriptParam", paramName).Msg("Collected script parameter as fact")
				i += paramLength
			}
		} else {
			if opcode.HasOperands() {
				operandLength := determineOperandLength(opcode, bytecode[i+1:])
				logging.Logger.Debug().Str("opcode", opcode.String()).Int("operandLength", operandLength).Msg("Opcode has operands")
				i += 1 + operandLength
			} else {
				i++
			}
		}
	}

	factList := make([]string, 0, len(facts))
	for fact := range facts {
		factList = append(factList, fact)
	}
	logging.Logger.Debug().Int("factCount", len(factList)).Msg("Completed collectFactsFromBytecode")
	return factList
}

// isValidFactName checks if the given fact name is valid.
// A valid fact name consists of lowercase and uppercase letters, digits,
// underscores, and colons.
func isValidFactName(name string) bool {
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == ':') {
			return false
		}
	}
	return true
}

// isValidFactLoadingSequence checks if the given opcode is a valid one that can follow a fact loading instruction.
func isValidFactLoadingSequence(opcode Opcode) bool {
	switch opcode {
	case LOAD_CONST_FLOAT, LOAD_CONST_STRING, LOAD_CONST_BOOL:
		return true
	default:
		return false
	}
}

// ReplaceLabelOffsets replaces the label offsets in the given bytecode slice.
// It searches for jump instructions (JUMP_IF_FALSE and JUMP_IF_TRUE) and checks if the next 4 bytes form a label 'Lxyz'.
// If a valid label is found, it scans forward to find the label definition and calculates the relative offset from the current instruction to the label.
// The label is then replaced with the offset bytes in the bytecode slice.
// If a label is not found for a jump instruction, a warning message is logged.
// The modified bytecode slice is returned.
func ReplaceLabelOffsets(bytecode []byte) []byte {
	logging.Logger.Debug().Msg("Replacing label offsets")

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
					logging.Logger.Debug().Str("label", label).Int("position", i).Int("offset", relativeOffset).Msg("Replaced label with offset")
				} else {
					logging.Logger.Warn().Str("label", label).Msg("Label not found for jump instruction")
				}
				i += 5 // Move past the JUMP instruction and the 4-byte label
			} else {
				i += 1 // Move to the next byte
			}
		} else {
			i += 1 // Move to the next byte
		}
	}

	logging.Logger.Debug().Msg("Label offsets replacement completed")
	return bytecode
}

// isDigit checks if the given byte is a digit.
// It returns true if the byte is a digit (0-9), otherwise false.
func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
