// rex/pkg/runtime/engine.go

package runtime

import (
	"encoding/binary"
	"math"
	"os"
	"rgehrsitz/rex/pkg/compiler"

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
	log.Info().Uint32("version", version).Msg("Read bytecode version")
	offset += 4
	checksum := binary.LittleEndian.Uint32(bytecode[offset:])
	log.Info().Uint32("checksum", checksum).Msg("Read bytecode checksum")
	offset += 4
	constPoolSize := binary.LittleEndian.Uint32(bytecode[offset:])
	log.Info().Uint32("constPoolSize", constPoolSize).Msg("Read constant pool size")
	offset += 4
	numRules := binary.LittleEndian.Uint32(bytecode[offset:])
	log.Info().Uint32("numRules", numRules).Msg("Read number of rules")
	offset += 4
	ruleExecIndexOffset := binary.LittleEndian.Uint32(bytecode[offset:])
	log.Info().Uint32("ruleExecIndexOffset", ruleExecIndexOffset).Msg("Read rule execution index offset")
	offset += 4
	factRuleIndexOffset := binary.LittleEndian.Uint32(bytecode[offset:])
	log.Info().Uint32("factRuleIndexOffset", factRuleIndexOffset).Msg("Read fact rule index offset")
	offset += 4
	factDepIndexOffset := binary.LittleEndian.Uint32(bytecode[offset:])
	log.Info().Uint32("factDepIndexOffset", factDepIndexOffset).Msg("Read fact dependency index offset")
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

		// Adjust the byte offset by adding the size of the header
		adjustedByteOffset := byteOffset + compiler.HeaderSize

		engine.ruleExecutionIndex = append(engine.ruleExecutionIndex, compiler.RuleExecutionIndex{
			RuleName:   name,
			ByteOffset: adjustedByteOffset,
		})
		log.Info().Str("ruleName", name).Int("byteOffset", adjustedByteOffset).Msg("Read rule execution index entry")
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
		log.Info().Str("fact", fact).Strs("rules", rules).Msg("Read fact rule index entry")
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
		log.Info().Str("rule", rule).Strs("facts", facts).Msg("Read fact dependency index entry")
	}

	log.Info().Msg("Engine initialized from bytecode")
	return engine, nil
}

func (e *Engine) ProcessFactUpdate(factName string, factValue interface{}) {
	log.Info().Str("factName", factName).Interface("factValue", factValue).Msg("Processing fact update")

	// Update the fact value in the store
	e.Facts[factName] = factValue

	// Find all rules that reference the updated fact
	ruleNames, ok := e.factRuleIndex[factName]
	if !ok {
		log.Info().Str("factName", factName).Msg("No rules found for the updated fact")
		return
	}

	log.Info().Str("factName", factName).Strs("ruleNames", ruleNames).Msg("Found rules referencing the updated fact")

	// Evaluate each rule
	for _, ruleName := range ruleNames {
		e.evaluateRule(ruleName)
	}
}

