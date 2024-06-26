// rex/pkg/compiler/codegen_test.go

package compiler

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoveLabels(t *testing.T) {
	instructions := []Instruction{
		{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 1")},
		{Opcode: LABEL, Operands: []byte("L0")},
		{Opcode: ACTION_START},
	}

	expectedInstructions := []Instruction{
		{Opcode: JUMP_IF_FALSE, Operands: []byte("temperature GT 30 1")},
		{Opcode: ACTION_START},
	}

	finalInstructions := RemoveLabels(instructions)
	assert.Equal(t, expectedInstructions, finalInstructions)
}

func TestGenerateBytecodeComplexNestedConditions(t *testing.T) {
	ruleset := &Ruleset{
		Rules: []Rule{
			{
				Name: "ComplexNestedRule",
				Conditions: ConditionGroup{
					All: []*ConditionOrGroup{
						{
							Fact:     "windSpeed",
							Operator: "LT",
							Value:    15.5,
						},
						{
							Any: []*ConditionOrGroup{
								{
									Fact:     "temperature",
									Operator: "GT",
									Value:    30.0,
								},
								{
									All: []*ConditionOrGroup{
										{
											Fact:     "humidity",
											Operator: "LT",
											Value:    50,
										},
										{
											Fact:     "pressure",
											Operator: "GT",
											Value:    1000,
										},
									},
								},
							},
						},
					},
				},
				Actions: []Action{
					{
						Type:   "updateStore",
						Target: "alert",
						Value:  true,
					},
				},
			},
		},
	}

	bytecodeFile := GenerateBytecode(ruleset)

	// Helper function to read a string from bytecode
	readString := func(offset int) (string, int) {
		length := int(bytecodeFile.Instructions[offset])
		offset++
		str := string(bytecodeFile.Instructions[offset : offset+length])
		return str, offset + length
	}

	// Helper function to read a float64 from bytecode
	readFloat64 := func(offset int) (float64, int) {
		bits := binary.LittleEndian.Uint64(bytecodeFile.Instructions[offset : offset+8])
		return math.Float64frombits(bits), offset + 8
	}

	// Check header
	assert.Equal(t, uint32(1), bytecodeFile.Header.Version)
	assert.Equal(t, uint32(1), bytecodeFile.Header.NumRules)

	// Find RULE_START
	ruleStartIndex := bytes.Index(bytecodeFile.Instructions, []byte{byte(RULE_START)})
	assert.NotEqual(t, -1, ruleStartIndex, "RULE_START opcode not found")

	offset := ruleStartIndex + 1
	ruleName, offset := readString(offset)
	assert.Equal(t, "ComplexNestedRule", ruleName)

	// Check for windSpeed condition (should be first)
	windSpeedIndex := bytes.Index(bytecodeFile.Instructions[offset:], []byte{byte(LOAD_FACT_FLOAT)})
	assert.NotEqual(t, -1, windSpeedIndex, "LOAD_FACT_FLOAT for windSpeed not found")
	offset += windSpeedIndex + 1

	factName, offset := readString(offset)
	assert.Equal(t, "windSpeed", factName)

	constFloatIndex := bytes.Index(bytecodeFile.Instructions[offset:], []byte{byte(LOAD_CONST_FLOAT)})
	assert.NotEqual(t, -1, constFloatIndex, "LOAD_CONST_FLOAT for windSpeed value not found")
	offset += constFloatIndex + 1

	windSpeedValue, offset := readFloat64(offset)
	assert.InDelta(t, 15.5, windSpeedValue, 0.001, "Incorrect windSpeed value")

	ltIndex := bytes.Index(bytecodeFile.Instructions[offset:], []byte{byte(LT_FLOAT)})
	assert.NotEqual(t, -1, ltIndex, "LT_FLOAT opcode not found")
	offset += ltIndex + 1

	// Check for temperature condition
	tempIndex := bytes.Index(bytecodeFile.Instructions[offset:], []byte{byte(LOAD_FACT_FLOAT)})
	assert.NotEqual(t, -1, tempIndex, "LOAD_FACT_FLOAT for temperature not found")
	offset += tempIndex + 1

	factName, offset = readString(offset)
	assert.Equal(t, "temperature", factName)

	constFloatIndex = bytes.Index(bytecodeFile.Instructions[offset:], []byte{byte(LOAD_CONST_FLOAT)})
	assert.NotEqual(t, -1, constFloatIndex, "LOAD_CONST_FLOAT for temperature value not found")
	offset += constFloatIndex + 1

	tempValue, offset := readFloat64(offset)
	assert.InDelta(t, 30.0, tempValue, 0.001, "Incorrect temperature value")

	gtIndex := bytes.Index(bytecodeFile.Instructions[offset:], []byte{byte(GT_FLOAT)})
	assert.NotEqual(t, -1, gtIndex, "GT_FLOAT opcode not found")
	offset += gtIndex + 1

	// Check for humidity condition
	humidityIndex := bytes.Index(bytecodeFile.Instructions[offset:], []byte{byte(LOAD_FACT_FLOAT)})
	assert.NotEqual(t, -1, humidityIndex, "LOAD_FACT_FLOAT for humidity not found")
	offset += humidityIndex + 1

	factName, offset = readString(offset)
	assert.Equal(t, "humidity", factName)

	// Check for pressure condition
	pressureIndex := bytes.Index(bytecodeFile.Instructions[offset:], []byte{byte(LOAD_FACT_FLOAT)})
	assert.NotEqual(t, -1, pressureIndex, "LOAD_FACT_FLOAT for pressure not found")
	offset += pressureIndex + 1

	factName, offset = readString(offset)
	assert.Equal(t, "pressure", factName)

	// Check for action
	actionStartIndex := bytes.Index(bytecodeFile.Instructions[offset:], []byte{byte(ACTION_START)})
	assert.NotEqual(t, -1, actionStartIndex, "ACTION_START opcode not found")
	offset += actionStartIndex + 1

	actionTypeIndex := bytes.Index(bytecodeFile.Instructions[offset:], []byte{byte(ACTION_TYPE)})
	assert.NotEqual(t, -1, actionTypeIndex, "ACTION_TYPE opcode not found")
	offset += actionTypeIndex + 1

	actionType, offset := readString(offset)
	assert.Equal(t, "updateStore", actionType)

	actionTargetIndex := bytes.Index(bytecodeFile.Instructions[offset:], []byte{byte(ACTION_TARGET)})
	assert.NotEqual(t, -1, actionTargetIndex, "ACTION_TARGET opcode not found")
	offset += actionTargetIndex + 1

	actionTarget, offset := readString(offset)
	assert.Equal(t, "alert", actionTarget)

	actionValueIndex := bytes.Index(bytecodeFile.Instructions[offset:], []byte{byte(ACTION_VALUE_BOOL)})
	assert.NotEqual(t, -1, actionValueIndex, "ACTION_VALUE_BOOL opcode not found")
	offset += actionValueIndex + 1

	actionValue := bytecodeFile.Instructions[offset] == 1
	assert.True(t, actionValue, "Incorrect action value")

	// Check indices
	assert.Len(t, bytecodeFile.RuleExecIndex, 1, "Should have one rule execution index")
	assert.Equal(t, "ComplexNestedRule", bytecodeFile.RuleExecIndex[0].RuleName)

	assert.Len(t, bytecodeFile.FactRuleLookupIndex, 4, "Should have four facts in the fact-rule lookup index")
	assert.Contains(t, bytecodeFile.FactRuleLookupIndex, "windSpeed")
	assert.Contains(t, bytecodeFile.FactRuleLookupIndex, "temperature")
	assert.Contains(t, bytecodeFile.FactRuleLookupIndex, "humidity")
	assert.Contains(t, bytecodeFile.FactRuleLookupIndex, "pressure")

	assert.Len(t, bytecodeFile.FactDependencyIndex, 1, "Should have one fact dependency index")
	assert.Equal(t, "ComplexNestedRule", bytecodeFile.FactDependencyIndex[0].RuleName)
	assert.ElementsMatch(t, []string{"windSpeed", "temperature", "humidity", "pressure"}, bytecodeFile.FactDependencyIndex[0].Facts)
}

func TestGenerateBytecodeMultipleRules(t *testing.T) {
	ruleset := &Ruleset{
		Rules: []Rule{
			{
				Name: "Rule1",
				Conditions: ConditionGroup{
					All: []*ConditionOrGroup{
						{
							Fact:     "temperature",
							Operator: "GT",
							Value:    30.0,
						},
					},
				},
				Actions: []Action{
					{
						Type:   "updateFact",
						Target: "alert",
						Value:  true,
					},
				},
			},
			{
				Name: "Rule2",
				Conditions: ConditionGroup{
					Any: []*ConditionOrGroup{
						{
							Fact:     "humidity",
							Operator: "GT",
							Value:    80,
						},
						{
							Fact:     "pressure",
							Operator: "LT",
							Value:    900,
						},
					},
				},
				Actions: []Action{
					{
						Type:   "sendMessage",
						Target: "operator",
						Value:  "Check weather conditions",
					},
				},
			},
		},
	}

	bytecode := GenerateBytecode(ruleset)
	assert.NotEmpty(t, bytecode.Instructions)
	assert.Equal(t, uint32(2), bytecode.Header.NumRules)

	// Add more specific assertions to check the structure of the generated bytecode for multiple rules
}

func TestGenerateBytecodeDataTypes(t *testing.T) {
	ruleset := &Ruleset{
		Rules: []Rule{
			{
				Name: "DataTypeRule",
				Conditions: ConditionGroup{
					All: []*ConditionOrGroup{
						{
							Fact:     "temperature",
							Operator: "GT",
							Value:    30.5, // float
						},
						{
							Fact:     "status",
							Operator: "EQ",
							Value:    "active", // string
						},
						{
							Fact:     "isEmergency",
							Operator: "EQ",
							Value:    true, // bool
						},
					},
				},
				Actions: []Action{
					{
						Type:   "updateFact",
						Target: "alertLevel",
						Value:  2, // int
					},
					{
						Type:   "sendMessage",
						Target: "operator",
						Value:  "Emergency situation detected",
					},
				},
			},
		},
	}

	bytecode := GenerateBytecode(ruleset)
	assert.NotEmpty(t, bytecode.Instructions)

	// Add specific assertions to check if the bytecode correctly handles different data types
}

func TestGenerateBytecodeOperators(t *testing.T) {
	ruleset := &Ruleset{
		Rules: []Rule{
			{
				Name: "OperatorRule",
				Conditions: ConditionGroup{
					All: []*ConditionOrGroup{
						{
							Fact:     "string1",
							Operator: "CONTAINS",
							Value:    "test",
						},
						{
							Fact:     "string2",
							Operator: "NOT_CONTAINS",
							Value:    "error",
						},
						{
							Fact:     "number",
							Operator: "LTE",
							Value:    100,
						},
						{
							Fact:     "bool",
							Operator: "NEQ",
							Value:    false,
						},
					},
				},
				Actions: []Action{
					{
						Type:   "logEvent",
						Target: "system",
						Value:  "All conditions met",
					},
				},
			},
		},
	}

	bytecode := GenerateBytecode(ruleset)
	assert.NotEmpty(t, bytecode.Instructions)

	// Add specific assertions to check if the bytecode correctly handles different operators
}

func TestOptimizeInstructions(t *testing.T) {
	instructions := []Instruction{
		{Opcode: JUMP_IF_FALSE, Operands: []byte("temp GT 30 L001")},
		{Opcode: JUMP_IF_TRUE, Operands: []byte("L002")},
		{Opcode: LABEL, Operands: []byte("L001")},
		{Opcode: LOAD_CONST_BOOL, Operands: []byte{0}},
		{Opcode: JUMP, Operands: []byte("L003")},
		{Opcode: LABEL, Operands: []byte("L002")},
		{Opcode: LOAD_CONST_BOOL, Operands: []byte{1}},
		{Opcode: LABEL, Operands: []byte("L003")},
	}

	optimized := OptimizeInstructions(instructions)

	assert.Less(t, len(optimized), len(instructions), "Optimized instructions should be fewer")

	// Add more specific assertions to check if unnecessary jumps and labels are removed
}

func TestGenerateIndices(t *testing.T) {
	bytecode := []byte{
		byte(RULE_START), 5, 'R', 'u', 'l', 'e', '1',
		byte(LOAD_FACT_FLOAT), 4, 't', 'e', 'm', 'p',
		byte(LOAD_CONST_FLOAT), 0, 0, 0, 0, 0, 0, 0x41, 0xf0, // 30.0 in IEEE 754
		byte(GT_FLOAT),
		byte(JUMP_IF_FALSE), 'L', '0', '0', '1',
		byte(ACTION_START),
		byte(ACTION_TYPE), 10, 'u', 'p', 'd', 'a', 't', 'e', 'F', 'a', 'c', 't',
		byte(ACTION_TARGET), 5, 'a', 'l', 'e', 'r', 't',
		byte(ACTION_VALUE_BOOL), 1,
		byte(ACTION_END),
		byte(RULE_END),
	}

	ruleExecIndex, factRuleIndex, factDepIndex := GenerateIndices(bytecode)

	assert.Len(t, ruleExecIndex, 1, "Should have one rule execution index")
	assert.Equal(t, "Rule1", ruleExecIndex[0].RuleName)
	assert.Equal(t, 0, ruleExecIndex[0].ByteOffset)

	assert.Len(t, factRuleIndex, 1, "Should have one fact in the fact-rule lookup index")
	assert.Contains(t, factRuleIndex, "temp")
	assert.Equal(t, []string{"Rule1"}, factRuleIndex["temp"])

	assert.Len(t, factDepIndex, 1, "Should have one fact dependency index")
	assert.Equal(t, "Rule1", factDepIndex[0].RuleName)
	assert.Equal(t, []string{"temp"}, factDepIndex[0].Facts)
}

func TestFloatToBytes(t *testing.T) {
	testCases := []float64{0, 1, -1, 3.14159, -3.14159, math.MaxFloat64, math.SmallestNonzeroFloat64}

	for _, tc := range testCases {
		bytes := floatToBytes(tc)
		assert.Len(t, bytes, 8)

		// Convert bytes back to float64
		bits := binary.LittleEndian.Uint64(bytes)
		result := math.Float64frombits(bits)

		assert.Equal(t, tc, result)
	}
}

func TestBoolToBytes(t *testing.T) {
	assert.Equal(t, []byte{1}, boolToBytes(true))
	assert.Equal(t, []byte{0}, boolToBytes(false))
}
