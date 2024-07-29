// rex/pkg/scripting/safe_vm_test.go

package scripting

import (
	"testing"
	"time"

	"rgehrsitz/rex/pkg/compiler"

	"github.com/stretchr/testify/assert"
)

func TestNewSafeVM(t *testing.T) {
	vm := NewSafeVM()
	assert.NotNil(t, vm)
	assert.NotNil(t, vm.vm)
	assert.NotNil(t, vm.scripts)
}

func TestSetScript(t *testing.T) {
	vm := NewSafeVM()
	script := compiler.Script{
		Params: []string{"a", "b"},
		Body:   "return a + b;",
	}
	err := vm.SetScript("test", script)
	assert.NoError(t, err)
	assert.Contains(t, vm.scripts, "test")

	// Verify that the script was stored correctly
	storedScript, exists := vm.scripts["test"]
	assert.True(t, exists)
	assert.Equal(t, script, storedScript)
}

func TestRunScript(t *testing.T) {
	vm := NewSafeVM()
	script := compiler.Script{
		Params: []string{"a", "b"},
		Body:   "return a + b;",
	}
	err := vm.SetScript("add", script)
	assert.NoError(t, err)

	result, err := vm.RunScript("add", map[string]interface{}{"a": 5, "b": 3}, 100*time.Millisecond)
	assert.NoError(t, err)
	assert.Equal(t, float64(8), result)
}

func TestRunScriptTimeout(t *testing.T) {
	vm := NewSafeVM()
	script := compiler.Script{
		Params: []string{},
		Body:   "while(true) {}", // Infinite loop
	}
	err := vm.SetScript("infinite", script)
	assert.NoError(t, err)

	_, err = vm.RunScript("infinite", nil, 100*time.Millisecond)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "script execution timed out")
}

func TestRunNonExistentScript(t *testing.T) {
	vm := NewSafeVM()
	_, err := vm.RunScript("nonexistent", nil, 100*time.Millisecond)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "script not found")
}

func TestRunInvalidScript(t *testing.T) {
	vm := NewSafeVM()
	script := compiler.Script{
		Params: []string{"a"},
		Body:   "return b;", // 'b' is not defined
	}
	err := vm.SetScript("invalid", script)
	assert.NoError(t, err)

	_, err = vm.RunScript("invalid", map[string]interface{}{"a": 5}, 100*time.Millisecond)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ReferenceError") // Otto should throw a ReferenceError for undefined variables
}
