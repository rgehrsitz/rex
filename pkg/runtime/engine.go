// rex/pkg/runtime/engine.go

package runtime

import (
	"encoding/binary"
	"math"
	"os"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/scripting"
	"rgehrsitz/rex/pkg/store"
	"strconv"
	"strings"
	"time"

	"rgehrsitz/rex/pkg/logging"
)

type Engine struct {
	bytecode            []byte
	ruleExecutionIndex  []compiler.RuleExecutionIndex
	factRuleIndex       map[string][]string
	factDependencyIndex []compiler.FactDependencyIndex
	Facts               map[string]interface{}
	store               store.Store
	priorityThreshold   int
	ScriptEngine        *scripting.SafeVM
}

// New method to create an engine from a file
func NewEngineFromFile(filename string, store store.Store, priorityThreshold int) (*Engine, error) {

	bytecode, err := os.ReadFile(filename)
	if err != nil {
		return nil, logging.NewError(logging.ErrorTypeRuntime, "Failed to read bytecode file", err, map[string]interface{}{"filename": filename})
	}
	logging.Logger.Debug().Int("bytecodeLength", len(bytecode)).Msg("Read bytecode file")

	engine := &Engine{
		bytecode:            bytecode,
		ruleExecutionIndex:  make([]compiler.RuleExecutionIndex, 0),
		factRuleIndex:       make(map[string][]string),
		factDependencyIndex: make([]compiler.FactDependencyIndex, 0),
		Facts:               make(map[string]interface{}),
		store:               store,
		priorityThreshold:   priorityThreshold,
		ScriptEngine:        scripting.NewSafeVM(),
	}

	offset := 0

	// Read header
	if offset+28 > len(bytecode) {
		return nil, logging.NewError(logging.ErrorTypeRuntime, "Bytecode file too short for header", nil, nil)
	}
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
	logging.Logger.Debug().Int("offset", offset).Msg("Starting to read rule execution index")
	for i := 0; i < int(numRules); i++ {
		if offset+4 > len(bytecode) {
			return nil, logging.NewError(logging.ErrorTypeRuntime, "Unexpected end of bytecode while reading rule execution index", nil, nil)
		}
		nameLen := int(binary.LittleEndian.Uint32(bytecode[offset:]))
		offset += 4
		if offset+nameLen+4 > len(bytecode) {
			return nil, logging.NewError(logging.ErrorTypeRuntime, "Unexpected end of bytecode while reading rule name", nil, nil)
		}
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

	go engine.StartFactProcessing()

	logging.Logger.Info().Msg("Engine initialized from bytecode")

	return engine, nil
}

func (e *Engine) ProcessFactUpdate(factName string, factValue interface{}) {
	logging.Logger.Debug().Str("factName", factName).Interface("factValue", factValue).Msg("Processing fact update")

	// Update the fact value in the store
	if num, ok := factValue.(int); ok {
		e.Facts[factName] = float64(num)
	} else if num, ok := factValue.(float32); ok {
		e.Facts[factName] = float64(num)
	} else {
		e.Facts[factName] = factValue
	}

	// **NEW LOGIC START**: Check for wildcard patterns
	if strings.Contains(factName, "*") {
		logging.Logger.Debug().Str("factName", factName).Msg("Wildcard pattern detected in fact name")

		// Retrieve all matching keys for the wildcard pattern
		keys, err := e.store.ScanFacts(strings.ReplaceAll(factName, "*", "*"))
		if err != nil {
			logging.Logger.Error().Err(err).Str("factName", factName).Msg("Failed to scan wildcard facts from Redis")
			return
		}

		// Query Redis for all matching facts
		factValues, err := e.store.MGetFacts(keys...)
		if err != nil {
			logging.Logger.Error().Err(err).Strs("facts", keys).Msg("Failed to retrieve facts from Redis")
			return
		}

		// Update the local store with all retrieved wildcard facts
		for fact, value := range factValues {
			e.Facts[fact] = value
		}
	}
	// Find all rules that reference the updated fact
	ruleNames, ok := e.factRuleIndex[factName]
	if !ok {
		logging.Logger.Debug().Str("factName", factName).Msg("No rules found for the updated fact")
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
		logging.Logger.Debug().Str("ruleName", ruleName).Msg("Evaluating rule")
		err := e.evaluateRule(ruleName)
		if err != nil {
			logging.Logger.Error().Err(err).Str("ruleName", ruleName).Msg("Failed to evaluate rule")
			// Handle the error as needed, e.g., stop processing further rules
			return
		}
	}

	logging.Logger.Debug().Str("factName", factName).Interface("factValue", factValue).Msg("Finished processing fact update")
}

func (e *Engine) evaluateRule(ruleName string) error {
	logging.Logger.Debug().
		Str("ruleName", ruleName).
		Msg("Starting rule evaluation")
	logging.Logger.Debug().
		Interface("facts", e.Facts).
		Msg("Current facts")

	var ruleOffset int
	var rulePriority int
	found := false
	for _, r := range e.ruleExecutionIndex {
		if r.RuleName == ruleName {
			ruleOffset = r.ByteOffset
			rulePriority = r.Priority
			found = true
			break
		}
	}

	if !found {
		return logging.NewError(logging.ErrorTypeRuntime, "Rule not found in ruleExecutionIndex", nil, map[string]interface{}{"ruleName": ruleName})
	}

	logging.Logger.Debug().Str("ruleName", ruleName).Int("offset", ruleOffset).Int("priority", rulePriority).Msg("Found rule in ruleExecutionIndex")

	offset := ruleOffset
	var action compiler.Action

	var factValue interface{}
	var constValue interface{}
	var comparisonResult bool

	relevantFacts := make(map[string]interface{})
	ruleTriggered := false

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

		case compiler.PRIORITY:
			bits := binary.LittleEndian.Uint32(e.bytecode[offset : offset+5])
			rulePriority = int(bits)
			offset += 4
			logging.Logger.Debug().Int("priority", rulePriority).Msg("Encountered PRIORITY opcode")
			continue

		case compiler.RULE_END:
			if ruleTriggered && rulePriority <= e.priorityThreshold {
				logging.Logger.Info().
					Str("ruleName", ruleName).
					Int("priority", rulePriority).
					Interface("relevantFacts", relevantFacts).
					Msg("High-priority rule triggered")
			}
			return nil

		case compiler.LOAD_FACT_FLOAT, compiler.LOAD_FACT_STRING, compiler.LOAD_FACT_BOOL:
			nameLen := int(e.bytecode[offset])
			offset++
			factName := string(e.bytecode[offset : offset+nameLen])
			offset += nameLen

			factValue = e.Facts[factName]
			relevantFacts[factName] = factValue
			logging.Logger.Debug().Str("factName", factName).Interface("factValue", factValue).Msg("Loaded fact")

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
			compiler.LT_FLOAT, compiler.LTE_FLOAT, compiler.GT_FLOAT, compiler.GTE_FLOAT,
			compiler.CONTAINS_STRING, compiler.NOT_CONTAINS_STRING:
			comparisonResult = e.compare(factValue, constValue, opcode)
			if comparisonResult {
				ruleTriggered = true
			}
			logging.Logger.Debug().Bool("comparisonResult", comparisonResult).Msg("Comparison result")

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
			err := e.executeAction(action)
			if err != nil {
				logging.Logger.Error().Err(err).Msg("Failed to execute action")
				return err
			}

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

		case compiler.SCRIPT_DEF:
			logging.Logger.Debug().Msg("Encountered SCRIPT_DEF opcode")
			scriptNameLen := int(e.bytecode[offset])
			offset++
			scriptName := string(e.bytecode[offset : offset+scriptNameLen])
			offset += scriptNameLen

			paramsCount := int(e.bytecode[offset])
			offset++
			params := make([]string, paramsCount)
			for i := 0; i < paramsCount; i++ {
				paramLen := int(e.bytecode[offset])
				offset++
				params[i] = string(e.bytecode[offset : offset+paramLen])
				offset += paramLen
			}

			bodyLen := int(e.bytecode[offset])
			offset++
			body := string(e.bytecode[offset : offset+bodyLen])
			offset += bodyLen

			script := compiler.Script{
				Params: params,
				Body:   body,
			}
			err := e.ScriptEngine.SetScript(scriptName, script)
			if err != nil {
				return logging.NewError(logging.ErrorTypeRuntime, "Failed to set script", err, map[string]interface{}{"ruleName": ruleName, "scriptName": scriptName})
			}
			logging.Logger.Debug().Str("scriptName", scriptName).Str("body", body).Strs("params", params).Msg("Script defined")

		case compiler.SCRIPT_CALL:
			logging.Logger.Debug().Msg("Encountered SCRIPT_CALL opcode")
			scriptNameLen := int(e.bytecode[offset])
			offset++
			scriptName := string(e.bytecode[offset : offset+scriptNameLen])
			offset += scriptNameLen

			logging.Logger.Debug().Str("scriptName", scriptName).Msg("Calling script")

			paramsCount := int(e.bytecode[offset])
			offset++
			params := make(map[string]interface{})
			for i := 0; i < paramsCount; i++ {
				paramNameLen := int(e.bytecode[offset])
				offset++
				paramName := string(e.bytecode[offset : offset+paramNameLen])
				offset += paramNameLen

				params[paramName] = e.Facts[paramName]
			}

			logging.Logger.Debug().Interface("scriptName", scriptName).Interface("params", params).Msg("Script parameters")

			action.Value = map[string]interface{}{
				"scriptName": scriptName,
				"params":     params,
			}

			err := e.executeAction(action)
			if err != nil {
				logging.Logger.Error().Err(err).Str("scriptName", scriptName).Interface("params", params).Msg("Failed to run script")
				return logging.NewError(logging.ErrorTypeRuntime, "Failed to run script", err, map[string]interface{}{"ruleName": ruleName, "scriptName": scriptName})
			}

		case compiler.WILDCARD_SCAN:
			// Extract the pattern length
			patternLength := int(e.bytecode[offset])
			offset++

			// Extract the pattern string
			pattern := string(e.bytecode[offset : offset+patternLength])
			offset += patternLength

			logging.Logger.Debug().Str("pattern", pattern).Msg("Executing WILDCARD_SCAN opcode")

			// Perform the scan using the pattern
			keys, err := e.store.ScanFacts(pattern)
			if err != nil {
				logging.Logger.Error().Err(err).Str("pattern", pattern).Msg("Failed to perform wildcard scan in Redis")
				return err
			}

			// Retrieve all matching facts
			factValues, err := e.store.MGetFacts(keys...)
			if err != nil {
				logging.Logger.Error().Err(err).Strs("keys", keys).Msg("Failed to retrieve facts from Redis")
				return err
			}

			// Update local facts with retrieved values
			for fact, value := range factValues {
				e.Facts[fact] = value
			}

		default:
			err := logging.NewError(logging.ErrorTypeRuntime, "Unknown opcode encountered", nil, map[string]interface{}{"opcode": opcode})
			logging.Logger.Warn().Err(err).Msg("Unknown opcode")
			return err
		}
	}

	logging.Logger.Debug().
		Str("ruleName", ruleName).
		Bool("ruleTriggered", ruleTriggered).
		Msg("Finished rule evaluation")

	return nil
}

