// rex/pkg/compiler/traverse.go

package compiler

import (
	"fmt"
	"strings"

	"rgehrsitz/rex/pkg/logging"
)

// Node represents a node in the condition tree for generating bytecode.
type Node struct {
	All  []Node
	Any  []Node
	Cond *Condition
}

// Condition represents a single condition in the rule.
type Condition struct {
	Fact     string
	Operator string
	Value    interface{}
}

// convertConditionGroupToNode converts a ConditionGroup to a Node structure.
func convertConditionGroupToNode(cg ConditionGroup) Node {
	node := Node{}
	for _, item := range cg.All {
		if item.Fact != "" {
			node.All = append(node.All, Node{
				Cond: &Condition{
					Fact:     item.Fact,
					Operator: item.Operator,
					Value:    convertValue(item.Value),
				},
			})
		} else {
			node.All = append(node.All, convertConditionOrGroupToNode(item))
		}
	}
	for _, item := range cg.Any {
		if item.Fact != "" {
			node.Any = append(node.Any, Node{
				Cond: &Condition{
					Fact:     item.Fact,
					Operator: item.Operator,
					Value:    convertValue(item.Value),
				},
			})
		} else {
			node.Any = append(node.Any, convertConditionOrGroupToNode(item))
		}
	}
	return node
}

// convertConditionOrGroupToNode converts a ConditionOrGroup to a Node structure.
func convertConditionOrGroupToNode(cog *ConditionOrGroup) Node {
	node := Node{}
	for _, item := range cog.All {
		if item.Fact != "" {
			node.All = append(node.All, Node{
				Cond: &Condition{
					Fact:     item.Fact,
					Operator: item.Operator,
					Value:    convertValue(item.Value),
				},
			})
		} else {
			node.All = append(node.All, convertConditionOrGroupToNode(item))
		}
	}
	for _, item := range cog.Any {
		if item.Fact != "" {
			node.Any = append(node.Any, Node{
				Cond: &Condition{
					Fact:     item.Fact,
					Operator: item.Operator,
					Value:    convertValue(item.Value),
				},
			})
		} else {
			node.Any = append(node.Any, convertConditionOrGroupToNode(item))
		}
	}
	return node
}

// convertValue dynamically determines the type of the value and returns it.
func convertValue(value interface{}) interface{} {
	switch v := value.(type) {
	case float64:
		if v == float64(int(v)) {
			return int(v)
		}
		return v
	case string:
		return v
	case bool:
		return v
	default:
		return v
	}
}

var labelCounter = 0

func getNextLabel(prefix string) string {
	label := fmt.Sprintf("%s%03d", prefix, labelCounter)
	labelCounter++
	return label
}

func traverse(node Node, successLabel string, failLabel string, instructions *[]Instruction, prefix string) {
	if len(node.All) > 0 {
		nextFailLabel := failLabel
		for i, child := range node.All {
			nextSuccessLabel := successLabel
			if i != len(node.All)-1 {
				nextSuccessLabel = getNextLabel(prefix)
			}
			traverse(child, nextSuccessLabel, nextFailLabel, instructions, prefix)
			if i != len(node.All)-1 {
				*instructions = append(*instructions, Instruction{Opcode: LABEL, Operands: []byte(nextSuccessLabel)})
			}
		}
	} else if len(node.Any) > 0 {
		for i, child := range node.Any {
			nextFailLabel := failLabel
			if i != len(node.Any)-1 {
				nextFailLabel = getNextLabel(prefix)
			}
			traverse(child, successLabel, nextFailLabel, instructions, prefix)
			if i != len(node.Any)-1 {
				*instructions = append(*instructions, Instruction{Opcode: LABEL, Operands: []byte(nextFailLabel)})
			}
		}
	} else if node.Cond != nil {
		condition := fmt.Sprintf("%s %s %v", node.Cond.Fact, node.Cond.Operator, node.Cond.Value)
		*instructions = append(*instructions, Instruction{Opcode: JUMP_IF_FALSE, Operands: []byte(condition + " " + failLabel)})
		*instructions = append(*instructions, Instruction{Opcode: JUMP_IF_TRUE, Operands: []byte(successLabel)})
	}
}

