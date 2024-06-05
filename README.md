# Rules Engine eXtended (REX)

## Rules Specification

### JSON Structure

The rules are defined in a JSON file with the following structure:

- rules: an array of rule objects

### Rule Object

A rule object has the following properties:

- name: a unique string identifying the rule
- priority: an integer indicating the rule's priority (default: 10)
- conditions: an object containing a single property:
  - ANY or ALL: an array of condition groups
- actions: an array of action objects
- producedFacts: an array of strings representing the facts produced by this rule (optional)
- consumedFacts: an array of strings representing the facts consumed by this rule (optional)

### Condition Group

A condition group is an object containing:

- conditions: an array of condition objects
- operator: a string indicating the logical operator (ANY or ALL)

### Condition Object

A condition object has the following properties:

- fact: a string identifying the fact to evaluate
- operator: a string indicating the comparison operator (EQ, NEQ, LT, LTE, GT, GTE, CONTAINS, NOT_CONTAINS, TRUE, FALSE)
- value: the value to compare against

### Action Object

An action object has the following properties:

- type: a string indicating the action type ("updateStore" or "sendMessage")
- fact: a string identifying the fact to update or send
- value: the value to update or send
- customProperty: an optional object containing custom properties for the action

### Execution Order

Actions will be executed in the order they are defined in the rule.

### Fact and Value Data Types

Facts are strings. Values can be strings, bools, ints (numbers without decimal points), or floats (numbers with decimal points).

### Priority Ties

Due to concurrent evaluations and other factors, no guarantees can be made regarding how priority ties are resolved. The engine will do its best to resolve all higher priority rules before lower ones, but no precedence can be guaranteed beyond that.

### Example JSON Ruleset

```json
{
  "rules": [
    {
      "name": "rule-1",
      "priority": 10,
      "conditions": {
        "all": [
          {
            "fact": "temperature",
            "operator": "GT",
            "value": 30.0
          },
          {
            "fact": "humidity",
            "operator": "LT",
            "value": 60
          },
          {
            "any": [
              {
                "fact": "pressure",
                "operator": "LT",
                "value": 1010
              },
              {
                "fact": "flow_rate",
                "operator": "GT",
                "value": 5.0
              }
            ]
          }
        ]
      },
      "actions": [
        {
          "type": "updateStore",
          "target": "temperature_status",
          "value": true
        }
      ]
    },
    {
      "name": "rule-2",
      "priority": 15,
      "conditions": {
        "all": [
          {
            "any": [
              {
                "fact": "pressure",
                "operator": "EQ",
                "value": 1013
              },
              {
                "fact": "flow_rate",
                "operator": "GTE",
                "value": 5.0
              }
            ]
          },
          {
            "any": [
              {
                "fact": "temperature",
                "operator": "EQ",
                "value": 72
              },
              {
                "fact": "flow_rate",
                "operator": "LT",
                "value": 5.0
              }
            ]
          }
        ]
      },
      "actions": [
        {
          "type": "sendMessage",
          "target": "alert-service",
          "value": "Alert: Pressure or flow rate exceeded limits!"
        }
      ]
    }
  ]
}
```

This example JSON ruleset defines two rules, "rule-1" and "rule-2", with conditions and actions. The conditions are grouped using the "all" and "any" operators, and the actions are defined with the "updateStore" and "sendMessage" types.

Note that the rule spec allows for nested condition groups, as seen in the example, where an "any" group is inside an "all" group. This allows for complex logical combinations of conditions.
