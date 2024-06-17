// rex/pkg/runtime/engine.go

package runtime

import (
	"encoding/binary"
	"fmt"
	"os"
	"rgehrsitz/rex/pkg/compiler"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

type Engine struct {
	bytecode            []byte
	ruleExecutionIndex  []compiler.RuleExecutionIndex
	factRuleIndex       map[string][]string
	factDependencyIndex []compiler.FactDependencyIndex
	Facts               map[string]interface{}
}

func NewEngineFromFile(filename string) (*Engine, error) {
	bytecode, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	engine := &Engine{
		bytecode:            bytecode,
		ruleExecutionIndex:  make([]compiler.RuleExecutionIndex, 0),
		factRuleIndex:       make(map[string][]string),
		factDependencyIndex: make([]compiler.FactDependencyIndex, 0),
		Facts:               make(map[string]interface{}),
	}

	offset := 0

	// Read header
	version := binary.LittleEndian.Uint32(bytecode[offset:])
	log.Printf("Version: %d\n", version)
	offset += 4
	checksum := binary.LittleEndian.Uint32(bytecode[offset:])
	log.Printf("Checksum: %d\n", checksum)
	offset += 4
	constPoolSize := binary.LittleEndian.Uint32(bytecode[offset:])
	log.Printf("Constant pool size: %d\n", constPoolSize)
	offset += 4
	numRules := binary.LittleEndian.Uint32(bytecode[offset:])
	log.Printf("Number of rules: %d\n", numRules)
	offset += 4
	ruleExecIndexOffset := binary.LittleEndian.Uint32(bytecode[offset:])
	offset += 4
	factRuleIndexOffset := binary.LittleEndian.Uint32(bytecode[offset:])
	offset += 4
	factDepIndexOffset := binary.LittleEndian.Uint32(bytecode[offset:])
	offset += 4

	// Read rule execution index
	offset = int(ruleExecIndexOffset)
	for i := 0; i < int(numRules); i++ {
		nameLen := int(binary.LittleEndian.Uint32(bytecode[offset:]))
		offset += 4
		name := string(bytecode[offset : offset+nameLen])
		offset += nameLen
		byteOffset := int(binary.LittleEndian.Uint32(bytecode[offset:]))
		offset += 4
		engine.ruleExecutionIndex = append(engine.ruleExecutionIndex, compiler.RuleExecutionIndex{
			RuleName:   name,
			ByteOffset: byteOffset,
		})
	}

	// Read fact rule index
	offset = int(factRuleIndexOffset)
	for offset < int(factDepIndexOffset) {
		factLen := int(binary.LittleEndian.Uint32(bytecode[offset:]))
		offset += 4
		fact := string(bytecode[offset : offset+factLen])
		offset += factLen
		rulesCount := int(binary.LittleEndian.Uint32(bytecode[offset:]))
		offset += 4
		var rules []string
		for j := 0; j < rulesCount; j++ {
			ruleLen := int(binary.LittleEndian.Uint32(bytecode[offset:]))
			offset += 4
			rule := string(bytecode[offset : offset+ruleLen])
			offset += ruleLen
			rules = append(rules, rule)
		}
		engine.factRuleIndex[fact] = rules
	}

	// Read fact dependency index
	offset = int(factDepIndexOffset)
	for offset < len(bytecode) {
		ruleLen := int(binary.LittleEndian.Uint32(bytecode[offset:]))
		offset += 4
		rule := string(bytecode[offset : offset+ruleLen])
		offset += ruleLen
		factsCount := int(binary.LittleEndian.Uint32(bytecode[offset:]))
		offset += 4
		var facts []string
		for j := 0; j < factsCount; j++ {
			factLen := int(binary.LittleEndian.Uint32(bytecode[offset:]))
			offset += 4
			fact := string(bytecode[offset : offset+factLen])
			offset += factLen
			facts = append(facts, fact)
		}
		engine.factDependencyIndex = append(engine.factDependencyIndex, compiler.FactDependencyIndex{
			RuleName: rule,
			Facts:    facts,
		})
	}

	return engine, nil
}

func (e *Engine) ProcessFactUpdate(factName string, factValue interface{}) {
	// Update the fact value in the store
	e.Facts[factName] = factValue

	// Find all rules that reference the updated fact
	ruleNames, ok := e.factRuleIndex[factName]
	if !ok {
		return
	}

	// Evaluate each rule
	for _, ruleName := range ruleNames {
		e.evaluateRule(ruleName)
	}
}

func (e *Engine) evaluateRule(ruleName string) {
	log.Printf("Evaluating rule: %s\n", ruleName)
	var ruleOffset int
	found := false
	for _, rule := range e.ruleExecutionIndex {
		if rule.RuleName == ruleName {
			ruleOffset = rule.ByteOffset
			found = true
			break
		}
	}

	if !found {
		log.Printf("Rule %s not found in ruleExecutionIndex\n", ruleName)
		return
	}

	log.Printf("Rule %s found at offset %d\n", ruleName, ruleOffset)
	offset := ruleOffset
	var action compiler.Action

	for offset < len(e.bytecode) {
		opcode := e.bytecode[offset]
		offset++

		log.Printf("Executing opcode: %v at offset %d\n", opcode, offset-1)

		switch opcode {
		case byte(compiler.RULE_START):
			log.Printf("RULE_START opcode encountered")
			continue
		case byte(compiler.JUMP_IF_FALSE), byte(compiler.JUMP_IF_TRUE):
			parts := strings.Split(string(e.bytecode[offset:offset+int(opcode)]), " ")
			offset += int(opcode)
			if len(parts) != 4 {
				log.Printf("Invalid operands format for %v: %s\n", opcode, parts)
				return
			}

			fact := parts[0]
			operator := parts[1]
			valueStr := parts[2]
			offsetStr := parts[3]

			var value interface{}
			var valueType string

			if i, err := strconv.Atoi(valueStr); err == nil {
				value = i
				valueType = "int"
			} else if f, err := strconv.ParseFloat(valueStr, 64); err == nil {
				value = f
				valueType = "float"
			} else if b, err := strconv.ParseBool(valueStr); err == nil {
				value = b
				valueType = "bool"
			} else {
				value = valueStr
				valueType = "string"
			}

			factValue, ok := e.Facts[fact]
			if !ok {
				log.Printf("Fact %s not found\n", fact)
				return
			}

			result := evaluate(factValue, operator, value, valueType)
			log.Printf("Condition result: %v\n", result)

			if (opcode == byte(compiler.JUMP_IF_FALSE) && !result) || (opcode == byte(compiler.JUMP_IF_TRUE) && result) {
				jumpOffset, err := strconv.Atoi(offsetStr)
				if err != nil {
					log.Printf("Invalid jump offset: %s\n", offsetStr)
					return
				}
				log.Printf("Jumping to offset: %d\n", jumpOffset)
				offset = jumpOffset
			}
		case byte(compiler.ACTION_START):
			log.Printf("Starting actions")
			action = compiler.Action{}
		case byte(compiler.LOAD_CONST_STRING):
			action.Target = string(e.bytecode[offset : offset+int(opcode)])
			offset += int(opcode)
			log.Printf("Loaded constant string: %s\n", action.Target)
		case byte(compiler.LOAD_CONST_BOOL):
			if opcode != 1 {
				log.Printf("Invalid operands length for LOAD_CONST_BOOL: %v\n", opcode)
				return
			}
			action.Value = e.bytecode[offset] == 1
			offset++
			log.Printf("Loaded constant bool: %v\n", action.Value)
		// case byte(compiler.UPDATE_FACT):
		// 	action.Type = "updateStore"
		// 	log.Printf("Executing action: %v\n", action)
		// 	e.executeAction(action)
		case byte(compiler.ACTION_END):
			log.Printf("Ending actions")
		case byte(compiler.RULE_END):
			log.Printf("End of rule: %s\n", ruleName)
			return
		default:
			log.Printf("Unknown opcode: %v\n", opcode)
		}
	}
}

func evaluate(factValue interface{}, operator string, conditionValue interface{}, valueType string) bool {
	log.Printf("Evaluating fact value: %v, operator: %s, condition value: %v, value type: %s\n", factValue, operator, conditionValue, valueType)
	var result bool
	switch valueType {
	case "int":
		if factValueFloat, ok := factValue.(float64); ok {
			log.Printf("Comparing float fact value: %v with int condition value: %v", factValueFloat, conditionValue)
			result = compareFloat(float64(factValueFloat), float64(conditionValue.(int)), operator)
		} else if factValueInt, ok := factValue.(int); ok {
			log.Printf("Comparing int fact value: %v with int condition value: %v", factValueInt, conditionValue)
			result = compareInt(factValueInt, conditionValue.(int), operator)
		}
	case "float":
		if factValueInt, ok := factValue.(int); ok {
			log.Printf("Comparing int fact value: %v with float condition value: %v", factValueInt, conditionValue)
			result = compareFloat(float64(factValueInt), conditionValue.(float64), operator)
		} else if factValueFloat, ok := factValue.(float64); ok {
			log.Printf("Comparing float fact value: %v with float condition value: %v", factValueFloat, conditionValue)
			result = compareFloat(factValueFloat, conditionValue.(float64), operator)
		}
	case "bool":
		factValueBool, ok := factValue.(bool)
		if !ok {
			log.Printf("Type mismatch: fact value is not a bool\n")
			return false
		}
		conditionValueBool := conditionValue.(bool)
		result = compareBool(factValueBool, conditionValueBool, operator)
	case "string":
		factValueStr, ok := factValue.(string)
		if !ok {
			log.Printf("Type mismatch: fact value is not a string\n")
			return false
		}
		conditionValueStr := conditionValue.(string)
		result = compareString(factValueStr, conditionValueStr, operator)
	default:
		log.Printf("Unsupported value type: %s\n", valueType)
		return false
	}
	log.Printf("Evaluation result: %v\n", result)
	return result
}

// func (e *Engine) executeAction(action compiler.Action) {
// 	switch action.Type {
// 	case "updateStore":
// 		e.Facts[action.Target] = action.Value
// 		log.Printf("Updating store: %s = %v\n", action.Target, action.Value)
// 	case "sendMessage":
// 		log.Printf("Sending message to %s: %v\n", action.Target, action.Value)
// 	default:
// 		log.Printf("Unknown action type: %s\n", action.Type)
// 	}
// }

func compareInt(a, b int, operator string) bool {
	switch operator {
	case "EQ":
		return a == b
	case "NEQ":
		return a != b
	case "LT":
		return a < b
	case "LTE":
		return a <= b
	case "GT":
		return a > b
	case "GTE":
		return a >= b
	default:
		fmt.Printf("Unsupported operator: %s\n", operator)
		return false
	}
}

func compareFloat(a, b float64, operator string) bool {
	switch operator {
	case "EQ":
		return a == b
	case "NEQ":
		return a != b
	case "LT":
		return a < b
	case "LTE":
		return a <= b
	case "GT":
		return a > b
	case "GTE":
		return a >= b
	default:
		fmt.Printf("Unsupported operator: %s\n", operator)
		return false
	}
}

func compareBool(a, b bool, operator string) bool {
	switch operator {
	case "EQ":
		return a == b
	case "NEQ":
		return a != b
	default:
		fmt.Printf("Unsupported operator: %s\n", operator)
		return false
	}
}

func compareString(a, b string, operator string) bool {
	switch operator {
	case "EQ":
		return a == b
	case "NEQ":
		return a != b
	case "LT":
		return a < b
	case "LTE":
		return a <= b
	case "GT":
		return a > b
	case "GTE":
		return a >= b
	default:
		fmt.Printf("Unsupported operator: %s\n", operator)
		return false
	}
}
