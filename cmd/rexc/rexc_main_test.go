// rex/cmd/rexc/rexc_main_test.go

package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expected    *Config
		expectError bool
	}{
		{
			name: "Valid flags",
			args: []string{"-rules", "test.json", "-loglevel", "debug", "-logoutput", "file"},
			expected: &Config{
				JSONFilePath: "test.json",
				LogLevel:     "debug",
				LogOutput:    "file",
			},
			expectError: false,
		},
		{
			name:        "Missing rules flag",
			args:        []string{"-loglevel", "info"},
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parseFlags(tt.args)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, config)
			}
		})
	}
}

func TestReadJSONFile(t *testing.T) {
	// Create a temporary JSON file for testing
	tempFile, err := os.CreateTemp("", "test_rules_*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tempFile.Name())

	testJSON := []byte(`{"test": "data"}`)
	if _, err := tempFile.Write(testJSON); err != nil {
		t.Fatal(err)
	}
	tempFile.Close()

	// Test reading the file
	data, err := readJSONFile(tempFile.Name())
	assert.NoError(t, err)
	assert.Equal(t, testJSON, data)

	// Test reading a non-existent file
	_, err = readJSONFile("non_existent_file.json")
	assert.Error(t, err)
}

func TestRun(t *testing.T) {
	// Create a temporary JSON file for testing
	tempFile, err := os.CreateTemp("", "test_rules_*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tempFile.Name())

	testJSON := []byte(`{
		"rules": [
			{
				"name": "test_rule",
				"conditions": {
					"all": [
						{
							"fact": "temperature",
							"operator": "GT",
							"value": 30
						}
					]
				},
				"actions": [
					{
						"type": "updateStore",
						"target": "status",
						"value": "hot"
					}
				]
			}
		]
	}`)
	if _, err := tempFile.Write(testJSON); err != nil {
		t.Fatal(err)
	}
	tempFile.Close()

	config := &Config{
		JSONFilePath: tempFile.Name(),
		LogLevel:     "info",
		LogOutput:    "console",
	}

	// Redirect stdout to capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = run(config)
	assert.NoError(t, err)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Assert expected output
	assert.Contains(t, output, "Generated Bytecode:")

	// Check if output.bytecode file was created
	_, err = os.Stat("output.bytecode")
	assert.NoError(t, err, "output.bytecode file should exist")
	defer os.Remove("output.bytecode")
}

// TestMainFunction tests the overall flow of the main function
func TestMainFunction(t *testing.T) {
	// Create a temporary JSON file for testing
	tempFile, err := os.CreateTemp("", "test_rules_*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tempFile.Name())

	// Write test JSON data to the temporary file
	testJSON := []byte(`{
		"rules": [
			{
				"name": "test_rule",
				"conditions": {
					"all": [
						{
							"fact": "temperature",
							"operator": "GT",
							"value": 30
						}
					]
				},
				"actions": [
					{
						"type": "updateStore",
						"target": "status",
						"value": "hot"
					}
				]
			}
		]
	}`)
	if _, err := tempFile.Write(testJSON); err != nil {
		t.Fatal(err)
	}
	tempFile.Close()

	// Redirect stdout and stderr to capture output
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutR, stdoutW, _ := os.Pipe()
	stderrR, stderrW, _ := os.Pipe()
	os.Stdout = stdoutW
	os.Stderr = stderrW

	// Set up command-line arguments
	oldArgs := os.Args
	os.Args = []string{"cmd", "-rules", tempFile.Name(), "-loglevel", "info", "-logoutput", "console"}

	// Run main function
	err = mainFunc()

	// Close writers and restore stdout, stderr, and command-line arguments
	stdoutW.Close()
	stderrW.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	os.Args = oldArgs

	// Read captured output
	var stdoutBuf, stderrBuf bytes.Buffer
	stdoutBuf.ReadFrom(stdoutR)
	stderrBuf.ReadFrom(stderrR)
	output := stdoutBuf.String()

	// Assert no error occurred
	assert.NoError(t, err)

	// Assert expected output
	assert.Contains(t, output, "Generated Bytecode:")
	assert.Contains(t, output, "Successfully generated bytecode and wrote to file")

	// Check if output.bytecode file was created
	_, err = os.Stat("output.bytecode")
	assert.NoError(t, err, "output.bytecode file should exist")
	defer os.Remove("output.bytecode")
}
