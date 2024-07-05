// rex/pkg/compiler/structs.go
package compiler

type Ruleset struct {
	Rules []Rule `json:"rules"`
}

type Rule struct {
	Name       string         `json:"name"`
	Priority   int            `json:"priority"`
	Conditions ConditionGroup `json:"conditions"`
	Actions    []Action       `json:"actions"`
}

type ConditionGroup struct {
	All []*ConditionOrGroup `json:"all,omitempty"`
	Any []*ConditionOrGroup `json:"any,omitempty"`
}

type ConditionOrGroup struct {
	Fact     string              `json:"fact,omitempty"`
	Operator string              `json:"operator,omitempty"`
	Value    interface{}         `json:"value,omitempty"`
	All      []*ConditionOrGroup `json:"all,omitempty"`
	Any      []*ConditionOrGroup `json:"any,omitempty"`
}

type Action struct {
	Type   string      `json:"type"`
	Target string      `json:"target"`
	Value  interface{} `json:"value"`
}

type Header struct {
	Version             uint32
	Checksum            uint32
	ConstPoolSize       uint32
	NumRules            uint32
	RuleExecIndexOffset uint32
	FactRuleIndexOffset uint32
	FactDepIndexOffset  uint32
}

type RuleExecutionIndex struct {
	RuleNameLength uint32
	RuleName       string
	ByteOffset     int
	Priority       int
}

type FactRuleLookupIndex struct {
	FactName string
	Rules    []string
}

type FactDependencyIndex struct {
	RuleNameLength uint32
	RuleName       string
	Facts          []string
}
