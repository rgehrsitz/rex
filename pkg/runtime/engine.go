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

type Condition struct {
	Fact      string
	Operator  string
	Value     interface{}
	ValueType string
}

type Engine struct {
	ruleset             *compiler.Ruleset
	ruleExecutionIndex  []RuleExecutionIndex
	factRuleIndex       map[string][]string
	factDependencyIndex []FactDependencyIndex
	bytecode            []byte
	Facts               map[string]interface{}
}

type RuleExecutionIndex struct {
	RuleName   string
	ByteOffset int
}

type FactDependencyIndex struct {
	RuleName string
	Facts    []string
}

func NewEngine(bytecode []byte) *Engine {
	engine := &Engine{
		ruleset:             &compiler.Ruleset{},
		ruleExecutionIndex:  []RuleExecutionIndex{},
		factRuleIndex:       make(map[string][]string),
		factDependencyIndex: []FactDependencyIndex{},
		bytecode:            bytecode,
		Facts:               make(map[string]interface{}),
	}

	// Parse header and indices from bytecode
	engine.parseHeaderAndIndices()

	return engine
}

// Add this method to the Engine struct
func (e *Engine) GetFacts() map[string]interface{} {
	return e.Facts
}

func (e *Engine) parseHeaderAndIndices() {
	log.Printf("Parsing header and indices\n")
	offset := 0

	version := binary.LittleEndian.Uint16(e.bytecode[offset:])
	log.Printf("Version: %d\n", version)
	offset += 2

	checksum := binary.LittleEndian.Uint32(e.bytecode[offset:])
	log.Printf("Checksum: %d\n", checksum)
	offset += 4

	constPoolSize := binary.LittleEndian.Uint16(e.bytecode[offset:])
	log.Printf("Constant pool size: %d\n", constPoolSize)
	offset += 2

	numRules := binary.LittleEndian.Uint16(e.bytecode[offset:])
	log.Printf("Number of rules: %d\n", numRules)
	offset += 2

	ruleExecIndexOffset := binary.LittleEndian.Uint32(e.bytecode[offset:])
	log.Printf("Rule execution index offset: %d\n", ruleExecIndexOffset)
	offset += 4

	factRuleIndexOffset := binary.LittleEndian.Uint32(e.bytecode[offset:])
	log.Printf("Fact rule lookup index offset: %d\n", factRuleIndexOffset)
	offset += 4

	factDepIndexOffset := binary.LittleEndian.Uint32(e.bytecode[offset:])
	log.Printf("Fact dependency index offset: %d\n", factDepIndexOffset)
	offset += 4

	log.Printf("Bytecode length: %d\n", len(e.bytecode))

	// Read rule execution index
	offset = int(ruleExecIndexOffset)
	log.Printf("Reading rule execution index at offset: %d\n", offset)
	for i := 0; i < int(numRules); i++ {
		if offset+4 > len(e.bytecode) {
			log.Fatal().Msgf("Index out of range while reading name length at offset: %d\n", offset+4)
		}
		nameLen := int(binary.LittleEndian.Uint32(e.bytecode[offset:]))
		log.Printf("Name length: %d\n", nameLen)
		offset += 4

		if offset+nameLen > len(e.bytecode) {
			log.Fatal().Msgf("Index out of range while reading name at offset: %d\n", offset+nameLen)
		}
		name := string(e.bytecode[offset : offset+nameLen])
		log.Printf("Name: %s\n", name)
		offset += nameLen

		if offset+4 > len(e.bytecode) {
			log.Fatal().Msgf("Index out of range while reading byte offset at offset: %d\n", offset+4)
		}
		byteOffset := binary.LittleEndian.Uint32(e.bytecode[offset:])
		log.Printf("Byte offset: %d\n", byteOffset)
		offset += 4

		e.ruleExecutionIndex = append(e.ruleExecutionIndex, RuleExecutionIndex{
			RuleName:   name,
			ByteOffset: int(byteOffset),
		})
	}

	// Read fact rule index
	offset = int(factRuleIndexOffset)
	log.Printf("Reading fact rule index at offset: %d\n", offset)
	for offset < int(factDepIndexOffset) {
		if offset+4 > len(e.bytecode) {
			log.Fatal().Msgf("Index out of range while reading fact length at offset: %d\n", offset+4)
		}
		factLen := int(binary.LittleEndian.Uint32(e.bytecode[offset:]))
		log.Printf("Fact length: %d\n", factLen)
		offset += 4

		if offset+factLen > len(e.bytecode) {
			log.Fatal().Msgf("Index out of range while reading fact at offset: %d\n", offset+factLen)
		}
		fact := string(e.bytecode[offset : offset+factLen])
		log.Printf("Fact: %s\n", fact)
		offset += factLen

		if offset+4 > len(e.bytecode) {
			log.Fatal().Msgf("Index out of range while reading rules count at offset: %d\n", offset+4)
		}
		rulesCount := int(binary.LittleEndian.Uint32(e.bytecode[offset:]))
		log.Printf("Rules count: %d\n", rulesCount)
		offset += 4

		var rules []string
		for j := 0; j < rulesCount; j++ {
			if offset+4 > len(e.bytecode) {
				log.Fatal().Msgf("Index out of range while reading rule length at offset: %d\n", offset+4)
			}
			ruleLen := int(binary.LittleEndian.Uint32(e.bytecode[offset:]))
			log.Printf("Rule length: %d\n", ruleLen)
			offset += 4

			if offset+ruleLen > len(e.bytecode) {
				log.Fatal().Msgf("Index out of range while reading rule at offset: %d\n", offset+ruleLen)
			}
			rule := string(e.bytecode[offset : offset+ruleLen])
			log.Printf("Rule: %s\n", rule)
			offset += ruleLen
			rules = append(rules, rule)
		}
		e.factRuleIndex[fact] = rules
	}

	// Read fact dependency index
	offset = int(factDepIndexOffset)
	log.Printf("Reading fact dependency index at offset: %d\n", offset)
	for offset < len(e.bytecode) {
		if offset+4 > len(e.bytecode) {
			log.Fatal().Msgf("Index out of range while reading rule length at offset: %d\n", offset+4)
		}
		ruleLen := int(binary.LittleEndian.Uint32(e.bytecode[offset:]))
		log.Printf("Rule length: %d\n", ruleLen)
		offset += 4

		if offset+ruleLen > len(e.bytecode) {
			log.Fatal().Msgf("Index out of range while reading rule at offset: %d\n", offset+ruleLen)
		}
		rule := string(e.bytecode[offset : offset+ruleLen])
		log.Printf("Rule: %s\n", rule)
		offset += ruleLen

		if offset+4 > len(e.bytecode) {
			log.Fatal().Msgf("Index out of range while reading facts count at offset: %d\n", offset+4)
		}
		factsCount := int(binary.LittleEndian.Uint32(e.bytecode[offset:]))
		log.Printf("Facts count: %d\n", factsCount)
		offset += 4

		var facts []string
		for j := 0; j < factsCount; j++ {
			if offset+4 > len(e.bytecode) {
				log.Fatal().Msgf("Index out of range while reading fact length at offset: %d\n", offset+4)
			}
			factLen := int(binary.LittleEndian.Uint32(e.bytecode[offset:]))
			log.Printf("Fact length: %d\n", factLen)
			offset += 4

			if offset+factLen > len(e.bytecode) {
				log.Fatal().Msgf("Index out of range while reading fact at offset: %d\n", offset+factLen)
			}
			fact := string(e.bytecode[offset : offset+factLen])
			log.Printf("Fact: %s\n", fact)
			offset += factLen
			facts = append(facts, fact)
		}
		e.factDependencyIndex = append(e.factDependencyIndex, FactDependencyIndex{
			RuleName: rule,
			Facts:    facts,
		})
	}
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
		case byte(compiler.UPDATE_FACT):
			action.Type = "updateStore"
			log.Printf("Executing action: %v\n", action)
			e.executeAction(action)
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

func (e *Engine) executeAction(action compiler.Action) {
	switch action.Type {
	case "updateStore":
		e.Facts[action.Target] = action.Value
		log.Printf("Updating store: %s = %v\n", action.Target, action.Value)
	case "sendMessage":
		log.Printf("Sending message to %s: %v\n", action.Target, action.Value)
	default:
		log.Printf("Unknown action type: %s\n", action.Type)
	}
}

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

func NewEngineFromFile(filename string) (*Engine, error) {
	bytecode, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	engine := &Engine{
		bytecode:            bytecode,
		ruleExecutionIndex:  make([]RuleExecutionIndex, 0),
		factRuleIndex:       make(map[string][]string),
		factDependencyIndex: make([]FactDependencyIndex, 0),
		Facts:               make(map[string]interface{}),
	}

	offset := 0

	// Read version
	version := binary.LittleEndian.Uint16(bytecode[offset:])
	log.Printf("Version: %d\n", version)
	offset += 2

	// Read checksum
	checksum := binary.LittleEndian.Uint32(bytecode[offset:])
	log.Printf("Checksum: %d\n", checksum)
	offset += 4

	// Read constant pool size
	constPoolSize := binary.LittleEndian.Uint16(bytecode[offset:])
	log.Printf("Constant pool size: %d\n", constPoolSize)
	offset += 2

	// Read number of rules
	numRules := binary.LittleEndian.Uint16(bytecode[offset:])
	log.Printf("Number of rules: %d\n", numRules)
	offset += 2

	// Read rule execution index offset
	ruleExecIndexOffset := binary.LittleEndian.Uint32(bytecode[offset:])
	log.Printf("Rule execution index offset: %d\n", ruleExecIndexOffset)
	offset += 4

	// Read fact rule lookup index offset
	factRuleIndexOffset := binary.LittleEndian.Uint32(bytecode[offset:])
	log.Printf("Fact rule lookup index offset: %d\n", factRuleIndexOffset)
	offset += 4

	// Read fact dependency index offset
	factDepIndexOffset := binary.LittleEndian.Uint32(bytecode[offset:])
	log.Printf("Fact dependency index offset: %d\n", factDepIndexOffset)
	offset += 4

	// Read rule execution index
	offset = int(ruleExecIndexOffset)
	for i := 0; i < int(numRules); i++ {
		nameLen := int(bytecode[offset])
		log.Printf("Name length: %d\n", nameLen)
		offset++
		name := string(bytecode[offset : offset+nameLen])
		log.Printf("Name: %s\n", name)
		offset += nameLen
		byteOffset := binary.LittleEndian.Uint32(bytecode[offset:])
		offset += 4
		engine.ruleExecutionIndex = append(engine.ruleExecutionIndex, RuleExecutionIndex{
			RuleName:   name,
			ByteOffset: int(byteOffset),
		})
	}

	// Read fact rule index
	offset = int(factRuleIndexOffset)
	for offset < int(factDepIndexOffset) {
		factLen := int(bytecode[offset])
		offset++
		fact := string(bytecode[offset : offset+factLen])
		offset += factLen
		rulesCount := int(bytecode[offset])
		offset++
		var rules []string
		for i := 0; i < rulesCount; i++ {
			ruleLen := int(bytecode[offset])
			offset++
			rule := string(bytecode[offset : offset+ruleLen])
			offset += ruleLen
			rules = append(rules, rule)
		}
		engine.factRuleIndex[fact] = rules
	}

	// Read fact dependency index
	offset = int(factDepIndexOffset)
	for offset < len(bytecode) {
		ruleLen := int(bytecode[offset])
		offset++
		rule := string(bytecode[offset : offset+ruleLen])
		offset += ruleLen
		factsCount := int(bytecode[offset])
		offset++
		var facts []string
		for i := 0; i < factsCount; i++ {
			factLen := int(bytecode[offset])
			offset++
			fact := string(bytecode[offset : offset+factLen])
			offset += factLen
			facts = append(facts, fact)
		}
		engine.factDependencyIndex = append(engine.factDependencyIndex, FactDependencyIndex{
			RuleName: rule,
			Facts:    facts,
		})
	}

	return engine, nil
}