func (e *Engine) evaluateRule(ruleName string) {
	log.Info().Str("ruleName", ruleName).Msg("Evaluating rule")

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
		log.Warn().Str("ruleName", ruleName).Msg("Rule not found in ruleExecutionIndex")
		return
	}

	log.Info().Str("ruleName", ruleName).Int("offset", ruleOffset).Msg("Found rule in ruleExecutionIndex")

	offset := ruleOffset
	var action compiler.Action

	var factValue interface{}
	var constValue interface{}
	var comparisonResult bool

	for offset < len(e.bytecode) {
		opcode := compiler.Opcode(e.bytecode[offset])
		offset++

		log.Info().Uint8("opcode", uint8(opcode)).Int("offset", offset-1).Msg("Executing opcode")

		switch opcode {
		case compiler.RULE_START:
			ruleNameLength := int(e.bytecode[offset])
			log.Info().Msg("Encountered RULE_START opcode")
			offset++
			ruleName := string(e.bytecode[offset : offset+ruleNameLength])
			offset += ruleNameLength
			log.Info().Str("ruleName", ruleName).Msg("Encountered rule name")
			continue
		case compiler.RULE_END:
			log.Info().Msg("Encountered RULE_END opcode")
			return
		case compiler.LOAD_FACT_INT:
			nameLen := int(e.bytecode[offset])
			offset++
			factName := string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			factValue = e.Facts[factName]
			log.Info().Str("factName", factName).Interface("factValue", factValue).Msg("Encountered LOAD_FACT_INT opcode")
		case compiler.LOAD_FACT_FLOAT:
			nameLen := int(e.bytecode[offset])
			offset++
			factName := string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			factValue = e.Facts[factName]
			log.Info().Str("factName", factName).Interface("factValue", factValue).Msg("Encountered LOAD_FACT_FLOAT opcode")
		case compiler.LOAD_FACT_STRING:
			nameLen := int(e.bytecode[offset])
			offset++
			factName := string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			factValue = e.Facts[factName]
			log.Info().Str("factName", factName).Interface("factValue", factValue).Msg("Encountered LOAD_FACT_STRING opcode")
		case compiler.LOAD_FACT_BOOL:
			nameLen := int(e.bytecode[offset])
			offset++
			factName := string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			factValue = e.Facts[factName]
			log.Info().Str("factName", factName).Interface("factValue", factValue).Msg("Encountered LOAD_FACT_BOOL opcode")
		case compiler.LOAD_CONST_INT:
			constValue = int64(binary.LittleEndian.Uint64(e.bytecode[offset : offset+8]))
			offset += 8
			log.Info().Int64("constValue", constValue.(int64)).Msg("Encountered LOAD_CONST_INT opcode")
		case compiler.LOAD_CONST_FLOAT:
			bits := binary.LittleEndian.Uint64(e.bytecode[offset : offset+8])
			constValue = math.Float64frombits(bits)
			offset += 8
			log.Info().Float64("constValue", constValue.(float64)).Msg("Encountered LOAD_CONST_FLOAT opcode")
		case compiler.LOAD_CONST_STRING:
			nameLen := int(e.bytecode[offset])
			offset++
			constValue = string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			log.Info().Str("constValue", constValue.(string)).Msg("Encountered LOAD_CONST_STRING opcode")
		case compiler.LOAD_CONST_BOOL:
			constValue = e.bytecode[offset] == 1
			offset++
			log.Info().Bool("constValue", constValue.(bool)).Msg("Encountered LOAD_CONST_BOOL opcode")
		case compiler.EQ_INT, compiler.EQ_FLOAT, compiler.EQ_STRING, compiler.EQ_BOOL,
			compiler.NEQ_INT, compiler.NEQ_FLOAT, compiler.NEQ_STRING, compiler.NEQ_BOOL,
			compiler.LT_INT, compiler.LT_FLOAT,
			compiler.LTE_INT, compiler.LTE_FLOAT,
			compiler.GT_INT, compiler.GT_FLOAT,
			compiler.GTE_INT, compiler.GTE_FLOAT:
			comparisonResult = e.compare(factValue, constValue, opcode)
			log.Info().Bool("comparisonResult", comparisonResult).Msg("Encountered comparison opcode")
		case compiler.JUMP_IF_FALSE:
			jumpOffset := int(binary.LittleEndian.Uint32(e.bytecode[offset : offset+4]))
			offset += 4
			log.Info().Int("jumpOffset", jumpOffset).Msg("Encountered JUMP_IF_FALSE opcode")
			if !comparisonResult {
				offset = offset + jumpOffset
			}
		case compiler.JUMP_IF_TRUE:
			jumpOffset := int(binary.LittleEndian.Uint32(e.bytecode[offset : offset+4]))
			offset += 4
			log.Info().Int("jumpOffset", jumpOffset).Msg("Encountered JUMP_IF_TRUE opcode")
			if comparisonResult {
				offset = offset + jumpOffset
			}
		case compiler.ACTION_VALUE_INT:
			actionValue := int64(binary.LittleEndian.Uint64(e.bytecode[offset : offset+8]))
			offset += 8
			action.Value = actionValue
			log.Info().Int64("actionValue", actionValue).Msg("Encountered ACTION_VALUE_INT opcode")
		case compiler.ACTION_VALUE_FLOAT:
			bits := binary.LittleEndian.Uint64(e.bytecode[offset : offset+8])
			actionValue := math.Float64frombits(bits)
			offset += 8
			action.Value = actionValue
			log.Info().Float64("actionValue", actionValue).Msg("Encountered ACTION_VALUE_FLOAT opcode")
		case compiler.ACTION_VALUE_STRING:
			nameLen := int(e.bytecode[offset])
			offset++
			actionValue := string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			action.Value = actionValue
			log.Info().Str("actionValue", actionValue).Msg("Encountered ACTION_VALUE_STRING opcode")
		case compiler.ACTION_VALUE_BOOL:
			actionValue := e.bytecode[offset] == 1
			offset++
			action.Value = actionValue
			log.Info().Bool("actionValue", actionValue).Msg("Encountered ACTION_VALUE_BOOL opcode")
		case compiler.ACTION_START:
			log.Info().Msg("Encountered ACTION_START opcode")
		case compiler.ACTION_END:
			log.Info().Msg("Encountered ACTION_END opcode")
			e.executeAction(action)
			// we can skip the end of the rule and return back to the calling fucntion
			return
		case compiler.ACTION_TYPE:
			nameLen := int(e.bytecode[offset])
			offset++
			action.Type = string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			log.Info().Str("actionType", action.Type).Msg("Encountered ACTION_TYPE opcode")
		case compiler.ACTION_TARGET:
			nameLen := int(e.bytecode[offset])
			offset++
			action.Target = string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			log.Info().Str("actionTarget", action.Target).Msg("Encountered ACTION_TARGET opcode")
		default:
			log.Warn().Uint8("opcode", uint8(opcode)).Msg("Unknown opcode")
		}
	}
}

