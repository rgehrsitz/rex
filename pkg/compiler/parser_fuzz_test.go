package compiler

import "testing"

func FuzzParse(f *testing.F) {
	f.Add([]byte(`{
		"rules": [{
			"name": "temperature_rule",
			"conditions": {"all": [{"fact": "temperature", "operator": "GT", "value": 30}]},
			"actions": [{"type": "updateStore", "target": "status", "value": "hot"}]
		}]
	}`))
	f.Add([]byte(`{"rules": []}`))
	f.Add([]byte{0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Parse(data)
	})
}