func generateInstructions(root Node, prefix string) []Instruction {
	instructions := []Instruction{}
	startLabel := getNextLabel(prefix)
	failLabel := getNextLabel(prefix)
	traverse(root, startLabel, failLabel, &instructions, prefix)
	instructions = append(instructions, Instruction{Opcode: LABEL, Operands: []byte(startLabel)})
	instructions = append(instructions, Instruction{Opcode: LABEL, Operands: []byte(failLabel)})
	return instructions
}

// optimizeInstructions removes unnecessary jumps and optimizes the instructions.
func OptimizeInstructions(instructions []Instruction) []Instruction {
	optimizedInstructions := []Instruction{}
	labelPositions := make(map[string]int)

	// First pass: record label positions
	for i, instr := range instructions {
		if instr.Opcode == LABEL {
			label := string(instr.Operands)
			labelPositions[label] = i
		}
	}

	// Second pass: optimize instructions by removing unnecessary jumps
	for i := 0; i < len(instructions); i++ {
		instr := instructions[i]
		if instr.Opcode == JUMP_IF_TRUE {
			label := instr.Operands
			if labelPos, ok := labelPositions[string(label)]; ok && labelPos == i+1 {
				// Skip the JUMP_IF_TRUE instruction if the label is immediately after it
				continue
			}
		}
		optimizedInstructions = append(optimizedInstructions, instr)
	}

	finalInstructions := RemoveUnusedLabels(optimizedInstructions)

	return finalInstructions
}

// combineJIFJIT combines consecutive JIF and JIT instructions.
func CombineJIFJIT(instructions []Instruction) []Instruction {
	combinedInstructions := []Instruction{}
	for i := 0; i < len(instructions); i++ {
		instr := instructions[i]
		if instr.Opcode == JUMP_IF_FALSE {
			condition := string(instr.Operands)
			parts := strings.Split(condition, " ")
			if i+1 < len(instructions) && instructions[i+1].Opcode == JUMP_IF_TRUE {
				label := string(instructions[i+1].Operands)
				// Pad the label with leading zeros to ensure fixed length
				paddedLabel := fmt.Sprintf("%04s", label)
				combinedOperands := []byte(fmt.Sprintf("%s %s %s %s", parts[0], parts[1], parts[2], paddedLabel))
				combinedInstructions = append(combinedInstructions, Instruction{Opcode: JUMP_IF_TRUE, Operands: combinedOperands})
				i++ // Skip the next JUMP_IF_TRUE instruction
				logging.Logger.Debug().Msgf("Combined JIF and JIT instructions: %s", string(combinedOperands))
				continue
			}
		}
		combinedInstructions = append(combinedInstructions, instr)
	}
	logging.Logger.Debug().Msgf("Combined instructions: %+v", combinedInstructions)
	return combinedInstructions
}

// removeUnusedLabels removes labels that are not used by any jump instruction.
func RemoveUnusedLabels(instructions []Instruction) []Instruction {
	usedLabels := make(map[string]bool)
	finalInstructions := []Instruction{}

	// First pass: record all used labels
	for _, instr := range instructions {
		if instr.Opcode == JUMP_IF_FALSE || instr.Opcode == JUMP_IF_TRUE {
			operands := strings.Split(string(instr.Operands), " ")
			label := operands[len(operands)-1]
			usedLabels[label] = true
		}
	}

	logging.Logger.Debug().Msgf("Used labels: %+v", usedLabels)

	// Second pass: remove unused labels while preserving existing label identifiers
	for _, instr := range instructions {
		if instr.Opcode == LABEL {
			if !usedLabels[string(instr.Operands)] {
				// Skip the label if it's not used by any jump instruction
				logging.Logger.Debug().Msgf("Removing unused label: %s", string(instr.Operands))
				continue
			}
		}
		finalInstructions = append(finalInstructions, instr)
	}

	logging.Logger.Debug().Msgf("Final instructions: %+v", finalInstructions)

	return finalInstructions
}
