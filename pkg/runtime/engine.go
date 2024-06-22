package runtime

import (
	"encoding/binary"
	"math"
	"os"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"

	"rgehrsitz/rex/pkg/logging"
)

type Engine struct {
	bytecode            []byte
	ruleExecutionIndex  []compiler.RuleExecutionIndex
	factRuleIndex       map[string][]string
	factDependencyIndex []compiler.FactDependencyIndex
	Facts               map[string]interface{}
	store               store.Store
}

func NewEngineFromFile(filename string, store store.Store) (*Engine, error) {
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
		store:               store,
	}

	offset := 0

	// Read header
	version := binary.LittleEndian.Uint32(bytecode[offset:])
	logging.Logger.Debug().Uint32("version", version).Msg("Read bytecode version")
	offset += 4
	checksum := binary.LittleEndian.Uint32(bytecode[offset:])
	logging.Logger.Debug().Uint32("checksum", checksum).Msg("Read bytecode checksum")
	offset += 4
	constPoolSize := binary.LittleEndian.Uint32(bytecode[offset:])
	logging.Logger.Debug().Uint32("constPoolSize", constPoolSize).Msg("Read constant pool size")
	offset += 4
	numRules := binary.LittleEndian.Uint32(bytecode[offset:])
	logging.Logger.Debug().Uint32("numRules", numRules).Msg("Read number of rules")
	offset += 4
	ruleExecIndexOffset := binary.LittleEndian.Uint32(bytecode[offset:])
	logging.Logger.Debug().Uint32("ruleExecIndexOffset", ruleExecIndexOffset).Msg("Read rule execution index offset")
	offset += 4
	factRuleIndexOffset := binary.LittleEndian.Uint32(bytecode[offset:])
	logging.Logger.Debug().Uint32("factRuleIndexOffset", factRuleIndexOffset).Msg("Read fact rule index offset")
	offset += 4
	factDepIndexOffset := binary.LittleEndian.Uint32(bytecode[offset:])
	logging.Logger.Debug().Uint32("factDepIndexOffset", factDepIndexOffset).Msg("Read fact dependency index offset")
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
		logging.Logger.Debug().Str("ruleName", name).Int("byteOffset", adjustedByteOffset).Msg("Read rule execution index entry")
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
		logging.Logger.Debug().Str("fact", fact).Strs("rules", rules).Msg("Read fact rule index entry")
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
		logging.Logger.Debug().Str("rule", rule).Strs("facts", facts).Msg("Read fact dependency index entry")
	}

	logging.Logger.Info().Msg("Engine initialized from bytecode")
	return engine, nil
}

func (e *Engine) ProcessFactUpdate(factName string, factValue interface{}) {
	logging.Logger.Info().Str("factName", factName).Interface("factValue", factValue).Msg("Processing fact update")

	// Update the fact value in the store
	if num, ok := factValue.(int); ok {
		e.Facts[factName] = float64(num)
	} else if num, ok := factValue.(float32); ok {
		e.Facts[factName] = float64(num)
	} else {
		e.Facts[factName] = factValue
	}

	// Find all rules that reference the updated fact
	ruleNames, ok := e.factRuleIndex[factName]
	if !ok {
		logging.Logger.Info().Str("factName", factName).Msg("No rules found for the updated fact")
		return
	}

	logging.Logger.Debug().Str("factName", factName).Strs("ruleNames", ruleNames).Msg("Found rules referencing the updated fact")

	// Create a set of all facts that need to be queried (excluding the fact that triggered the update)
	factsToQuery := make(map[string]struct{})
	for _, ruleName := range ruleNames {
		for _, dep := range e.factDependencyIndex {
			if dep.RuleName == ruleName {
				for _, fact := range dep.Facts {
					if fact != factName {
						factsToQuery[fact] = struct{}{}
					}
				}
			}
		}
	}

	// Convert the set to a slice
	var factKeys []string
	for fact := range factsToQuery {
		factKeys = append(factKeys, fact)
	}

	factValues := make(map[string]interface{})
	var err error
	// Query the KV store for the required facts
	if len(factKeys) > 0 {
		factValues, err = e.store.MGetFacts(factKeys...)
		logging.Logger.Debug().Strs("facts", factKeys).Interface("values", factValues).Msg("Retrieved facts from KV store")
		if err != nil {
			logging.Logger.Error().Err(err).Msg("Failed to retrieve facts from KV store")
		}
	}

	// Update local fact store with retrieved facts
	var missingFacts []string
	for fact, value := range factValues {
		if value != nil {
			e.Facts[fact] = value
		} else {
			// Fact does not exist in the store
			logging.Logger.Warn().Str("fact", fact).Msg("Fact not found in store")
			delete(e.Facts, fact)
			missingFacts = append(missingFacts, fact)
		}
	}

	// Remove rules that depend on missing facts from ruleNames
	for _, missingFact := range missingFacts {
		for i := 0; i < len(ruleNames); i++ {
			ruleName := ruleNames[i]
			for _, dep := range e.factDependencyIndex {
				if dep.RuleName == ruleName {
					for _, fact := range dep.Facts {
						if fact == missingFact {
							// Remove the rule from ruleNames
							ruleNames = append(ruleNames[:i], ruleNames[i+1:]...)
							i--
							logging.Logger.Warn().
								Str("ruleName", ruleName).
								Str("missingFact", missingFact).
								Msg("Removing rule due to missing fact")
							break
						}
					}
					if len(ruleNames) == 0 {
						break
					}
				}
			}
		}
	}

	// Evaluate each rule
	for _, ruleName := range ruleNames {
		e.evaluateRule(ruleName)
	}
}

