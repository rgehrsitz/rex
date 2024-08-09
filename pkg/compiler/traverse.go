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

var labelCounter = 0
var usedLabels = map[string]bool{}
var availableLabels = []string{}

// convertConditionGroupToNode converts a ConditionGroup to a Node.
// It iterates over the conditions and condition groups in the given ConditionGroup,
// and creates a corresponding Node with the converted conditions and condition groups.
// The converted Node is then returned.
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

// convertConditionOrGroupToNode converts a ConditionOrGroup object to a Node object.
// It recursively traverses the ConditionOrGroup object and creates a Node object
// with the corresponding conditions and groups.
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
		return v
	case int:
		//convert int to float
		return float64(v)
	case string:
		return v
	case bool:
		return v
	default:
		return v
	}
}

func getNextLabel(prefix string) string {
	var label string
	if len(availableLabels) > 0 {
		label = availableLabels[0]
		availableLabels = availableLabels[1:]
	} else {
		label = fmt.Sprintf("%s%03d", prefix, labelCounter)
		labelCounter++
	}
	usedLabels[label] = true
	return label
}

// releaseLabel releases a label back to the pool of available labels.
func releaseLabel(label string) {
	if _, exists := usedLabels[label]; exists {
		delete(usedLabels, label)
		availableLabels = append(availableLabels, label)
	}
}

// traverse is a recursive function that traverses a tree-like structure represented by the given `node`.
// It generates a sequence of instructions based on the structure of the tree.
// The `successLabel` and `failLabel` parameters specify the labels to jump to in case of success or failure, respectively.
// The `instructions` parameter is a pointer to a slice of `Instruction` structs where the generated instructions will be appended.
// The `prefix` parameter is a string used to generate unique labels.
func traverse(node Node, successLabel string, failLabel string, instructions *[]Instruction, prefix string) {
	defer releaseLabel(successLabel)
	defer releaseLabel(failLabel)

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

// generateInstructions generates a sequence of instructions for traversing the given root node.
// It takes a root Node, a prefix string, and returns a slice of Instruction.
func generateInstructions(root Node, prefix string) []Instruction {
	instructions := []Instruction{}
	startLabel := getNextLabel(prefix)
	failLabel := getNextLabel(prefix)
	traverse(root, startLabel, failLabel, &instructions, prefix)
	instructions = append(instructions, Instruction{Opcode: LABEL, Operands: []byte(startLabel)})
	instructions = append(instructions, Instruction{Opcode: LABEL, Operands: []byte(failLabel)})
	releaseLabel(startLabel)
	releaseLabel(failLabel)
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

// CombineJIFJIT combines JUMP_IF_FALSE (JIF) and JUMP_IF_TRUE (JIT) instructions
// in the given slice of instructions. It looks for consecutive JIF and JIT instructions
// and combines them into a single JIT instruction with a padded label.
// The combined instructions are returned as a new slice of instructions.
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

// RemoveUnusedLabels removes labels that are not used by any jump instruction.
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
			label := string(instr.Operands)
			if !usedLabels[label] {
				// Release the unused label
				releaseLabel(label)
				// Skip the label if it's not used by any jump instruction
				logging.Logger.Debug().Msgf("Removing unused label: %s", label)
				continue
			}
		}
		finalInstructions = append(finalInstructions, instr)
	}

	logging.Logger.Debug().Msgf("Final instructions: %+v", finalInstructions)

	return finalInstructions
}
