// rex/internal/engine/rule_loader.go

package engine

import (
	"encoding/json"
	"os"
	"rgehrsitz/rex/pkg/rule"
)

func LoadRulesFromFile(filePath string) ([]rule.Rule, error) {
	var rules []rule.Rule
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(fileData, &rules)
	if err != nil {
		return nil, err
	}
	return rules, nil
}
