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
	case "explain", "bundle", "simulate", "test", "compare", "lint":
		return true
	}
	return false
}

// RunCLI returns 0 success, 1 input/I/O failure, or 2 a semantic difference,
// scenario failure, lint error, or failed replay. Stdout is always JSON.
func RunCLI(ctx context.Context, args []string, out, diagnostics io.Writer) int {
	if len(args) == 0 || !IsCommand(args[0]) {
		fmt.Fprintln(diagnostics, "expected explain, bundle, simulate, test, compare or lint")
		return 1
	}
	command := args[0]
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(diagnostics)
	rulesPath := fs.String("rules", "", "v4 source JSON")
	artifactPath := fs.String("artifact", "", "v4 compiled artifact (explain)")
	bundlePath := fs.String("bundle", "", "complete replay bundle")
	scenarioPath := fs.String("scenario", "", "scenario JSON (bundle) or suite JSON (test)")
	channels := fs.String("channels", "", "comma-separated input channels (lint)")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(diagnostics, "unexpected positional arguments")
		return 1
	}
	allowed := map[string]map[string]bool{
		"explain": {"rules": true, "artifact": true}, "lint": {"rules": true, "channels": true},
		"bundle": {"rules": true, "scenario": true}, "simulate": {"bundle": true},
		"compare": {"rules": true, "bundle": true}, "test": {"rules": true, "scenario": true},
	}
	invalid := ""
	fs.Visit(func(f *flag.Flag) {
		if !allowed[command][f.Name] {
			invalid = f.Name
		}
	})
	if invalid != "" {
		fmt.Fprintf(diagnostics, "-%s is not supported by %s\n", invalid, command)
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
		if *rulesPath == "" {
			return nil, fmt.Errorf("-rules is required")
		}
		return ReadFile(*rulesPath)
	}
	readBundle := func() (Bundle, error) {
		var b Bundle
		if *bundlePath == "" {
			return b, fmt.Errorf("-bundle is required")
		}
		raw, err := ReadFile(*bundlePath)
		if err != nil {
			return b, err
		}
		err = Decode(raw, &b)
		return b, err
	}
	switch command {
	case "explain":
		if (*rulesPath == "") == (*artifactPath == "") {
			return fail(fmt.Errorf("explain requires exactly one of -rules or -artifact"))
		}
		var artifact []byte
		var err error
		if *artifactPath != "" {
			artifact, err = ReadFile(*artifactPath)
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
		if *channels != "" {
			names = strings.Split(*channels, ",")
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
		raw, err := ReadFile(*scenarioPath)
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
		raw, err := ReadFile(*scenarioPath)
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
	names := map[string]bool{}
	reportBytes := 0
	for _, s := range suite.Scenarios {
		if names[s.Name] || s.Expect == nil || s.Expect.FinalState == nil || s.Expect.Actions == nil {
			return r, fmt.Errorf("scenario %q requires unique name and explicit expected final_state/actions", s.Name)
		}
		names[s.Name] = true
		b, err := NewBundle(source, s)
		if err != nil {
			return r, err
		}
		report, err := Replay(ctx, b)
		if err != nil {
			return r, err
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
		r.Passed = r.Passed && passed
		r.Scenarios = append(r.Scenarios, TestResult{s.Name, passed, mismatch, report})
		entryJSON, err := JSON(r.Scenarios[len(r.Scenarios)-1])
		if err != nil {
			return r, err
		}
		reportBytes += len(entryJSON)
		if reportBytes > MaxDocumentBytes {
			return r, fmt.Errorf("suite report exceeds byte limit")
		}
	}
	return r, nil
}
