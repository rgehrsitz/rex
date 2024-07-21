// rex/pkg/runtime/engine.go

package runtime

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/store"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"rgehrsitz/rex/pkg/logging"

	"github.com/shirou/gopsutil/v4/process"
)

type Engine struct {
	bytecode                    []byte
	ruleExecutionIndex          []compiler.RuleExecutionIndex
	factRuleIndex               map[string][]string
	factDependencyIndex         []compiler.FactDependencyIndex
	Facts                       map[string]interface{}
	store                       store.Store
	Stats                       EngineStats
	RuleStats                   map[string]*RuleStats
	FactStats                   map[string]*FactStats
	statsMutex                  sync.RWMutex
	priorityThreshold           int
	pid                         int32
	proc                        *process.Process
	enablePerformanceMonitoring bool
	stopMonitoring              chan struct{}
}

type EngineStats struct {
	TotalFactsProcessed int64
	TotalRulesProcessed int64
	TotalFactsUpdated   int64
	LastUpdateTime      time.Time
	EngineStartTime     time.Time
	CPUUsage            float64
	MemoryUsage         uint64
	GoroutineCount      int
	ErrorCount          int64
	WarningCount        int64
}

type RuleStats struct {
	Name               string
	ExecutionCount     int64
	LastExecutionTime  time.Time
	TotalExecutionTime time.Duration
	Priority           int
}

type FactStats struct {
	Name               string
	UpdateCount        int64
	LastUpdateTime     time.Time
	TotalUpdateLatency time.Duration
}

func (e *Engine) GetStats() map[string]interface{} {
	e.statsMutex.RLock()
	defer e.statsMutex.RUnlock()

	uptime := time.Since(e.Stats.EngineStartTime)
	uptimeStr := formatDuration(uptime)

	return map[string]interface{}{
		"TotalFactsProcessed": e.Stats.TotalFactsProcessed,
		"TotalRulesProcessed": e.Stats.TotalRulesProcessed,
		"TotalFactsUpdated":   e.Stats.TotalFactsUpdated,
		"LastUpdateTime":      e.Stats.LastUpdateTime.Format(time.RFC3339),
		"EngineUptime":        uptimeStr,
		"CPUUsage":            fmt.Sprintf("%.2f%%", e.Stats.CPUUsage),
		"MemoryUsage":         fmt.Sprintf("%.2f MB", float64(e.Stats.MemoryUsage)/(1024*1024)),
		"GoroutineCount":      e.Stats.GoroutineCount,
		"ErrorCount":          e.Stats.ErrorCount,
		"WarningCount":        e.Stats.WarningCount,
		"TotalRules":          len(e.ruleExecutionIndex),
		"TotalFacts":          len(e.Facts),
	}
}

// New method to create an engine from a file
func NewEngineFromFile(filename string, store store.Store, priorityThreshold int, enablePerformanceMonitoring bool) (*Engine, error) {

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
		Stats: EngineStats{
			EngineStartTime: time.Now(),
		},
		RuleStats:                   make(map[string]*RuleStats),
		FactStats:                   make(map[string]*FactStats),
		priorityThreshold:           priorityThreshold,
		pid:                         int32(os.Getpid()),
		enablePerformanceMonitoring: enablePerformanceMonitoring,
		stopMonitoring:              make(chan struct{}),
	}

	proc, err := process.NewProcess(engine.pid)
	if err != nil {
		return nil, logging.NewError(logging.ErrorTypeRuntime, "Failed to create process", err, nil)
	}
	engine.proc = proc

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

	// Initialize RuleStats for each rule
	for _, rule := range engine.ruleExecutionIndex {
		engine.RuleStats[rule.RuleName] = &RuleStats{
			Name:     rule.RuleName,
			Priority: rule.Priority,
		}
	}

	go engine.StartFactProcessing()

	logging.Logger.Info().Msg("Engine initialized from bytecode")

	// if enablePerformanceMonitoring {
	// 	engine.StartPerformanceMonitoring(time.Duration(EngineInterval) * time.Second) // or choose an appropriate interval
	// }

	return engine, nil
}

