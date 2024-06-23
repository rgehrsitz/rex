package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/brianvoe/gofakeit/v7"
)

type Rule struct {
	Name       string         `json:"name"`
	Priority   int            `json:"priority,omitempty"`
	Conditions ConditionGroup `json:"conditions"`
	Actions    []Action       `json:"actions"`
}

type ConditionGroup struct {
	All []Condition `json:"all,omitempty"`
	Any []Condition `json:"any,omitempty"`
}

type Condition struct {
	Fact     string      `json:"fact,omitempty"`
	Operator string      `json:"operator,omitempty"`
	Value    interface{} `json:"value,omitempty"`
	All      []Condition `json:"all,omitempty"`
	Any      []Condition `json:"any,omitempty"`
}

type Action struct {
	Type   string      `json:"type"`
	Target string      `json:"target"`
	Value  interface{} `json:"value"`
}

type Ruleset struct {
	Rules []Rule `json:"rules"`
}

var channelFacts = map[string][]string{
	"weather": {
		"temperature", "humidity", "pressure", "wind_speed", "wind_direction",
		"rainfall", "solar_radiation", "temperature_warning", "humidity_warning",
	},
	"network": {
		"speed", "capacity", "fault_status", "latency", "packet_loss",
		"bandwidth_usage", "connection_count", "error_rate",
	},
	"system": {
		"cpu_usage", "memory_usage", "disk_space", "process_count",
		"uptime", "load_average", "temperature", "fan_speed",
	},
	"energy": {
		"voltage", "current", "power", "energy_consumption",
		"power_factor", "frequency", "harmonic_distortion",
	},
	"water": {
		"ph", "conductivity", "turbidity", "dissolved_oxygen",
		"flow_rate", "pressure", "temperature", "chlorine_level",
	},
}

var operators = []string{
	"EQ", "NEQ", "LT", "LTE", "GT", "GTE", "CONTAINS", "NOT_CONTAINS",
}

func getRandomFact() string {
	channels := make([]string, 0, len(channelFacts))
	for channel := range channelFacts {
		channels = append(channels, channel)
	}

	channel := channels[rand.Intn(len(channels))]
	facts := channelFacts[channel]
	fact := facts[rand.Intn(len(facts))]

	return fmt.Sprintf("%s:%s", channel, fact)
}

func generateValue() interface{} {
	switch rand.Intn(3) {
	case 0:
		return gofakeit.Float64Range(-100, 100)
	case 1:
		return gofakeit.Bool()
	default:
		return gofakeit.Word()
	}
}

func generateCondition(depth int) Condition {
	if depth > 2 || rand.Float32() < 0.7 {
		return Condition{
			Fact:     getRandomFact(),
			Operator: operators[rand.Intn(len(operators))],
			Value:    generateValue(),
		}
	}

	var subConditions []Condition
	numSubConditions := rand.Intn(3) + 1
	for i := 0; i < numSubConditions; i++ {
		subConditions = append(subConditions, generateCondition(depth+1))
	}

	if rand.Float32() < 0.5 {
		return Condition{All: subConditions}
	} else {
		return Condition{Any: subConditions}
	}
}

func generateAction() Action {
	actionType := "updateStore"
	if rand.Float32() < 0.3 {
		actionType = "sendMessage"
	}

	return Action{
		Type:   actionType,
		Target: getRandomFact(),
		Value:  generateValue(),
	}
}

func generateRule(index int) Rule {
	var conditions ConditionGroup
	if rand.Float32() < 0.5 {
		conditions.All = []Condition{generateCondition(0)}
	} else {
		conditions.Any = []Condition{generateCondition(0)}
	}

	numActions := rand.Intn(2) + 1
	actions := make([]Action, numActions)
	for i := range actions {
		actions[i] = generateAction()
	}

	return Rule{
		Name:       fmt.Sprintf("rule-%d", index),
		Priority:   rand.Intn(20) + 1,
		Conditions: conditions,
		Actions:    actions,
	}
}

func main() {
	numRules := flag.Int("rules", 1000, "Number of rules to generate")
	outputFile := flag.String("output", "generated_ruleset.json", "Output file name")
	flag.Parse()

	gofakeit.Seed(time.Now().UnixNano())

	ruleset := Ruleset{Rules: make([]Rule, *numRules)}
	for i := range ruleset.Rules {
		ruleset.Rules[i] = generateRule(i + 1)
	}

	file, err := os.Create(*outputFile)
	if err != nil {
		fmt.Printf("Error creating file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(ruleset); err != nil {
		fmt.Printf("Error encoding JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated ruleset with %d rules. Saved to %s\n", *numRules, *outputFile)
}