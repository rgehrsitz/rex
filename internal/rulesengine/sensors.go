package rulesengine

import "rgehrsitz/rex/internal/rule"

// getAllSensorDependencies extracts all unique sensor names from the set of rules.
func getAllSensorDependencies(rules []rule.Rule) []string {
	sensorSet := make(map[string]struct{})

	for _, r := range rules {
		for _, fact := range r.ConsumedFacts {
			sensorSet[fact] = struct{}{}
		}
	}

	var sensors []string
	for sensor := range sensorSet {
		sensors = append(sensors, sensor)
	}

	return sensors
}
