# REX - Rules Engine eXtended ![alt text](cmd/rexd/rex_logo_128.png)

[![License](https://img.shields.io/badge/License-MIT-blue)](#license)
[![Auto Wiki](https://img.shields.io/badge/Auto_Wiki-Mutable.ai-blue)](https://wiki.mutable.ai/rgehrsitz/rex)

REX is a rules engine designed to process complex conditions and actions using a structured JSON format for rule definitions. It allows for defining rules, conditions, and actions that are compiled into bytecode by the REX Compiler, then executed by the REX Engine.  
REX is designed to be used in conjunction with a key/value store such as Redis or NATS, where REX subscribes to and recieves updates from the key/value store, evaluates the updated value, then updates and publishes applicable results back to the store.

## Features

- Define rules using JSON
- Support for various data types and comparison operations
- Logical and control flow instructions
- Action execution based on rules

## Getting Started

### Prerequisites

- Go 1.20 or higher

### Installation

Clone the repository:

```bash
git clone https://github.com/rgehrsitz/rex.git
```

Navigate to the project directory:

```bash
cd rex
```

### Running the Executables

The REX repository includes four main executables: rexc, rexd, redis_setup, and rule_gen. Below are the details on how to build, run, and understand the purpose of each executable.

### 1. Compiler

Purpose:
The rexc executable is the compiler that translates rules defined in JSON format into bytecode instructions that the runtime engine can execute.

How to Build:

```bash
go build ./cmd/rexc
```

How to Run:

```bash
./rexc -rules path/to/rules.json -loglevel info -logoutput console
```

Example:

```bash
./rexc -rules examples/rules2.json -loglevel debug -logoutput file
```

### 2. Runtime Engine

Purpose:
The rexd executable is the runtime engine that processes the bytecode generated by rexc. It uses a Redis store for managing and updating facts in real-time based on the rules.

How to Build:

```bash
go build ./cmd/rexd
```

How to Run:

```bash
./rexd -config path/to/config.json
```

Example:

```bash
./rexd -config cmd/rexd/rex_config.json
```

### 3. Redis Setup

Purpose:
The redis_setup executable initializes the Redis database with default values necessary for some testing of the REX system. It also provides a CLI for modifying values during debugging.

How to Build:

```bash
go build ./tools/redis_setup
```

How to Run:

```bash
./redis_setup
```

Example:

```bash
./redis_setup
```

### 4. Rule Generator

Purpose:
The rule_gen executable generates a large number of random rules in JSON format, which can be used for testing and benchmarking the REX system.

How to Build:

```bash
go build ./tools/rule_gen
```

How to Run:

bash
./rule_gen -rules number_of_rules -output output_file.json

```

Example:

bash
./rule_gen -rules 1000 -output generated_ruleset.json
```

### Usage

Defining Rules
Rules are defined in a JSON format. Each rule consists of conditions and actions. Here's an example:

```json
{
  "rules": [
    {
      "name": "rule-1",
      "conditions": {
        "all": [
          { "fact": "weather:temperature", "operator": "GT", "value": 30 },
          { "fact": "weather:humidity", "operator": "LT", "value": 40.01 }
        ]
      },
      "actions": [
        {
          "type": "updateStore",
          "target": "weather:temperature_warning",
          "value": "high"
        }
      ]
    },
    {
      "name": "rule_unique_name_bob",
      "conditions": {
        "all": [
          { "fact": "people:age", "operator": "LTE", "value": 60 },
          { "fact": "people:iq", "operator": "EQ", "value": 40.01 },
          {
            "any": [
              { "fact": "people:name", "operator": "CONTAINS", "value": "bob" },
              { "fact": "people:handsome", "operator": "EQ", "value": true }
            ]
          }
        ]
      },
      "actions": [
        {
          "type": "updateStore",
          "target": "people:bob_alert",
          "value": "true"
        }
      ]
    }
  ]
}
```

Running the Engine
Execute the engine with the defined rules:

```bash
./rex -rules rules.json
```

Bytecode
REX translates rules into bytecode instructions. The bytecode instructions include comparison operations, logical operations, fact loading, and control flow operations. The generated bytecode is executed to process the defined rules.

Development
Code Structure
cmd/rex: Main application entry point
pkg/compiler: Contains the bytecode compiler and related functions
pkg/parser: Parses the JSON rule definitions

## Rules Specification

### JSON Structure

The rules are defined in a JSON file with the following structure:

- rules: an array of rule objects

### Rule Object

A rule object has the following properties:

- name: a unique string identifying the rule
- priority: optional integer indicating the rule's priority (default: 10)
- conditions: an object containing a single property:
  - ANY or ALL: an array of condition groups
- actions: an array of action objects

### Condition Group

A condition group is an object containing:

- conditions: an array of condition objects.
- operator: a string indicating the logical operator (ANY or ALL).

### Condition Object

A condition object has the following properties:

- fact: a string identifying the fact to evaluate. Based on the way Redis works, the recommendation is 'channel:key' for the naming of facts.
- operator: a string indicating the comparison operator (EQ, NEQ, LT, LTE, GT, GTE, CONTAINS, NOT_CONTAINS).
- value: the value to compare against.

\*\*All condition objects not part of a grouping MUST be defined prior to any nested condition groups.
\*\*The characters is a string must NOT include a colon ':' due to how Redis parses channels/keys

### Action Object

An action object has the following properties:

- type: a string indicating the action type ("updateStore" or "sendMessage") (sendMessage is not yet implemented).
- fact: a string identifying the fact to update or send. Based on the way Redis works, the recommendation is 'channel:key' for the naming of facts.
- value: the value to update or send.
- customProperty: an optional object containing custom properties for the action.

### Execution Order

Actions will be executed in the order they are defined in the rule.

### Fact and Value Data Types

Facts are strings. Values can be strings surrounded by quotation marks (e.g. "fact_a"), bools (e.g. true or false), or numbers with or without decimal points (e.g. 30.01, 30, -12.123).

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
          "value": "Alert - Pressure or flow rate exceeded limits!"
        }
      ]
    }
  ]
}
```

This example JSON ruleset defines two rules, "rule-1" and "rule-2", with conditions and actions. The conditions are grouped using the "all" and "any" operators, and the actions are defined with the "updateStore" and "sendMessage" types.

Note that the rule spec allows for nested condition groups, as seen in the example, where an "any" group is inside an "all" group. This allows for complex logical combinations of conditions.

## Unit Testing

Prior to running the full unit test suite, ensure that a Redis instance is up and running using the standar Redis port 6379.
