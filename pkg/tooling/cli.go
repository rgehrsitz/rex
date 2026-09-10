package tooling

import (
	"context"
	"flag"
	"fmt"
	"io"
	"reflect"
	"strings"

	"rgehrsitz/rex/pkg/compiler"
)

func IsCommand(command string) bool {
	switch command {
	case "explain", "bundle", "simulate", "test", "compare", "lint", "partition-plan":
		return true
	}
	return false
}

// RunCLI returns 0 success, 1 input/I/O failure, or 2 a semantic difference,
// scenario failure, lint error, or failed replay. Stdout is always JSON.
func RunCLI(ctx context.Context, args []string, out, diagnostics io.Writer) int {
	if len(args) == 0 || !IsCommand(args[0]) {
		fmt.Fprintln(diagnostics, "expected explain, bundle, simulate, test, compare, lint or partition-plan")
		return 1
	}
	command := args[0]
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(diagnostics)
	var rulesPath, artifactPath, bundlePath, scenarioPath, channels string
	switch command {
	case "explain", "partition-plan":
		fs.StringVar(&rulesPath, "rules", "", "batch v4-v7 source JSON")
		fs.StringVar(&artifactPath, "artifact", "", "batch v4-v7 compiled artifact")
	case "lint":
		fs.StringVar(&rulesPath, "rules", "", "batch v4-v7 source JSON")
		fs.StringVar(&channels, "channels", "", "comma-separated input channels")
	case "bundle", "test":
		fs.StringVar(&rulesPath, "rules", "", "batch v4-v7 source JSON")
		fs.StringVar(&scenarioPath, "scenario", "", "scenario or suite JSON")
	case "simulate":
		fs.StringVar(&bundlePath, "bundle", "", "complete replay bundle")
	case "compare":
		fs.StringVar(&rulesPath, "rules", "", "candidate batch v4-v7 source JSON")
		fs.StringVar(&bundlePath, "bundle", "", "complete replay bundle")
	}
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(diagnostics, "unexpected positional arguments")
		return 1
	}
	fail := func(err error) int { fmt.Fprintln(diagnostics, err); return 1 }
	emit := func(v interface{}, code int) int {
		data, err := JSON(v)
		if err != nil {
			return fail(err)
		}
		if _, err = out.Write(data); err != nil {
			return fail(err)
		}
		return code
	}
	readSource := func() ([]byte, error) {
		if rulesPath == "" {
			return nil, fmt.Errorf("-rules is required")
		}
		return ReadFile(rulesPath)
	}
	readBundle := func() (Bundle, error) {
		var b Bundle
		if bundlePath == "" {
			return b, fmt.Errorf("-bundle is required")
		}
		raw, err := ReadFile(bundlePath)
		if err != nil {
			return b, err
		}
		err = Decode(raw, &b)
		return b, err
	}
	switch command {
	case "explain", "partition-plan":
		if (rulesPath == "") == (artifactPath == "") {
			return fail(fmt.Errorf("%s requires exactly one of -rules or -artifact", command))
		}
		var artifact []byte
		var err error
		if artifactPath != "" {
			artifact, err = ReadFile(artifactPath)
		} else {
			var source []byte
			source, err = readSource()
			if err == nil {
				artifact, err = compiler.CompileBatch(source)
			}
		}
		if err != nil {
			return fail(err)
		}
		if command == "partition-plan" {
			value, err := PlanPartitions(artifact)
			if err != nil {
				return fail(err)
			}
			return emit(value, 0)
		}
		value, err := Explain(artifact)
		if err != nil {
			return fail(err)
		}
		return emit(value, 0)
	case "lint":
		source, err := readSource()
		if err != nil {
			return fail(err)
		}
		var names []string
		if channels != "" {
			names = strings.Split(channels, ",")
		}
		r := Lint(source, names)
		code := 0
		if HasLintErrors(r) {
			code = 2
		}
		return emit(r, code)
	case "bundle":
		source, err := readSource()
		if err != nil {
			return fail(err)
		}
		if scenarioPath == "" {
			return fail(fmt.Errorf("-scenario is required"))
		}
		raw, err := ReadFile(scenarioPath)
		if err != nil {
			return fail(err)
		}
		var scenario Scenario
		if err = Decode(raw, &scenario); err != nil {
			return fail(err)
		}
		b, err := NewBundle(source, scenario)
		if err != nil {
			return fail(err)
		}
		return emit(b, 0)
	case "simulate":
		b, err := readBundle()
		if err != nil {
			return fail(err)
		}
		r, err := Replay(ctx, b)
		if err != nil {
			return fail(err)
		}
		code := 0
		if r.Error != "" {
			code = 2
		}
		return emit(r, code)
	case "compare":
		b, err := readBundle()
		if err != nil {
			return fail(err)
		}
		source, err := readSource()
		if err != nil {
			return fail(err)
		}
		r, err := Compare(ctx, b, source)
		if err != nil {
			return fail(err)
		}
		code := 0
		if r.Changed || r.Before.Error != "" || r.After.Error != "" {
			code = 2
		}
		return emit(r, code)
	case "test":
		source, err := readSource()
		if err != nil {
			return fail(err)
		}
		if scenarioPath == "" {
			return fail(fmt.Errorf("-scenario is required"))
		}
		raw, err := ReadFile(scenarioPath)
		if err != nil {
			return fail(err)
		}
		var suite Suite
		if err = Decode(raw, &suite); err != nil {
			return fail(err)
		}
		r, err := Test(ctx, source, suite)
		if err != nil {
			return fail(err)
		}
		code := 0
		if !r.Passed {
			code = 2
		}
		return emit(r, code)
	}
	return 1
}