func (e *Engine) ProcessFactUpdate(factName string, factValue interface{}) {
	logging.Logger.Debug().Str("factName", factName).Interface("factValue", factValue).Msg("Processing fact update")

	e.statsMutex.Lock()
	e.Stats.TotalFactsProcessed++
	e.statsMutex.Unlock()

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

	e.statsMutex.Lock()
	e.Stats.TotalRulesProcessed++
	e.statsMutex.Unlock()

	var startTime time.Time
	if e.enablePerformanceMonitoring {
		startTime = time.Now()
		e.statsMutex.Lock()
		if ruleStats, ok := e.RuleStats[ruleName]; ok {
			ruleStats.ExecutionCount++
			ruleStats.LastExecutionTime = startTime
		}
		e.statsMutex.Unlock()
	}

	var ruleOffset int
	var rulePriority int
	found := false
	for _, rule := range e.ruleExecutionIndex {
		if rule.RuleName == ruleName {
			ruleOffset = rule.ByteOffset
			rulePriority = rule.Priority
			found = true
			break
		}
	}

	if !found {
		err := logging.NewError(logging.ErrorTypeRuntime, "Rule not found in ruleExecutionIndex", nil, map[string]interface{}{"ruleName": ruleName})
		logging.Logger.Warn().Err(err).Msg("Rule not found")
		return err
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
		default:
			err := logging.NewError(logging.ErrorTypeRuntime, "Unknown opcode encountered", nil, map[string]interface{}{"opcode": opcode})
			logging.Logger.Warn().Err(err).Msg("Unknown opcode")
			return err
		}
	}

	if e.enablePerformanceMonitoring {
		e.statsMutex.Lock()
		executionDuration := time.Since(startTime)
		if ruleStats, ok := e.RuleStats[ruleName]; ok {
			ruleStats.TotalExecutionTime += executionDuration
		}
		e.statsMutex.Unlock()
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

// executeAction now returns an error
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

		// Update the fact value in the local fact store
		e.Facts[factName] = factValue

		e.statsMutex.Lock()
		e.Stats.TotalFactsUpdated++
		e.statsMutex.Unlock()

		// Send the fact update to the store via a set and publish command
		err := e.store.SetAndPublishFact(factName, factValue)
		if err != nil {
			logging.Logger.Error().Err(err).Str("factName", factName).Interface("factValue", factValue).Msg("Failed to update fact in store")
			return err
		}

		logging.Logger.Debug().Str("factName", factName).Interface("factValue", factValue).Msg("Updated fact in store")

		// Trigger the fact update processing if needed
		// e.ProcessFactUpdate(factName, factValue)

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

func (e *Engine) updateCPUUsage() error {
	percentage, err := e.proc.Percent(0)
	if err != nil {
		return err
	}
	e.Stats.CPUUsage = percentage
	logging.Logger.Debug().Float64("cpuUsage", percentage).Msg("Updated CPU usage")
	return nil
}

func (e *Engine) updateMemoryUsage() error {
	memInfo, err := e.proc.MemoryInfo()
	if err != nil {
		return err
	}
	e.Stats.MemoryUsage = memInfo.RSS
	logging.Logger.Debug().Uint64("memoryUsage", memInfo.RSS).Msg("Updated memory usage")
	return nil
}

func (e *Engine) updateGoroutineCount() {
	count := runtime.NumGoroutine()
	e.Stats.GoroutineCount = count
	logging.Logger.Debug().Int("goroutineCount", count).Msg("Updated goroutine count")
}

// New method to update all system stats at once
func (e *Engine) updateSystemStats() {
	logging.Logger.Debug().Msg("Starting to update system stats")

	if err := e.updateCPUUsage(); err != nil {
		logging.Logger.Error().Err(err).Msg("Failed to update CPU usage")
	}
	if err := e.updateMemoryUsage(); err != nil {
		logging.Logger.Error().Err(err).Msg("Failed to update memory usage")
	}
	e.updateGoroutineCount()

	e.Stats.LastUpdateTime = time.Now()

	logging.Logger.Debug().
		Float64("cpuUsage", e.Stats.CPUUsage).
		Uint64("memoryUsage", e.Stats.MemoryUsage).
		Int("goroutineCount", e.Stats.GoroutineCount).
		Time("lastUpdateTime", e.Stats.LastUpdateTime).
		Msg("System stats updated")
}

func formatDuration(duration time.Duration) string {
	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60
	seconds := int(duration.Seconds()) % 60

	var sb strings.Builder
	if days > 0 {
		sb.WriteString(fmt.Sprintf("%dd ", days))
	}
	if hours > 0 {
		sb.WriteString(fmt.Sprintf("%dh ", hours))
	}
	if minutes > 0 {
		sb.WriteString(fmt.Sprintf("%dm ", minutes))
	}
	sb.WriteString(fmt.Sprintf("%ds", seconds))

	return sb.String()
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

func (e *Engine) StartPerformanceMonitoring(interval time.Duration) {
	logging.Logger.Info().Msg("Starting performance monitoring")
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logging.Logger.Error().Interface("panic", r).Msg("Performance monitoring goroutine panicked")
			}
			logging.Logger.Info().Msg("Performance monitoring goroutine exited")
		}()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		logging.Logger.Info().Msg("Performance monitoring goroutine started")

		for {
			select {
			case <-ticker.C:
				logging.Logger.Debug().Msg("Ticker fired, updating system stats")
				func() {
					defer func() {
						if r := recover(); r != nil {
							logging.Logger.Error().Interface("panic", r).Msg("Panic while updating system stats")
						}
					}()
					e.statsMutex.Lock()
					e.updateSystemStats()
					e.statsMutex.Unlock()
				}()
				logging.Logger.Debug().Msg("System stats update completed")
			case <-e.stopMonitoring:
				logging.Logger.Info().Msg("Received stop signal, stopping performance monitoring")
				return
			}
		}
	}()
}

func (e *Engine) StopPerformanceMonitoring() {
	if e.enablePerformanceMonitoring {
		close(e.stopMonitoring)
	}
}

func (e *Engine) Shutdown() {
	logging.Logger.Info().Msg("Initiating engine shutdown")
	if e.enablePerformanceMonitoring {
		e.StopPerformanceMonitoring()
	}
	// Add any other cleanup operations here
	logging.Logger.Info().Msg("Engine shutdown complete")
}