func (e *Engine) evaluateRule(ruleName string) {
	logging.Logger.Debug().Str("ruleName", ruleName).Msg("Evaluating rule")

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
		logging.Logger.Warn().Str("ruleName", ruleName).Msg("Rule not found in ruleExecutionIndex")
		return
	}

	logging.Logger.Debug().Str("ruleName", ruleName).Int("offset", ruleOffset).Msg("Found rule in ruleExecutionIndex")

	offset := ruleOffset
	var action compiler.Action

	var factValue interface{}
	var constValue interface{}
	var comparisonResult bool

	for offset < len(e.bytecode) {
		opcode := compiler.Opcode(e.bytecode[offset])
		offset++

		logging.Logger.Debug().Uint8("opcode", uint8(opcode)).Int("offset", offset-1).Msg("Executing opcode")

		switch opcode {
		case compiler.RULE_START:
			ruleNameLength := int(e.bytecode[offset])
			logging.Logger.Debug().Msg("Encountered RULE_START opcode")
			offset++
			ruleName := string(e.bytecode[offset : offset+ruleNameLength])
			offset += ruleNameLength
			logging.Logger.Debug().Str("ruleName", ruleName).Msg("Encountered rule name")
			continue
		case compiler.RULE_END:
			logging.Logger.Debug().Msg("Encountered RULE_END opcode")
			return
		case compiler.LOAD_FACT_FLOAT:
			nameLen := int(e.bytecode[offset])
			offset++
			factName := string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			factValue = e.Facts[factName]
			logging.Logger.Debug().Str("factName", factName).Interface("factValue", factValue).Msg("Encountered LOAD_FACT_FLOAT opcode")
		case compiler.LOAD_FACT_STRING:
			nameLen := int(e.bytecode[offset])
			offset++
			factName := string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			factValue = e.Facts[factName]
			logging.Logger.Debug().Str("factName", factName).Interface("factValue", factValue).Msg("Encountered LOAD_FACT_STRING opcode")
		case compiler.LOAD_FACT_BOOL:
			nameLen := int(e.bytecode[offset])
			offset++
			factName := string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			factValue = e.Facts[factName]
			logging.Logger.Debug().Str("factName", factName).Interface("factValue", factValue).Msg("Encountered LOAD_FACT_BOOL opcode")
		case compiler.LOAD_CONST_FLOAT:
			bits := binary.LittleEndian.Uint64(e.bytecode[offset : offset+8])
			constValue = math.Float64frombits(bits)
			offset += 8
			logging.Logger.Debug().Float64("constValue", constValue.(float64)).Msg("Encountered LOAD_CONST_FLOAT opcode")
		case compiler.LOAD_CONST_STRING:
			nameLen := int(e.bytecode[offset])
			offset++
			constValue = string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			logging.Logger.Debug().Str("constValue", constValue.(string)).Msg("Encountered LOAD_CONST_STRING opcode")
		case compiler.LOAD_CONST_BOOL:
			constValue = e.bytecode[offset] == 1
			offset++
			logging.Logger.Debug().Bool("constValue", constValue.(bool)).Msg("Encountered LOAD_CONST_BOOL opcode")
		case compiler.EQ_FLOAT, compiler.EQ_STRING, compiler.EQ_BOOL,
			compiler.NEQ_FLOAT, compiler.NEQ_STRING, compiler.NEQ_BOOL,
			compiler.LT_FLOAT, compiler.LTE_FLOAT, compiler.GT_FLOAT, compiler.GTE_FLOAT:
			comparisonResult = e.compare(factValue, constValue, opcode)
			logging.Logger.Debug().Bool("comparisonResult", comparisonResult).Msg("Encountered comparison opcode")
		case compiler.JUMP_IF_FALSE:
			jumpOffset := int(binary.LittleEndian.Uint32(e.bytecode[offset : offset+4]))
			offset += 4
			logging.Logger.Debug().Int("jumpOffset", jumpOffset).Msg("Encountered JUMP_IF_FALSE opcode")
			if !comparisonResult {
				offset = offset + jumpOffset
			}
		case compiler.JUMP_IF_TRUE:
			jumpOffset := int(binary.LittleEndian.Uint32(e.bytecode[offset : offset+4]))
			offset += 4
			logging.Logger.Debug().Int("jumpOffset", jumpOffset).Msg("Encountered JUMP_IF_TRUE opcode")
			if comparisonResult {
				offset = offset + jumpOffset
			}
		case compiler.ACTION_VALUE_FLOAT:
			bits := binary.LittleEndian.Uint64(e.bytecode[offset : offset+8])
			actionValue := math.Float64frombits(bits)
			offset += 8
			action.Value = actionValue
			logging.Logger.Debug().Float64("actionValue", actionValue).Msg("Encountered ACTION_VALUE_FLOAT opcode")
		case compiler.ACTION_VALUE_STRING:
			nameLen := int(e.bytecode[offset])
			offset++
			actionValue := string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			action.Value = actionValue
			logging.Logger.Debug().Str("actionValue", actionValue).Msg("Encountered ACTION_VALUE_STRING opcode")
		case compiler.ACTION_VALUE_BOOL:
			actionValue := e.bytecode[offset] == 1
			offset++
			action.Value = actionValue
			logging.Logger.Debug().Bool("actionValue", actionValue).Msg("Encountered ACTION_VALUE_BOOL opcode")
		case compiler.ACTION_START:
			logging.Logger.Debug().Msg("Encountered ACTION_START opcode")
		case compiler.ACTION_END:
			logging.Logger.Debug().Msg("Encountered ACTION_END opcode")
			e.executeAction(action)
		case compiler.LABEL:
			offset += 4
			logging.Logger.Debug().Msg("Encountered LABEL opcode")
		case compiler.ACTION_TYPE:
			nameLen := int(e.bytecode[offset])
			offset++
			action.Type = string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			logging.Logger.Debug().Str("actionType", action.Type).Msg("Encountered ACTION_TYPE opcode")
		case compiler.ACTION_TARGET:
			nameLen := int(e.bytecode[offset])
			offset++
			action.Target = string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			logging.Logger.Debug().Str("actionTarget", action.Target).Msg("Encountered ACTION_TARGET opcode")
		default:
			logging.Logger.Warn().Uint8("opcode", uint8(opcode)).Msg("Unknown opcode")
		}
	}
}

