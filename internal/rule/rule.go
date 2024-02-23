package rule

type Rule struct {
	Name          string     `json:"name"`
	Priority      int        `json:"priority"`
	Conditions    Conditions `json:"conditions"`
	Event         Event      `json:"event"`
	ProducedFacts []string   `json:"producedFacts,omitempty"` // Facts produced by this rule
	ConsumedFacts []string   `json:"consumedFacts,omitempty"` // Facts consumed by this rule
}

type Event struct {
	EventType      string        `json:"eventType"`
	CustomProperty interface{}   `json:"customProperty"`
	Facts          []string      `json:"facts,omitempty"`
	Values         []interface{} `json:"values,omitempty"`
	Actions        []Action      `json:"actions,omitempty"`
}

type Action struct {
	Type   string      `json:"type"`   // "updateStore" or "sendMessage"
	Target string      `json:"target"` // Key for store update or address for message
	Value  interface{} `json:"value"`  // Value for store update or message content
}

type Conditions struct {
	All []Condition `json:"all"`
	Any []Condition `json:"any,omitempty"` // `omitempty` will omit this if nil or empty
}

type Condition struct {
	Fact      string      `json:"fact,omitempty"`
	Operator  string      `json:"operator,omitempty"`
	ValueType string      `json:"valueType,omitempty"` // "string", "int", "float", "bool", "datetime"
	Value     interface{} `json:"value,omitempty"`
	All       []Condition `json:"all,omitempty"`
	Any       []Condition `json:"any,omitempty"`
}

// Operator represents the type of comparison or logical operation to be performed.
type Operator string

const (
	Equal              Operator = "equal"
	NotEqual           Operator = "notEqual"
	GreaterThan        Operator = "greaterThan"
	GreaterThanOrEqual Operator = "greaterThanOrEqual"
	LessThan           Operator = "lessThan"
	LessThanOrEqual    Operator = "lessThanOrEqual"
	Contains           Operator = "contains"
	NotContains        Operator = "notContains"
)