type Suite struct {
	SchemaVersion int        `json:"schema_version"`
	Scenarios     []Scenario `json:"scenarios"`
}
type TestResult struct {
	Name       string   `json:"name"`
	Passed     bool     `json:"passed"`
	Mismatches []string `json:"mismatches"`
	Error      string   `json:"error,omitempty"`
	Report     Report   `json:"report"`
}
type TestReport struct {
	SchemaVersion int          `json:"schema_version"`
	Passed        bool         `json:"passed"`
	Scenarios     []TestResult `json:"scenarios"`
}

func Test(ctx context.Context, source []byte, suite Suite) (TestReport, error) {
	r := TestReport{SchemaVersion: SchemaVersion, Passed: true, Scenarios: []TestResult{}}
	if suite.SchemaVersion != SchemaVersion || len(suite.Scenarios) == 0 || len(suite.Scenarios) > 100 {
		return r, fmt.Errorf("suite requires schema 1 and 1..100 named scenarios")
	}
	if _, err := compiler.CompileBatch(source); err != nil {
		return r, err
	}
	names := map[string]bool{}
	reportBytes := 0
	appendResult := func(result TestResult) error {
		r.Passed = r.Passed && result.Passed
		r.Scenarios = append(r.Scenarios, result)
		entryJSON, err := JSON(result)
		if err != nil {
			return err
		}
		reportBytes += len(entryJSON)
		if reportBytes > MaxDocumentBytes {
			return fmt.Errorf("suite report exceeds byte limit")
		}
		return nil
	}
	for i, s := range suite.Scenarios {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("scenario[%d]", i)
		}
		validationError := ""
		switch {
		case s.Name == "":
			validationError = "scenario name is required"
		case names[s.Name]:
			validationError = fmt.Sprintf("duplicate scenario name %q", s.Name)
		case s.Expect == nil || s.Expect.FinalState == nil || s.Expect.Actions == nil:
			validationError = "explicit expected final_state and actions are required"
		}
		if validationError != "" {
			if err := appendResult(TestResult{Name: name, Passed: false, Mismatches: []string{"validation"}, Error: validationError}); err != nil {
				return r, err
			}
			continue
		}
		names[s.Name] = true
		b, err := NewBundle(source, s)
		if err != nil {
			if err := appendResult(TestResult{Name: name, Passed: false, Mismatches: []string{"validation"}, Error: err.Error()}); err != nil {
				return r, err
			}
			continue
		}
		report, err := Replay(ctx, b)
		if err != nil {
			if err := appendResult(TestResult{Name: name, Passed: false, Mismatches: []string{"replay"}, Error: err.Error()}); err != nil {
				return r, err
			}
			continue
		}
		mismatch := []string{}
		if !reflect.DeepEqual(s.Expect.FinalState, report.FinalState) {
			mismatch = append(mismatch, "final_state")
		}
		if !reflect.DeepEqual(s.Expect.Actions, Actions(report)) {
			mismatch = append(mismatch, "actions")
		}
		if (s.Expect.ErrorContains == "" && report.Error != "") || (s.Expect.ErrorContains != "" && !strings.Contains(report.Error, s.Expect.ErrorContains)) {
			mismatch = append(mismatch, "error")
		}
		passed := len(mismatch) == 0
		if err := appendResult(TestResult{Name: s.Name, Passed: passed, Mismatches: mismatch, Report: report}); err != nil {
			return r, err
		}
	}
	return r, nil
}