func (e *Engine) compare(factValue, constValue interface{}, opcode compiler.Opcode) bool {
	switch opcode {

	case compiler.EQ_FLOAT:
		return factValue.(float64) == constValue.(float64)
	case compiler.EQ_STRING:
		return factValue.(string) == constValue.(string)
	case compiler.EQ_BOOL:
		return factValue.(bool) == constValue.(bool)
	case compiler.NEQ_FLOAT:
		return factValue.(float64) != constValue.(float64)
	case compiler.NEQ_STRING:
		return factValue.(string) != constValue.(string)
	case compiler.NEQ_BOOL:
		return factValue.(bool) != constValue.(bool)
	case compiler.LT_FLOAT:
		return factValue.(float64) < constValue.(float64)
	case compiler.LTE_FLOAT:
		return factValue.(float64) <= constValue.(float64)
	case compiler.GT_FLOAT:
		return factValue.(float64) > constValue.(float64)
	case compiler.GTE_FLOAT:
		return factValue.(float64) >= constValue.(float64)
	default:
		logging.Logger.Warn().Uint8("opcode", uint8(opcode)).Msg("Unknown comparison opcode")
		return false
	}
}

func (e *Engine) executeAction(action compiler.Action) {
	switch action.Type {
	case "updateStore":
		factName := action.Target
		factValue := action.Value

		// Update the fact value in the local fact store
		e.Facts[factName] = factValue

		// Send the fact update to the store
		err := e.store.SetFact(factName, factValue)
		if err != nil {
			logging.Logger.Error().Err(err).Str("factName", factName).Interface("factValue", factValue).Msg("Failed to update fact in store")
			return
		}

		logging.Logger.Info().Str("factName", factName).Interface("factValue", factValue).Msg("Updated fact in store")

		// ****************************************
		//  If we want to automaticaly trigger a cascading rule(s)
		//  then this is where we would do it
		// Trigger the fact update processing
		// e.ProcessFactUpdate(factName, factValue)

	// Add more action types as needed
	default:
		logging.Logger.Warn().Str("type", action.Type).Msg("Unknown action type")
	}
}
