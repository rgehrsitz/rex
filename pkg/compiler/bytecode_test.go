// rex/pkg/compiler/bytecode_test.go

package compiler

import (
	"bytes"
	"encoding/binary"
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

func TestWriteBytecodeToFileWritesChecksum(t *testing.T) {
	filename := t.TempDir() + "/checksum.bytecode"
	bytecodeFile := BytecodeFile{
		Header: Header{Version: Version},
		Instructions: []byte{
			byte(RULE_START), 4, 'r', 'u', 'l', 'e',
			byte(PRIORITY), 0, 0, 0, 0,
			byte(RULE_END),
		},
	}

	assert.NoError(t, WriteBytecodeToFile(filename, bytecodeFile))
	data, err := os.ReadFile(filename)
	assert.NoError(t, err)

	checksum := binary.LittleEndian.Uint32(data[ChecksumOffset : ChecksumOffset+ChecksumSize])
	assert.NotZero(t, checksum)
	assert.Equal(t, checksum, CalculateBytecodeChecksum(data))

	headerTampered := append([]byte(nil), data...)
	headerTampered[0] ^= 0xff
	assert.NotEqual(t, checksum, CalculateBytecodeChecksum(headerTampered))

	payloadTampered := append([]byte(nil), data...)
	payloadTampered[HeaderSize] ^= 0xff
	assert.NotEqual(t, checksum, CalculateBytecodeChecksum(payloadTampered))
}

func TestCalculateBytecodeChecksumRejectsShortData(t *testing.T) {
	assert.Zero(t, CalculateBytecodeChecksum([]byte{0, 1, 2}))
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
