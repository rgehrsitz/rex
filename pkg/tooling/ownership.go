package tooling

import (
	"fmt"
	"rgehrsitz/rex/pkg/runtime"
	"rgehrsitz/rex/pkg/store"
)

type OwnershipSpec struct {
	Facts []string `json:"facts"`
}

// CheckOwnership uses the same complete artifact footprint as durable recovery.
func CheckOwnership(artifact []byte, names []string) (LintReport, error) {
	report := LintReport{SchemaVersion: SchemaVersion, Diagnostics: []Diagnostic{}}
	if names == nil {
		return report, fmt.Errorf("ownership facts are required")
	}
	policy, err := store.NewFactOwnership(names)
	if err != nil {
		return report, err
	}
	program, err := runtime.LoadProgram(artifact)
	if err != nil {
		return report, err
	}
	for _, key := range program.OwnershipFacts() {
		if err := policy.Validate(key); err != nil {
			report.Diagnostics = append(report.Diagnostics, Diagnostic{ID: "REX-P001", Severity: "error", Message: err.Error()})
		}
	}
	return report, nil
}
