// rex/pkg/compiler/codegen_test.go

package compiler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// func TestMapLabels(t *testing.T) {
// 	instructions := []Instruction{
// 		{Opcode: LABEL, Operands: []byte("L0")},
// 		{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 L1")},
// 		{Opcode: LABEL, Operands: []byte("L1")},
// 	}

// 	expectedLabelPositions := map[string]int{
// 		"L0": 0,
// 		"L1": 2,
// 	}

// 	labelPositions := MapLabels(instructions)
// 	assert.Equal(t, expectedLabelPositions, labelPositions)
// }

// func TestReplaceLabels(t *testing.T) {
// 	instructions := []Instruction{
// 		{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 L0")},
// 		{Opcode: ACTION_START},
// 		{Opcode: LABEL, Operands: []byte("L0")},
// 	}

// 	offsets := map[int]int{
// 		0: 0,
// 		1: 21,
// 		2: 22,
// 	}

// 	labelPositions := map[string]int{
// 		"L0": 2,
// 	}

// 	expectedInstructions := []Instruction{
// 		{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 1")},
// 		{Opcode: ACTION_START},
// 	}

// 	// Replace labels and remove them
// 	replacedInstructions := ReplaceLabels(instructions, offsets, labelPositions)
// 	finalInstructions := RemoveLabels(replacedInstructions)

// 	assert.Equal(t, expectedInstructions, finalInstructions)
// }

func TestRemoveLabels(t *testing.T) {
	instructions := []Instruction{
		{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 1")},
		{Opcode: LABEL, Operands: []byte("L0")},
		{Opcode: ACTION_START},
	}

	expectedInstructions := []Instruction{
		{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 1")},
		{Opcode: ACTION_START},
	}

	finalInstructions := RemoveLabels(instructions)
	assert.Equal(t, expectedInstructions, finalInstructions)
}
