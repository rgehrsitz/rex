// rex/pkg/compiler/bytecode_test.go

package compiler

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriteString(t *testing.T) {
	buf := new(bytes.Buffer)
	testCases := []struct {
		input    string
		expected []byte
	}{
		{"test", []byte{4, 0, 0, 0, 't', 'e', 's', 't'}},
		{"", []byte{0, 0, 0, 0}},
		{"hello world", []byte{11, 0, 0, 0, 'h', 'e', 'l', 'l', 'o', ' ', 'w', 'o', 'r', 'l', 'd'}},
	}

	for _, tc := range testCases {
		buf.Reset()
		err := writeString(buf, tc.input)
		assert.NoError(t, err)
		assert.Equal(t, tc.expected, buf.Bytes())
	}
}

func TestWriteBytecodeToFileEmptyFile(t *testing.T) {
	emptyFile := BytecodeFile{}
	tempFile := "empty_bytecode.bin"
	defer os.Remove(tempFile)

	err := WriteBytecodeToFile(tempFile, emptyFile)
	assert.NoError(t, err)

	// Verify file contents
	content, err := os.ReadFile(tempFile)
	assert.NoError(t, err)
	assert.Len(t, content, HeaderSize) // Only header should be written
}

func TestOpcodeHasOperands(t *testing.T) {
	testCases := []struct {
		opcode   Opcode
		expected bool
	}{
		{LOAD_CONST_FLOAT, true},
		{JUMP_IF_TRUE, true},
		{AND, false},
		{OR, false},
		{LABEL, true},
	}

	for _, tc := range testCases {
		assert.Equal(t, tc.expected, tc.opcode.HasOperands())
	}
}
