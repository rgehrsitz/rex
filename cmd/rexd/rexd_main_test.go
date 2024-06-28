// rex/cmd/rexd/rexd_main_test.go

package main

import (
	"encoding/binary"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func createMockBytecodeFile(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write header
	binary.Write(file, binary.LittleEndian, uint32(1))  // Version
	binary.Write(file, binary.LittleEndian, uint32(0))  // Checksum
	binary.Write(file, binary.LittleEndian, uint32(0))  // ConstPoolSize
	binary.Write(file, binary.LittleEndian, uint32(1))  // NumRules
	binary.Write(file, binary.LittleEndian, uint32(28)) // RuleExecIndexOffset
	binary.Write(file, binary.LittleEndian, uint32(44)) // FactRuleIndexOffset
	binary.Write(file, binary.LittleEndian, uint32(68)) // FactDepIndexOffset

	// Write mock instructions (just placeholders)
	file.Write(make([]byte, 1000)) // Write 1000 bytes of placeholder instructions

	// Write mock rule execution index
	binary.Write(file, binary.LittleEndian, uint32(4)) // RuleNameLength
	file.Write([]byte("rule"))                         // RuleName
	binary.Write(file, binary.LittleEndian, uint32(0)) // ByteOffset

	// Write mock fact rule index
	binary.Write(file, binary.LittleEndian, uint32(4)) // FactNameLength
	file.Write([]byte("fact"))                         // FactName
	binary.Write(file, binary.LittleEndian, uint32(1)) // RulesCount
	binary.Write(file, binary.LittleEndian, uint32(4)) // RuleNameLength
	file.Write([]byte("rule"))                         // RuleName

	// Write mock fact dependency index
	binary.Write(file, binary.LittleEndian, uint32(4)) // RuleNameLength
	file.Write([]byte("rule"))                         // RuleName
	binary.Write(file, binary.LittleEndian, uint32(1)) // FactsCount
	binary.Write(file, binary.LittleEndian, uint32(4)) // FactNameLength
	file.Write([]byte("fact"))                         // FactName

	return nil
}

func TestMain(t *testing.T) {
	// Create a temporary config file for testing
	tempFile, err := ioutil.TempFile("", "test_config*.json")
	assert.NoError(t, err)
	defer os.Remove(tempFile.Name())

	// Write some test config data
	testConfig := `{
		"bytecode_file": "test_bytecode.bin",
		"logging": {
			"level": "debug",
			"destination": "console",
			"timeFormat": "Unix"
		},
		"redis": {
			"address": "localhost:6379",
			"password": "",
			"database": 0,
			"channels": ["test_channel"]
		},
		"engine": {
			"update_interval": 1
		},
		"dashboard": {
			"enabled": false,
			"port": 8080,
			"update_interval": 1
		}
	}`
	_, err = tempFile.Write([]byte(testConfig))
	assert.NoError(t, err)
	tempFile.Close()

	// Set up command-line arguments
	os.Args = []string{"rexd", "-config", tempFile.Name()}

	// Create a mock bytecode file
	err = createMockBytecodeFile("test_bytecode.bin")
	assert.NoError(t, err)
	defer os.Remove("test_bytecode.bin")

	// Redirect stdout and stderr
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stdout = w
	os.Stderr = w

	// Run the main function in a goroutine
	done := make(chan bool)
	go func() {
		main()
		done <- true
	}()

	// Wait for a short time to allow the program to start
	select {
	case <-done:
		// If main() returns quickly, it probably encountered an error
		t.Fatal("Main function returned unexpectedly")
	case <-time.After(500 * time.Millisecond):
		// This is the expected path - main() should still be running
	}

	// Restore stdout and stderr
	w.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	// Read the output
	out, _ := ioutil.ReadAll(r)
	output := string(out)

	// Check if the output contains expected messages
	assert.Contains(t, output, "REX runtime engine started")

}
