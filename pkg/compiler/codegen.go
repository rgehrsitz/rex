// rex/pkg/compiler/codegen.go

package compiler

import (
	"encoding/binary"
	"fmt"
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
func CalculateOffsets(instructions []Instruction) map[int]int {
	offsets := make(map[int]int)
	currentOffset := 0

	for i, instr := range instructions {
		offsets[i] = currentOffset
		log.Info().Msgf("Instruction %d: Opcode %v, Size %d, Offset %d", i, instr.Opcode, instr.Size(), currentOffset)
		currentOffset += instr.Size()
	}

	return offsets
}

// MapLabels maps labels to their corresponding positions.
func MapLabels(instructions []Instruction) map[string]int {
	labelPositions := make(map[string]int)
	for i, instr := range instructions {
		if instr.Opcode == LABEL {
			label := string(instr.Operands)
			labelPositions[label] = i
			log.Info().Msgf("Label %s at position %d", label, i)
		}
	}
	return labelPositions
}

// ReplaceLabels replaces labels with the corresponding byte offsets.
func ReplaceLabels(instructions []Instruction, offsets map[int]int, labelPositions map[string]int) []Instruction {
	finalInstructions := []Instruction{}

	for i, instr := range instructions {
		switch instr.Opcode {
		case JUMP_IF_FALSE, JUMP_IF_TRUE:
			parts := strings.Split(string(instr.Operands), " ")
			label := parts[3]
			if pos, ok := labelPositions[label]; ok {
				// Calculate the relative offset from the current instruction to the target instruction
				offset := offsets[pos] - (offsets[i] + instr.Size())
				parts[3] = strconv.Itoa(offset)
				instr.Operands = []byte(strings.Join(parts, " "))
				log.Info().Msgf("Replaced label %s with offset %d in instruction %d", label, offset, i)
			}
		}
		finalInstructions = append(finalInstructions, instr)
	}

	// Log final instructions with their offsets
	for i, instr := range finalInstructions {
		position := offsets[i]
		log.Info().Msgf("Replaced Label Instruction %d: Opcode %v, Operands %v, Position %d", i, instr.Opcode, instr.Operands, position)
	}

	return finalInstructions
}

// RemoveLabels removes any remaining label instructions and adjusts offsets.
func RemoveLabels(instructions []Instruction) []Instruction {
	finalInstructions := []Instruction{}
	offsetAdjustment := 0

	for _, instr := range instructions {
		if instr.Opcode == LABEL {
			offsetAdjustment += instr.Size()
			continue
		}
		// Adjust jump offsets
		if instr.Opcode == JUMP_IF_FALSE || instr.Opcode == JUMP_IF_TRUE {
			parts := strings.Split(string(instr.Operands), " ")
			offset, err := strconv.Atoi(parts[3])
			if err == nil {
				adjustedOffset := offset - offsetAdjustment
				parts[3] = strconv.Itoa(adjustedOffset)
				instr.Operands = []byte(strings.Join(parts, " "))
			}
		}
		finalInstructions = append(finalInstructions, instr)

		log.Info().Msgf("Instruction %d: Opcode %v, Operands %v, Position %d", len(finalInstructions), instr.Opcode, instr.Operands, len(finalInstructions))
	}

	return finalInstructions
}

// GenerateBytecode generates the bytecode instructions from the ruleset.
func GenerateBytecode(ruleset *Ruleset) []byte {
	var bytecode []byte

	// Generate rules bytecode
	for _, rule := range ruleset.Rules {
		ruleStartInstructions := []Instruction{{
			Opcode:   RULE_START,
			Operands: []byte(rule.Name),
		}}

		// Convert the conditions to a Node structure
		conditionNode := convertConditionGroupToNode(rule.Conditions)

		// Generate instructions from the condition tree
		instructions := generateInstructions(conditionNode, "L")

		// Optimize the generated instructions
		instructions = OptimizeInstructions(instructions)
		instructions = CombineJIFJIT(instructions)
		instructions = RemoveUnusedLabels(instructions)

		// Generate bytecode for actions
		var actionInstructions []Instruction
		for _, action := range rule.Actions {
			actionInstructions = append(actionInstructions, Instruction{
				Opcode:   ACTION_START,
				Operands: []byte(action.Type),
			})
			actionInstructions = append(actionInstructions, Instruction{
				Opcode:   OP_LITERAL,
				Operands: []byte(action.Target),
			})
			valBytes := []byte(fmt.Sprintf("%v", action.Value))
			actionInstructions = append(actionInstructions, Instruction{
				Opcode:   OP_LITERAL,
				Operands: valBytes,
			})
			actionInstructions = append(actionInstructions, Instruction{
				Opcode:   ACTION_END,
				Operands: nil,
			})
		}

		//combine instructions
		if len(instructions) > 0 {
			lastInstruction := instructions[len(instructions)-1]
			instructions = append(instructions[:len(instructions)-1], actionInstructions...)
			instructions = append(instructions, lastInstruction)
		} else {
			instructions = append(instructions, actionInstructions...)
		}

		// Map labels to their corresponding positions
		labelPositions := MapLabels(instructions)

		// Calculate byte offsets
		offsets := CalculateOffsets(instructions)

		// Replace labels and remove them
		replacedInstructions := ReplaceLabels(instructions, offsets, labelPositions)
		finalInstructions := RemoveLabels(replacedInstructions)

		// Append the rule start instructions to the bytecode
		for _, instr := range ruleStartInstructions {
			bytecode = append(bytecode, byte(instr.Opcode))
			bytecode = append(bytecode, instr.Operands...)
		}
		// Append the optimized instructions to the bytecode
		for _, instr := range finalInstructions {
			bytecode = append(bytecode, byte(instr.Opcode))
			bytecode = append(bytecode, instr.Operands...)
		}

		bytecode = append(bytecode, byte(RULE_END))
	}

	// Generate indices
	ruleExecIndex, factRuleIndex, factDepIndex := GenerateIndices(ruleset, bytecode)

	// Write indices to bytecode
	for _, idx := range ruleExecIndex {
		bytecode = append(bytecode, []byte(idx.RuleName)...)
		offsetBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(offsetBytes, uint32(idx.ByteOffset))
		bytecode = append(bytecode, offsetBytes...)
	}

	for fact, rules := range factRuleIndex {
		bytecode = append(bytecode, []byte(fact)...)
		bytecode = append(bytecode, byte(len(rules)))
		for _, rule := range rules {
			bytecode = append(bytecode, []byte(rule)...)
		}
	}

	for _, idx := range factDepIndex {
		bytecode = append(bytecode, []byte(idx.RuleName)...)
		bytecode = append(bytecode, byte(len(idx.Facts)))
		for _, fact := range idx.Facts {
			bytecode = append(bytecode, []byte(fact)...)
		}
	}

	return bytecode
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
