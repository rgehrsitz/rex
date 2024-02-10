package evaluator

import (
	"rgehrsitz/rex/internal/rule"
	"rgehrsitz/rex/internal/sensors"
	"rgehrsitz/rex/internal/store"
)

// EvaluateRules is the entry point for evaluating a set of rules given a sensor event.
func EvaluateRules(sensorEvent sensors.SensorEvent, ruleSet []rule.Rule, store store.Store) {
	sensorDependencies := getAllSensorDependencies(ruleSet)
	sensorValues, _ := store.GetValues(sensorDependencies)

	for _, rule := range ruleSet {
		if shouldEvaluateRule(rule, sensorEvent.SensorName) {
			evaluateRule(rule, sensorValues)
		}
	}
}

// getAllSensorDependencies extracts all unique sensor names from the rules.
func getAllSensorDependencies(ruleSet []rule.Rule) []string {
	sensorNames := make(map[string]struct{})
	for _, rule := range ruleSet {
		for _, sensorName := range rule.SensorDependencies {
			sensorNames[sensorName] = struct{}{}
		}
	}

	var uniqueSensorNames []string
	for name := range sensorNames {
		uniqueSensorNames = append(uniqueSensorNames, name)
	}
	return uniqueSensorNames
}

// shouldEvaluateRule determines if a rule should be evaluated based on the sensor event.
func shouldEvaluateRule(rule rule.Rule, sensorEventName string) bool {
	// Implementation depends on rule structure; typically checks if the sensorEventName is in rule's dependencies.
	return true // Placeholder implementation
}

// evaluateRule evaluates a single rule based on the prefetched sensor values.
func evaluateRule(rule rule.Rule, sensorValues map[string]interface{}) {
	// Evaluate the rule using the sensor values.
	// Placeholder for the rule evaluation logic.
}
