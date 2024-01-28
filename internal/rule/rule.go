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
	Any []Condition `json:"any"`
}

type Condition struct {
	Fact     string      `json:"fact,omitempty"`
	Operator string      `json:"operator,omitempty"`
	Value    interface{} `json:"value,omitempty"`
	All      []Condition `json:"all,omitempty"`
	Any      []Condition `json:"any,omitempty"`
}
