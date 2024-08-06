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
	scripts map[string]compiler.Script
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

	done := make(chan interface{})
	errChan := make(chan error)

	s.vm.Interrupt = make(chan func(), 1)
	defer func() { s.vm.Interrupt = nil }()

	go func() {
		defer close(done)
		defer close(errChan)
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

		exportedResult, err := result.Export()
		if err != nil {
			errChan <- fmt.Errorf("error exporting result: %w", err)
			return
		}
		if floatResult, ok := exportedResult.(float64); ok {
			if math.IsInf(floatResult, 0) || math.IsNaN(floatResult) {
				logging.Logger.Warn().Str("scriptName", name).Float64("result", floatResult).Msg("Script produced Inf or NaN value")
				errChan <- fmt.Errorf("script produced invalid numeric result")
				return
			}
		}
		done <- exportedResult
	}()

	select {
	case result := <-done:
		return result, nil
	case err := <-errChan:
		logging.Logger.Error().Err(err).Str("scriptName", name).Msg("Script execution error")
		return nil, err
	case <-time.After(timeout + 10*time.Millisecond):
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
