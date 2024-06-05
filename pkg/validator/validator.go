package validator

import (
	"fmt"
	"rgehrsitz/rex/pkg/compiler"
)

func ValidateRule(rule *compiler.Rule) error {
	if len(rule.Conditions.All) == 0 && len(rule.Conditions.Any) == 0 {
		return fmt.Errorf("rule must have at least one condition")
	}

	// Add more validation checks as needed

	return nil
}
