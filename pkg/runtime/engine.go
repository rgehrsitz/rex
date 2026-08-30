// rex/pkg/runtime/engine.go

package runtime

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/scripting"
	"rgehrsitz/rex/pkg/store"
	"strings"
	"time"

	"rgehrsitz/rex/pkg/logging"
)

// DefaultMaxActionsPerEvaluation bounds a rule evaluation unless a caller
// explicitly configures another limit.
const DefaultMaxActionsPerEvaluation = 32

type Engine struct {
	bytecode                []byte
	ruleExecutionIndex      []compiler.RuleExecutionIndex
	factRuleIndex           map[string][]string
	factDependencyIndex     []compiler.FactDependencyIndex
	Facts                   map[string]interface{}
	store                   store.ContextStore
	priorityThreshold       int
	maxActionsPerEvaluation int
	scriptsEnabled          bool
	ScriptEngine            *scripting.SafeVM
	executionObserver       ExecutionObserver
}

// SetScriptsEnabled controls whether this engine may evaluate embedded
// JavaScript. Scripts are disabled by default because the in-process Otto VM is
// not an isolation boundary. Enable them only for rulesets from trusted authors.
func (e *Engine) SetScriptsEnabled(enabled bool) {
	e.scriptsEnabled = enabled
	if enabled {
		logging.Logger.Warn().Msg("Script execution enabled for trusted rulesets")
	}
}

// SetMaxActionsPerEvaluation caps actions executed for one rule evaluation.
// A non-positive limit disables the cap for compatibility with embedded uses.
func (e *Engine) SetMaxActionsPerEvaluation(limit int) {
	e.maxActionsPerEvaluation = limit
}

