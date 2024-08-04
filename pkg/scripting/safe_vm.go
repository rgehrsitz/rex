// rex/pkg/scripting/safe_vm.go

package scripting

import (
	"fmt"
	"math"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/logging"
	"strings"
	"time"

	"github.com/robertkrimen/otto"
)

type SafeVM struct {
	vm      *otto.Otto
	scripts map[string]compiler.Script // Change this to compiler.Script
}

func NewSafeVM() *SafeVM {
	vm := otto.New()

	// Limit available globals
	if mathObj, err := vm.Get("Math"); err == nil {
		vm.Set("Math", mathObj)
	}
	if dateObj, err := vm.Get("Date"); err == nil {
		vm.Set("Date", dateObj)
	}

	// Remove potentially dangerous functions
	vm.Set("eval", otto.UndefinedValue())
	vm.Set("Function", otto.UndefinedValue())

	return &SafeVM{
		vm:      vm,
		scripts: make(map[string]compiler.Script),
	}
}

func (s *SafeVM) SetScript(name string, script compiler.Script) error {
	logging.Logger.Debug().Str("scriptName", name).Msg("Setting script")
	s.scripts[name] = script
	return nil
}

func (s *SafeVM) RunScript(name string, params map[string]interface{}, timeout time.Duration) (interface{}, error) {
	script, ok := s.scripts[name]
	if !ok {
		logging.Logger.Error().Str("scriptName", name).Msg("Script not found")
		return nil, fmt.Errorf("script not found: %s", name)
	}

	logging.Logger.Debug().Str("scriptName", name).Interface("params", params).Msg("Running script")

	funcDef := fmt.Sprintf("(function(%s) { %s })", strings.Join(script.Params, ","), script.Body)

	logging.Logger.Debug().Str("scriptName", name).Str("funcDef", funcDef).Msg("Defined function")

	resultChan := make(chan otto.Value, 1)
	errChan := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				if r == "Execution timeout" {
					errChan <- fmt.Errorf("script execution timed out")
				} else {
					errChan <- fmt.Errorf("script panicked: %v", r)
				}
			}
		}()

		s.vm.SetStackDepthLimit(1000)

		value, err := s.vm.Eval(funcDef)
		if err != nil {
			errChan <- fmt.Errorf("error evaluating function: %w", err)
			return
		}

		args := make([]interface{}, len(script.Params))
		for i, param := range script.Params {
			args[i] = params[param]
		}

		logging.Logger.Debug().Str("scriptName", name).Interface("args", args).Msg("Calling function with arguments")

		result, err := value.Call(otto.NullValue(), args...)
		if err != nil {
			errChan <- err
			return
		}

		resultChan <- result
	}()

	s.vm.Interrupt = make(chan func(), 1)

	go func() {
		time.Sleep(timeout)
		s.vm.Interrupt <- func() {
			panic("Execution timeout")
		}
	}()

	select {
	case result := <-resultChan:
		s.vm.Interrupt = nil
		exportedResult, err := result.Export()
		if err != nil {
			return nil, fmt.Errorf("error exporting result: %w", err)
		}
		if floatResult, ok := exportedResult.(float64); ok {
			if math.IsInf(floatResult, 0) || math.IsNaN(floatResult) {
				logging.Logger.Warn().Str("scriptName", name).Float64("result", floatResult).Msg("Script produced Inf or NaN value")
				return nil, fmt.Errorf("script produced invalid numeric result")
			}
		}
		logging.Logger.Debug().Str("scriptName", name).Interface("exportedResult", exportedResult).Msg("Script result")
		return exportedResult, nil
	case err := <-errChan:
		s.vm.Interrupt = nil
		logging.Logger.Error().Err(err).Str("scriptName", name).Msg("Script execution error")
		return nil, err
	case <-time.After(timeout + 10*time.Millisecond):
		s.vm.Interrupt = nil
		logging.Logger.Error().Str("scriptName", name).Msg("Script execution timed out")
		return nil, fmt.Errorf("script execution timed out")
	}
}

func (s *SafeVM) RegisterGlobalFunction(name string, script compiler.Script) error {
	funcDef := fmt.Sprintf("function %s(%s) { %s }", name, strings.Join(script.Params, ","), script.Body)
	_, err := s.vm.Run(funcDef)
	if err != nil {
		return fmt.Errorf("failed to register global function: %w", err)
	}
	return nil
}
