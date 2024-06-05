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
	Version       uint16 // Version of the bytecode spec
	Checksum      uint32 // Checksum for integrity verification
	ConstPoolSize uint16 // Size of the constant pool
	NumRules      uint16 // Number of rules in the bytecode
	// ... other metadata fields
}

type RuleExecutionIndex struct {
	RuleName   string
	ByteOffset int
}

type FactRuleLookupIndex struct {
	FactName string
	Rules    []string
}

type FactDependencyIndex struct {
	RuleName string
	Facts    []string
}