// New method to create an engine from a file
func NewEngineFromFile(filename string, store store.ContextStore, priorityThreshold int) (*Engine, error) {

	bytecode, err := os.ReadFile(filename)
	if err != nil {
		return nil, logging.NewError(logging.ErrorTypeRuntime, "Failed to read bytecode file", err, map[string]interface{}{"filename": filename})
	}
	if err := validateBytecode(bytecode); err != nil {
		return nil, logging.NewError(logging.ErrorTypeRuntime, fmt.Sprintf("Invalid bytecode file: %v", err), err, map[string]interface{}{"filename": filename})
	}
	logging.Logger.Debug().Int("bytecodeLength", len(bytecode)).Msg("Read bytecode file")

	engine := &Engine{
		bytecode:                bytecode,
		ruleExecutionIndex:      make([]compiler.RuleExecutionIndex, 0),
		factRuleIndex:           make(map[string][]string),
		factDependencyIndex:     make([]compiler.FactDependencyIndex, 0),
		Facts:                   make(map[string]interface{}),
		store:                   store,
		priorityThreshold:       priorityThreshold,
		maxActionsPerEvaluation: DefaultMaxActionsPerEvaluation,
		ScriptEngine:            scripting.NewSafeVM(),
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

	logging.Logger.Info().Msg("Engine initialized from bytecode")

	return engine, nil
}

func (e *Engine) ProcessFactUpdate(factName string, factValue interface{}) {
	if err := e.ProcessFactUpdateContext(context.Background(), factName, factValue); err != nil {
		logging.Logger.Error().Err(err).Str("factName", factName).Msg("Failed to process fact update")
	}
}

// ProcessFactUpdateContext evaluates rules affected by a fact update with a
// caller-owned context for store operations and action execution.
func (e *Engine) ProcessFactUpdateContext(ctx context.Context, factName string, factValue interface{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	logger := traceLogger(ctx)

	logger.Debug().Str("factName", factName).Interface("factValue", factValue).Msg("Processing fact update")

	// Update the fact value in the store
	if num, ok := factValue.(int); ok {
		e.Facts[factName] = float64(num)
	} else if num, ok := factValue.(float32); ok {
		e.Facts[factName] = float64(num)
	} else {
		e.Facts[factName] = factValue
	}

	// Find all rules that reference the updated fact
	indexedRuleNames, ok := e.factRuleIndex[factName]
	if !ok {
		logger.Info().
			Str("event", "rule_evaluation_candidates").
			Str("fact_name", factName).
			Strs("rule_names", []string{}).
			Msg("Selected rule evaluation candidates")
		return nil
	}
	ruleNames := indexedRuleNames

	logger.Info().
		Str("event", "rule_evaluation_candidates").
		Str("fact_name", factName).
		Strs("rule_names", ruleNames).
		Msg("Selected rule evaluation candidates")

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
		factValues, err = e.store.MGetFactsContext(ctx, factKeys...)
		logger.Debug().Strs("facts", factKeys).Interface("values", factValues).Msg("Retrieved facts from KV store")
		if err != nil {
			logger.Error().
				Err(err).
				Str("event", "fact_dependencies_failed").
				Str("fact_name", factName).
				Msg("Failed to retrieve dependent facts")
			return err
		}
	}

	// Update local fact store with retrieved facts
	var missingFacts []string
	for fact, value := range factValues {
		if value != nil {
			e.Facts[fact] = value
		} else {
			// Fact does not exist in the store
			logger.Warn().Str("fact", fact).Msg("Fact not found in store")
			delete(e.Facts, fact)
			missingFacts = append(missingFacts, fact)
		}
	}
	if len(missingFacts) > 0 {
		// Filtering must not mutate the persistent index's backing array.
		ruleNames = append([]string(nil), indexedRuleNames...)
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
							logger.Warn().
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
		logger.Debug().Str("ruleName", ruleName).Msg("Evaluating rule")
		err := e.evaluateRuleContext(ctx, ruleName)
		if err != nil {
			logger.Error().
				Err(err).
				Str("event", "rule_evaluation_failed").
				Str("rule_name", ruleName).
				Msg("Failed to evaluate rule")
			// Handle the error as needed, e.g., stop processing further rules
			return err
		}
	}

	logger.Debug().Str("factName", factName).Interface("factValue", factValue).Msg("Finished processing fact update")
	return nil
}

func (e *Engine) evaluateRule(ruleName string) error {
	return e.evaluateRuleContext(context.Background(), ruleName)
}

func (e *Engine) evaluateRuleContext(ctx context.Context, ruleName string) error {
	logger := traceLogger(ctx)
	logger.Debug().
		Str("ruleName", ruleName).
		Msg("Starting rule evaluation")
	logger.Debug().
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

	logger.Debug().Str("ruleName", ruleName).Int("offset", ruleOffset).Int("priority", rulePriority).Msg("Found rule in ruleExecutionIndex")

	offset := ruleOffset
	var action compiler.Action

	var factValue interface{}
	var constValue interface{}
	var comparisonResult bool
	var comparisonFactName string
	actionsAttempted := 0

	relevantFacts := make(map[string]interface{})
	ruleTriggered := false

	for offset < len(e.bytecode) {
		if err := ctx.Err(); err != nil {
			return err
		}

		opcode := compiler.Opcode(e.bytecode[offset])
		offset++

		logger.Debug().Uint8("opcode", uint8(opcode)).Int("offset", offset-1).Msg("Executing opcode")

		switch opcode {
		case compiler.RULE_START:
			ruleNameLength := int(e.bytecode[offset])
			logger.Debug().Msg("Encountered RULE_START opcode")
			offset++
			ruleName := string(e.bytecode[offset : offset+ruleNameLength])
			offset += ruleNameLength
			logger.Debug().Str("ruleName", ruleName).Msg("Encountered rule name")
			continue

		case compiler.PRIORITY:
			bits := binary.LittleEndian.Uint32(e.bytecode[offset : offset+5])
			rulePriority = int(bits)
			offset += 4
			logger.Debug().Int("priority", rulePriority).Msg("Encountered PRIORITY opcode")
			continue

		case compiler.RULE_END:
			logger.Info().
				Str("event", "rule_evaluation_completed").
				Str("rule_name", ruleName).
				Int("priority", rulePriority).
				Bool("matched", actionsAttempted > 0).
				Int("actions_attempted", actionsAttempted).
				Msg("Completed rule evaluation")
			if ruleTriggered && rulePriority <= e.priorityThreshold {
				logger.Info().
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
			comparisonFactName = factName

			factValue = e.Facts[factName]
			relevantFacts[factName] = factValue
			logger.Debug().Str("factName", factName).Interface("factValue", factValue).Msg("Loaded fact")

		case compiler.LOAD_CONST_FLOAT:
			bits := binary.LittleEndian.Uint64(e.bytecode[offset : offset+8])
			constValue = math.Float64frombits(bits)
			offset += 8
			logger.Debug().Float64("constValue", constValue.(float64)).Msg("Encountered LOAD_CONST_FLOAT opcode")

		case compiler.LOAD_CONST_STRING:
			nameLen := int(e.bytecode[offset])
			offset++
			constValue = string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			logger.Debug().Str("constValue", constValue.(string)).Msg("Encountered LOAD_CONST_STRING opcode")

		case compiler.LOAD_CONST_BOOL:
			constValue = e.bytecode[offset] == 1
			offset++
			logger.Debug().Bool("constValue", constValue.(bool)).Msg("Encountered LOAD_CONST_BOOL opcode")

		case compiler.EQ_FLOAT, compiler.EQ_STRING, compiler.EQ_BOOL,
			compiler.NEQ_FLOAT, compiler.NEQ_STRING, compiler.NEQ_BOOL,
			compiler.LT_FLOAT, compiler.LTE_FLOAT, compiler.GT_FLOAT, compiler.GTE_FLOAT,
			compiler.CONTAINS_STRING, compiler.NOT_CONTAINS_STRING:
			comparisonResult = e.compare(factValue, constValue, opcode)
			if comparisonResult {
				ruleTriggered = true
			}
			logger.Info().
				Str("event", "rule_condition_evaluated").
				Str("rule_name", ruleName).
				Str("fact_name", comparisonFactName).
				Bool("matched", comparisonResult).
				Msg("Evaluated rule condition")

		case compiler.JUMP_IF_FALSE:
			jumpOffset := int(binary.LittleEndian.Uint32(e.bytecode[offset : offset+4]))
			offset += 4
			logger.Debug().Int("jumpOffset", jumpOffset).Msg("Encountered JUMP_IF_FALSE opcode")
			if !comparisonResult {
				offset = offset + jumpOffset
			}

		case compiler.JUMP_IF_TRUE:
			jumpOffset := int(binary.LittleEndian.Uint32(e.bytecode[offset : offset+4]))
			offset += 4
			logger.Debug().Int("jumpOffset", jumpOffset).Msg("Encountered JUMP_IF_TRUE opcode")
			if comparisonResult {
				offset = offset + jumpOffset
			}

		case compiler.ACTION_VALUE_FLOAT:
			bits := binary.LittleEndian.Uint64(e.bytecode[offset : offset+8])
			actionValue := math.Float64frombits(bits)
			offset += 8
			action.Value = actionValue
			logger.Debug().Float64("actionValue", actionValue).Msg("Encountered ACTION_VALUE_FLOAT opcode")

		case compiler.ACTION_VALUE_STRING:
			nameLen := int(e.bytecode[offset])
			offset++
			actionValue := string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			action.Value = actionValue
			logger.Debug().Str("actionValue", actionValue).Msg("Encountered ACTION_VALUE_STRING opcode")

		case compiler.ACTION_VALUE_BOOL:
			actionValue := e.bytecode[offset] == 1
			offset++
			action.Value = actionValue
			logger.Debug().Bool("actionValue", actionValue).Msg("Encountered ACTION_VALUE_BOOL opcode")

		case compiler.ACTION_START:
			logger.Debug().Msg("Encountered ACTION_START opcode")

		case compiler.ACTION_END:
			logger.Debug().Msg("Encountered ACTION_END opcode")
			if e.maxActionsPerEvaluation > 0 && actionsAttempted >= e.maxActionsPerEvaluation {
				return fmt.Errorf("rule %q exceeded action limit of %d", ruleName, e.maxActionsPerEvaluation)
			}
			if actionsAttempted == 0 {
				e.recordRuleFired(ruleName)
			}
			actionsAttempted++
			err := e.executeActionContext(ctx, action)
			if err != nil {
				logger.Error().
					Err(err).
					Str("event", "action_failed").
					Str("action_type", action.Type).
					Str("action_target", action.Target).
					Msg("Failed to execute action")
				return err
			}

		case compiler.LABEL:
			offset += 4
			logger.Debug().Msg("Encountered LABEL opcode")

		case compiler.ACTION_TYPE:
			nameLen := int(e.bytecode[offset])
			offset++
			action.Type = string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			logger.Debug().Str("actionType", action.Type).Msg("Encountered ACTION_TYPE opcode")

		case compiler.ACTION_TARGET:
			nameLen := int(e.bytecode[offset])
			offset++
			action.Target = string(e.bytecode[offset : offset+nameLen])
			offset += nameLen
			logger.Debug().Str("actionTarget", action.Target).Msg("Encountered ACTION_TARGET opcode")

		case compiler.SCRIPT_DEF:
			logger.Debug().Msg("Encountered SCRIPT_DEF opcode")
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
			logger.Debug().Str("scriptName", scriptName).Str("body", body).Strs("params", params).Msg("Script defined")

		case compiler.SCRIPT_CALL:
			logger.Debug().Msg("Encountered SCRIPT_CALL opcode")
			scriptNameLen := int(e.bytecode[offset])
			offset++
			scriptName := string(e.bytecode[offset : offset+scriptNameLen])
			offset += scriptNameLen

			logger.Debug().Str("scriptName", scriptName).Msg("Calling script")

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

			logger.Debug().Interface("scriptName", scriptName).Interface("params", params).Msg("Script parameters")

			action.Value = map[string]interface{}{
				"scriptName": scriptName,
				"params":     params,
			}

		default:
			err := logging.NewError(logging.ErrorTypeRuntime, "Unknown opcode encountered", nil, map[string]interface{}{"opcode": opcode})
			logger.Warn().Err(err).Msg("Unknown opcode")
			return err
		}
	}

	logger.Debug().
		Str("ruleName", ruleName).
		Bool("ruleTriggered", ruleTriggered).
		Msg("Finished rule evaluation")

	return nil
}

// compare compares the given factValue and constValue based on the provided opcode.
// It returns false when either value does not match the opcode's expected type.
func (e *Engine) compare(factValue, constValue interface{}, opcode compiler.Opcode) bool {
	switch opcode {
	case compiler.EQ_FLOAT, compiler.NEQ_FLOAT, compiler.LT_FLOAT, compiler.LTE_FLOAT, compiler.GT_FLOAT, compiler.GTE_FLOAT:
		fact, factOK := factValue.(float64)
		constant, constantOK := constValue.(float64)
		if !factOK || !constantOK {
			return e.invalidComparison(factValue, constValue, opcode)
		}

		switch opcode {
		case compiler.EQ_FLOAT:
			return fact == constant
		case compiler.NEQ_FLOAT:
			return fact != constant
		case compiler.LT_FLOAT:
			return fact < constant
		case compiler.LTE_FLOAT:
			return fact <= constant
		case compiler.GT_FLOAT:
			return fact > constant
		case compiler.GTE_FLOAT:
			return fact >= constant
		}

	case compiler.EQ_STRING, compiler.NEQ_STRING, compiler.CONTAINS_STRING, compiler.NOT_CONTAINS_STRING:
		fact, factOK := factValue.(string)
		constant, constantOK := constValue.(string)
		if !factOK || !constantOK {
			return e.invalidComparison(factValue, constValue, opcode)
		}

		switch opcode {
		case compiler.EQ_STRING:
			return fact == constant
		case compiler.NEQ_STRING:
			return fact != constant
		case compiler.CONTAINS_STRING:
			return strings.Contains(fact, constant)
		case compiler.NOT_CONTAINS_STRING:
			return !strings.Contains(fact, constant)
		}

	case compiler.EQ_BOOL, compiler.NEQ_BOOL:
		fact, factOK := factValue.(bool)
		constant, constantOK := constValue.(bool)
		if !factOK || !constantOK {
			return e.invalidComparison(factValue, constValue, opcode)
		}

		if opcode == compiler.EQ_BOOL {
			return fact == constant
		}
		return fact != constant

	default:
		logging.Logger.Warn().Uint8("opcode", uint8(opcode)).Msg("Unknown comparison opcode")
	}

	return false
}

func (e *Engine) invalidComparison(factValue, constValue interface{}, opcode compiler.Opcode) bool {
	logging.Logger.Warn().
		Uint8("opcode", uint8(opcode)).
		Interface("factValue", factValue).
		Interface("constValue", constValue).
		Msg("Comparison values do not match the expected type")
	return false
}

func (e *Engine) executeAction(action compiler.Action) error {
	return e.executeActionContext(context.Background(), action)
}

func (e *Engine) executeActionContext(ctx context.Context, action compiler.Action) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	logger := traceLogger(ctx)
	skipped := false
	defer func() {
		switch {
		case err != nil:
			e.recordActionFailed(action, err)
		case skipped:
			e.recordActionSkipped(action)
		default:
			e.recordActionSucceeded(action)
		}
	}()

	logger.Debug().
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
				if !e.scriptsEnabled {
					logger.Warn().
						Str("event", "action_skipped").
						Str("outcome", "skipped").
						Str("action_type", action.Type).
						Str("scriptName", scriptName).
						Str("actionTarget", factName).
						Msg("Skipping script action because script execution is disabled; enable engine.scripts_enabled only for trusted rulesets")
					skipped = true
					return nil
				}
				params, ok := scriptInfo["params"].(map[string]interface{})
				if !ok {
					return fmt.Errorf("invalid script action parameters for %q", scriptName)
				}
				logger.Debug().
					Str("scriptName", scriptName).
					Interface("params", params).
					Msg("Executing script")
				result, err := e.ScriptEngine.RunScript(scriptName, params, 100*time.Millisecond)
				if err != nil {
					logger.Error().
						Err(err).
						Str("event", "action_failed").
						Str("action_type", action.Type).
						Str("action_target", action.Target).
						Str("scriptName", scriptName).
						Msg("Failed to run script")
					return err
				}
				factValue = result
				logger.Debug().
					Str("scriptName", scriptName).
					Interface("scriptResult", result).
					Msg("Script executed")
			}
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		// Update the fact value in the local fact store
		e.Facts[factName] = factValue

		logger.Debug().
			Str("factName", factName).
			Interface("factValue", factValue).
			Msg("Fact updated in local store")

		// Send the fact update to the store via a set and publish command
		err := e.store.SetAndPublishFactContext(ctx, factName, factValue)
		if err != nil {
			logger.Error().
				Err(err).
				Str("event", "action_failed").
				Str("action_type", action.Type).
				Str("action_target", action.Target).
				Str("factName", factName).
				Interface("factValue", factValue).
				Msg("Failed to update fact in Redis store")
			return err
		}

		logger.Debug().Str("factName", factName).Interface("factValue", factValue).Msg("Updated fact in Redis store")

		// Verify the fact was stored correctly
		storedValue, err := e.store.GetFactContext(ctx, factName)
		if err != nil {
			logger.Error().Err(err).Str("factName", factName).Msg("Failed to retrieve fact from Redis store")
		} else {
			logger.Debug().Str("factName", factName).Interface("storedValue", storedValue).Msg("Retrieved fact from Redis store")
		}

		logger.Info().
			Str("event", "action_completed").
			Str("outcome", "succeeded").
			Str("action_type", action.Type).
			Str("action_target", action.Target).
			Msg("Completed action")

	default:
		err := logging.NewError(logging.ErrorTypeRuntime, "Unknown action type encountered", nil, map[string]interface{}{"type": action.Type})
		logger.Warn().Err(err).Msg("Unknown action type")
		return err
	}

	logger.Debug().
		Str("actionType", action.Type).
		Str("actionTarget", action.Target).
		Msg("Finished executing action")

	return nil
}

func (e *Engine) Shutdown() {
	logging.Logger.Info().Msg("Initiating engine shutdown")

	// Shutdown performance monitoring

	logging.Logger.Info().Msg("Engine shutdown complete")
}
