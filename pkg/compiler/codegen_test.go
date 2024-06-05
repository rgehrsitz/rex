// rex/pkg/compiler/codegen_test.go

package compiler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateOffsets(t *testing.T) {
	instructions := []Instruction{
		{Opcode: HEADER_START},
		{Opcode: VERSION, Operands: []byte{1, 0}},
		{Opcode: CONST_POOL_SIZE, Operands: []byte{0, 0}},
		{Opcode: NUM_RULES, Operands: []byte{1}},
		{Opcode: HEADER_END},
		{Opcode: LABEL, Operands: []byte("L0")},
		{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 L1")},
		{Opcode: LABEL, Operands: []byte("L1")},
	}

	expectedOffsets := map[string]int{
		"HEADER_START []":       0,
		"VERSION [1 0]":         1,
		"CONST_POOL_SIZE [0 0]": 4,
		"NUM_RULES [1]":         7,
		"HEADER_END []":         9,
		"LABEL [76 48]":         10,
		"JUMP_IF_FALSE [116 101 109 112 101 114 97 116 117 114 101 32 71 84 32 51 48 32 76 49]": 13,
		"LABEL [76 49]": 34,
	}

	offsets := CalculateOffsets(instructions)
	assert.Equal(t, expectedOffsets, offsets)
}

func TestMapLabels(t *testing.T) {
	instructions := []Instruction{
		{Opcode: LABEL, Operands: []byte("L0")},
		{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 L1")},
		{Opcode: LABEL, Operands: []byte("L1")},
	}

	expectedLabelPositions := map[string]int{
		"L0": 0,
		"L1": 2,
	}

	labelPositions := MapLabels(instructions)
	assert.Equal(t, expectedLabelPositions, labelPositions)
}

func TestReplaceLabels(t *testing.T) {
	instructions := []Instruction{
		{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 L0")},
		{Opcode: ACTION_START},
		{Opcode: LABEL, Operands: []byte("L0")},
	}

	offsets := map[int]int{
		0: 0,
		1: 21,
		2: 22,
	}

	labelPositions := map[string]int{
		"L0": 2,
	}

	expectedInstructions := []Instruction{
		{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 1")},
		{Opcode: ACTION_START},
	}

	// Replace labels and remove them
	replacedInstructions := ReplaceLabels(instructions, offsets, labelPositions)
	finalInstructions := RemoveLabels(replacedInstructions)

	assert.Equal(t, expectedInstructions, finalInstructions)
}

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