// compare compares the given `factValue` and `constValue` based on the provided `opcode`.
// It returns true if the comparison is successful, otherwise false.
func (e *Engine) compare(factValue, constValue interface{}, opcode compiler.Opcode) bool {
	if factValue == nil || constValue == nil {
		logging.Logger.Warn().Msgf("Nil value encountered in comparison: factValue=%v, constValue=%v", factValue, constValue)
		return false
	}

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
	case compiler.CONTAINS_STRING:
		return strings.Contains(factValue.(string), constValue.(string))
	case compiler.NOT_CONTAINS_STRING:
		return !strings.Contains(factValue.(string), constValue.(string))
	default:
		logging.Logger.Warn().Uint8("opcode", uint8(opcode)).Msg("Unknown comparison opcode")
		return false
	}
}

func (e *Engine) executeAction(action compiler.Action) error {
	logging.Logger.Debug().
		Str("actionType", action.Type).
		Str("actionTarget", action.Target).
		Interface("actionValue", action.Value).
		Msg("Executing action")

	switch action.Type {
	case "updateStore":
		factName := action.Target
		factValue := action.Value

		// Check if the factValue is a script call
		if scriptInfo, ok := factValue.(map[string]interface{}); ok {
			if scriptName, ok := scriptInfo["scriptName"].(string); ok {
				params := scriptInfo["params"].(map[string]interface{})
				logging.Logger.Debug().
					Str("scriptName", scriptName).
					Interface("params", params).
					Msg("Executing script")
				result, err := e.ScriptEngine.RunScript(scriptName, params, 100*time.Millisecond)
				if err != nil {
					logging.Logger.Error().Err(err).Str("scriptName", scriptName).Msg("Failed to run script")
					return err
				}
				factValue = result
				logging.Logger.Debug().
					Str("scriptName", scriptName).
					Interface("scriptResult", result).
					Msg("Script executed")
			}
		}

		// Update the fact value in the local fact store
		e.Facts[factName] = factValue

		logging.Logger.Debug().
			Str("factName", factName).
			Interface("factValue", factValue).
			Msg("Fact updated in local store")

		// Send the fact update to the store via a set and publish command
		err := e.store.SetAndPublishFact(factName, factValue)
		if err != nil {
			logging.Logger.Error().Err(err).Str("factName", factName).Interface("factValue", factValue).Msg("Failed to update fact in Redis store")
			return err
		}

		logging.Logger.Debug().Str("factName", factName).Interface("factValue", factValue).Msg("Updated fact in Redis store")

		// Verify the fact was stored correctly
		storedValue, err := e.store.GetFact(factName)
		if err != nil {
			logging.Logger.Error().Err(err).Str("factName", factName).Msg("Failed to retrieve fact from Redis store")
		} else {
			logging.Logger.Debug().Str("factName", factName).Interface("storedValue", storedValue).Msg("Retrieved fact from Redis store")
		}

	default:
		err := logging.NewError(logging.ErrorTypeRuntime, "Unknown action type encountered", nil, map[string]interface{}{"type": action.Type})
		logging.Logger.Warn().Err(err).Msg("Unknown action type")
		return err
	}

	logging.Logger.Debug().
		Str("actionType", action.Type).
		Str("actionTarget", action.Target).
		Msg("Finished executing action")

	return nil
}

func (e *Engine) StartFactProcessing() {
	logging.Logger.Info().Msg("Starting fact processing loop")
	factChan := e.store.ReceiveFacts()

	for msg := range factChan {
		logging.Logger.Debug().
			Str("channel", msg.Channel).
			Str("payload", msg.Payload).
			Msg("Received fact update")

		parts := strings.SplitN(msg.Payload, "=", 2)
		if len(parts) != 2 {
			logging.Logger.Warn().
				Str("payload", msg.Payload).
				Msg("Invalid fact update format")
			continue
		}

		factName := parts[0]
		factValue := parts[1]

		// Convert factValue to appropriate type
		var value interface{}
		if floatVal, err := strconv.ParseFloat(factValue, 64); err == nil {
			value = floatVal
		} else if boolVal, err := strconv.ParseBool(factValue); err == nil {
			value = boolVal
		} else {
			value = factValue
		}

		e.ProcessFactUpdate(factName, value)
	}
}

func (e *Engine) Shutdown() {
	logging.Logger.Info().Msg("Initiating engine shutdown")

	// Shutdown performance monitoring

	logging.Logger.Info().Msg("Engine shutdown complete")
}