func (e *Engine) compare(factValue, constValue interface{}, opcode compiler.Opcode) bool {
	switch opcode {
	case compiler.EQ_INT:
		return factValue.(int64) == constValue.(int64)
	case compiler.EQ_FLOAT:
		return factValue.(float64) == constValue.(float64)
	case compiler.EQ_STRING:
		return factValue.(string) == constValue.(string)
	case compiler.EQ_BOOL:
		return factValue.(bool) == constValue.(bool)
	case compiler.NEQ_INT:
		return factValue.(int64) != constValue.(int64)
	case compiler.NEQ_FLOAT:
		return factValue.(float64) != constValue.(float64)
	case compiler.NEQ_STRING:
		return factValue.(string) != constValue.(string)
	case compiler.NEQ_BOOL:
		return factValue.(bool) != constValue.(bool)
	case compiler.LT_INT:
		return factValue.(int64) < constValue.(int64)
	case compiler.LT_FLOAT:
		return factValue.(float64) < constValue.(float64)
	case compiler.LTE_INT:
		return factValue.(int64) <= constValue.(int64)
	case compiler.LTE_FLOAT:
		return factValue.(float64) <= constValue.(float64)
	case compiler.GT_INT:
		return factValue.(int64) > constValue.(int64)
	case compiler.GT_FLOAT:
		return factValue.(float64) > constValue.(float64)
	case compiler.GTE_INT:
		return factValue.(int64) >= constValue.(int64)
	case compiler.GTE_FLOAT:
		return factValue.(float64) >= constValue.(float64)
	default:
		log.Warn().Uint8("opcode", uint8(opcode)).Msg("Unknown comparison opcode")
		return false
	}
}

func (e *Engine) executeAction(action compiler.Action) {
	switch action.Type {
	case "updateFact":
		e.Facts[action.Target] = action.Value
		log.Info().Str("target", action.Target).Interface("value", action.Value).Msg("Updated fact")
	// Add more action types as needed
	default:
		log.Warn().Str("type", action.Type).Msg("Unknown action type")
	}
}
