// rex/tools/rule_gen/main.go

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

var operators = map[string][]string{
	"numeric": {"EQ", "NEQ", "LT", "LTE", "GT", "GTE"},
	"boolean": {"EQ", "NEQ"},
	"string":  {"EQ", "NEQ", "CONTAINS", "NOT_CONTAINS"},
}

func main() {
	numRules, outputFile := parseFlags(os.Args[1:])
	ruleset := generateRuleset(numRules)
	err := writeRulesetToFile(ruleset, outputFile)
	if err != nil {
		fmt.Printf("Error writing ruleset to file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Generated ruleset with %d rules. Saved to %s\n", numRules, outputFile)
}

func parseFlags(args []string) (int, string) {
	fs := flag.NewFlagSet("rule_gen", flag.ExitOnError)
	numRules := fs.Int("rules", 1000, "Number of rules to generate")
	outputFile := fs.String("output", "generated_ruleset.json", "Output file name")
	fs.Parse(args)
	return *numRules, *outputFile
}

func generateRuleset(numRules int) Ruleset {
	gofakeit.Seed(time.Now().UnixNano())
	ruleset := Ruleset{Rules: make([]Rule, numRules)}
	for i := range ruleset.Rules {
		ruleset.Rules[i] = generateRule(i + 1)
	}
	return ruleset
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

func generateCondition(depth int) Condition {
	if depth > 2 || rand.Float32() < 0.7 {
		channel, fact := getRandomFact()
		factType := getFactType(fact)
		return Condition{
			Fact:     fmt.Sprintf("%s:%s", channel, fact),
			Operator: operators[factType][rand.Intn(len(operators[factType]))],
			Value:    generateValue(factType),
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

	channel, fact := getRandomFact()
	factType := getFactType(fact)

	return Action{
		Type:   actionType,
		Target: fmt.Sprintf("%s:%s", channel, fact),
		Value:  generateValue(factType),
	}
}

func getRandomFact() (string, string) {
	channels := make([]string, 0, len(channelFacts))
	for channel := range channelFacts {
		channels = append(channels, channel)
	}

	channel := channels[rand.Intn(len(channels))]
	facts := channelFacts[channel]
	fact := facts[rand.Intn(len(facts))]

	return channel, fact
}

func getFactType(fact string) string {
	numericFacts := []string{"temperature", "humidity", "pressure", "wind_speed", "rainfall", "solar_radiation",
		"speed", "capacity", "latency", "packet_loss", "bandwidth_usage", "connection_count",
		"cpu_usage", "memory_usage", "disk_space", "process_count", "uptime", "load_average", "fan_speed",
		"voltage", "current", "power", "energy_consumption", "power_factor", "frequency", "harmonic_distortion",
		"ph", "conductivity", "turbidity", "dissolved_oxygen", "flow_rate"}

	booleanFacts := []string{"temperature_warning", "humidity_warning", "fault_status"}

	for _, f := range numericFacts {
		if f == fact {
			return "numeric"
		}
	}
	for _, f := range booleanFacts {
		if f == fact {
			return "boolean"
		}
	}
	return "string"
}

func generateValue(factType string) interface{} {
	switch factType {
	case "numeric":
		return gofakeit.Float64Range(-100, 100)
	case "boolean":
		return gofakeit.Bool()
	default:
		return gofakeit.Word()
	}
}

func writeRulesetToFile(ruleset Ruleset, outputFile string) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("error creating file: %v", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(ruleset); err != nil {
		return fmt.Errorf("error encoding JSON: %v", err)
	}
	return nil
}
