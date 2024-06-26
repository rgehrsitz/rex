package compiler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConvertConditionGroupToNode(t *testing.T) {
	tests := []struct {
		name     string
		input    ConditionGroup
		expected Node
	}{
		{
			name: "Single All Condition",
			input: ConditionGroup{
				All: []*ConditionOrGroup{
					{Fact: "temperature", Operator: "GT", Value: 30.0},
				},
			},
			expected: Node{
				All: []Node{
					{Cond: &Condition{Fact: "temperature", Operator: "GT", Value: 30.0}},
				},
			},
		},
		{
			name: "Multiple Any Conditions",
			input: ConditionGroup{
				Any: []*ConditionOrGroup{
					{Fact: "temperature", Operator: "GT", Value: 30.0},
					{Fact: "humidity", Operator: "LT", Value: 50.0},
				},
			},
			expected: Node{
				Any: []Node{
					{Cond: &Condition{Fact: "temperature", Operator: "GT", Value: 30.0}},
					{Cond: &Condition{Fact: "humidity", Operator: "LT", Value: 50.0}},
				},
			},
		},
		{
			name: "Nested Conditions",
			input: ConditionGroup{
				All: []*ConditionOrGroup{
					{Fact: "temperature", Operator: "GT", Value: 30.0},
					{
						Any: []*ConditionOrGroup{
							{Fact: "humidity", Operator: "LT", Value: 50.0},
							{Fact: "pressure", Operator: "GT", Value: 1000.0},
						},
					},
				},
			},
			expected: Node{
				All: []Node{
					{Cond: &Condition{Fact: "temperature", Operator: "GT", Value: 30.0}},
					{
						Any: []Node{
							{Cond: &Condition{Fact: "humidity", Operator: "LT", Value: 50.0}},
							{Cond: &Condition{Fact: "pressure", Operator: "GT", Value: 1000.0}},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertConditionGroupToNode(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateInstructions(t *testing.T) {
	tests := []struct {
		name            string
		input           Node
		expectedOpcodes []Opcode
	}{
		{
			name: "Single Condition",
			input: Node{
				All: []Node{
					{Cond: &Condition{Fact: "temperature", Operator: "GT", Value: 30.0}},
				},
			},
			expectedOpcodes: []Opcode{JUMP_IF_FALSE, JUMP_IF_TRUE, LABEL, LABEL},
		},
		{
			name: "Multiple Any Conditions",
			input: Node{
				Any: []Node{
					{Cond: &Condition{Fact: "temperature", Operator: "GT", Value: 30.0}},
					{Cond: &Condition{Fact: "humidity", Operator: "LT", Value: 50.0}},
				},
			},
			expectedOpcodes: []Opcode{JUMP_IF_FALSE, JUMP_IF_TRUE, LABEL, JUMP_IF_FALSE, JUMP_IF_TRUE, LABEL, LABEL},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instructions := generateInstructions(tt.input, "L")
			opcodes := make([]Opcode, len(instructions))
			for i, instr := range instructions {
				opcodes[i] = instr.Opcode
			}
			assert.Equal(t, tt.expectedOpcodes, opcodes)
		})
	}
}

func TestOptimizeInstructionsTraverse(t *testing.T) {
	tests := []struct {
		name     string
		input    []Instruction
		expected []Instruction
	}{
		{
			name: "Remove Unnecessary Jump",
			input: []Instruction{
				{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 L001")},
				{Opcode: JUMP_IF_TRUE, Operands: []byte("L002")},
				{Opcode: LABEL, Operands: []byte("L002")},
			},
			expected: []Instruction{
				{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 L001")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := OptimizeInstructions(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCombineJIFJIT(t *testing.T) {
	tests := []struct {
		name     string
		input    []Instruction
		expected []Instruction
	}{
		{
			name: "Combine JIF and JIT",
			input: []Instruction{
				{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 L001")},
				{Opcode: JUMP_IF_TRUE, Operands: []byte("L002")},
			},
			expected: []Instruction{
				{Opcode: JUMP_IF_TRUE, Operands: []byte("temperature GT 30 L002")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CombineJIFJIT(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRemoveUnusedLabels(t *testing.T) {
	tests := []struct {
		name     string
		input    []Instruction
		expected []Instruction
	}{
		{
			name: "Remove Unused Label",
			input: []Instruction{
				{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 L001")},
				{Opcode: LABEL, Operands: []byte("L001")},
				{Opcode: LABEL, Operands: []byte("L002")},
			},
			expected: []Instruction{
				{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 L001")},
				{Opcode: LABEL, Operands: []byte("L001")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RemoveUnusedLabels(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
