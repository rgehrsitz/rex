// rex/pkg/runtime/engine_test.go

package runtime

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rgehrsitz/rex/pkg/compiler"
	"rgehrsitz/rex/pkg/logging"
	"rgehrsitz/rex/pkg/store"
)

func setupMiniredis(t *testing.T) (*miniredis.Miniredis, *store.RedisStore) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}

	redisStore, err := store.NewRedisStore(context.Background(), store.RedisOptions{Addr: s.Addr()})
	require.NoError(t, err)
	return s, redisStore
}

func mustGenerateBytecode(t testing.TB, ruleset *compiler.Ruleset) compiler.BytecodeFile {
	t.Helper()
	bytecode, err := compiler.GenerateBytecode(ruleset)
	require.NoError(t, err)
	return bytecode
}

func createTestBytecodeFile(t *testing.T, ruleset *compiler.Ruleset) string {
	bytecode := mustGenerateBytecode(t, ruleset)
	filename := "test_bytecode.bin"
	err := compiler.WriteBytecodeToFile(filename, bytecode)
	assert.NoError(t, err)
	return filename
}

func validBytecode(t *testing.T) []byte {
	t.Helper()

	ruleset := &compiler.Ruleset{Rules: []compiler.Rule{{
		Name: "temperature_rule",
		Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{
			Fact:     "temperature",
			Operator: "GT",
			Value:    30.0,
		}}},
		Actions: []compiler.Action{{Type: "updateStore", Target: "status", Value: "hot"}},
	}}}
	filename := t.TempDir() + "/valid.bytecode"
	require.NoError(t, compiler.WriteBytecodeToFile(filename, mustGenerateBytecode(t, ruleset)))

	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	return data
}

func validTwoRuleBytecode(t *testing.T) []byte {
	t.Helper()

	ruleset := &compiler.Ruleset{Rules: []compiler.Rule{
		{
			Name: "temperature_rule",
			Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{
				Fact:     "temperature",
				Operator: "GT",
				Value:    30.0,
			}}},
			Actions: []compiler.Action{{Type: "updateStore", Target: "status", Value: "hot"}},
		},
		{
			Name: "humidity_rule",
			Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{
				Fact:     "humidity",
				Operator: "GT",
				Value:    80.0,
			}}},
			Actions: []compiler.Action{{Type: "updateStore", Target: "status", Value: "humid"}},
		},
	}}
	filename := t.TempDir() + "/valid-two-rules.bytecode"
	require.NoError(t, compiler.WriteBytecodeToFile(filename, mustGenerateBytecode(t, ruleset)))

	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	return data
}

func firstConditionalJumpOffset(data []byte) int {
	instructionsEnd := int(binary.LittleEndian.Uint32(data[16:20]))
	instructions := data[compiler.HeaderSize:instructionsEnd]
	jumpOffset := bytes.Index(instructions, []byte{byte(compiler.GT_FLOAT), byte(compiler.JUMP_IF_FALSE)})
	if jumpOffset == -1 {
		panic("validBytecode fixture must contain a conditional jump")
	}
	return compiler.HeaderSize + jumpOffset + 1
}

func secondRuleLabelResumeOffset(data []byte) int {
	instructionsEnd := int(binary.LittleEndian.Uint32(data[16:20]))
	instructions := data[compiler.HeaderSize:instructionsEnd]
	secondRulePrefix := append([]byte{byte(compiler.RULE_START), byte(len("humidity_rule"))}, []byte("humidity_rule")...)
	secondRule := bytes.Index(instructions, secondRulePrefix)
	if secondRule == -1 {
		panic("fixture must contain the second rule")
	}
	label := bytes.Index(instructions[secondRule:], []byte{byte(compiler.LABEL), 'L'})
	if label == -1 {
		panic("fixture's second rule must contain a label")
	}
	return compiler.HeaderSize + secondRule + label + 5
}

type eventConsumerProbeStore struct {
	receiveCalls chan struct{}
}

type contextCaptureStore struct {
	setAndPublishContext context.Context
	getContext           context.Context
	setAndPublishErr     error
	mGetErr              error
	mGetValues           map[string]interface{}
	mGetKeys             []string
	publishCount         int
}

type recordingExecutionObserver struct {
	rulesFired       []string
	actionsSucceeded []string
	actionsSkipped   []string
	actionFailures   []string
}

func (o *recordingExecutionObserver) RuleFired(ruleName string) {
	o.rulesFired = append(o.rulesFired, ruleName)
}

func (o *recordingExecutionObserver) ActionSucceeded(actionType string) {
	o.actionsSucceeded = append(o.actionsSucceeded, actionType)
}

func (o *recordingExecutionObserver) ActionSkipped(actionType string) {
	o.actionsSkipped = append(o.actionsSkipped, actionType)
}

func (o *recordingExecutionObserver) ActionFailed(actionType string, _ error) {
	o.actionFailures = append(o.actionFailures, actionType)
}

func captureStructuredLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var output bytes.Buffer
	originalLogger := logging.Logger
	logging.Logger = zerolog.New(&output)
	t.Cleanup(func() { logging.Logger = originalLogger })
	return &output
}

func structuredLogEvents(t *testing.T, output *bytes.Buffer) []map[string]interface{} {
	t.Helper()

	var events []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		if line == "" {
			continue
		}

		var event map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		events = append(events, event)
	}
	return events
}

func findStructuredEvent(events []map[string]interface{}, name string) map[string]interface{} {
	for _, event := range events {
		if event["event"] == name {
			return event
		}
	}
	return nil
}

func (s *contextCaptureStore) Close() error { return nil }

func (s *contextCaptureStore) SetFactContext(context.Context, string, interface{}) error { return nil }

func (s *contextCaptureStore) SetAndPublishFactContext(ctx context.Context, _ string, _ interface{}) error {
	s.setAndPublishContext = ctx
	s.publishCount++
	return s.setAndPublishErr
}

func (s *contextCaptureStore) GetFactContext(ctx context.Context, _ string) (interface{}, error) {
	s.getContext = ctx
	return nil, nil
}

func (s *contextCaptureStore) MGetFactsContext(_ context.Context, keys ...string) (map[string]interface{}, error) {
	s.mGetKeys = append([]string(nil), keys...)
	if s.mGetValues == nil {
		return nil, s.mGetErr
	}

	values := make(map[string]interface{}, len(keys))
	for _, key := range keys {
		values[key] = s.mGetValues[key]
	}
	return values, s.mGetErr
}

func (s *eventConsumerProbeStore) Close() error { return nil }

func (s *eventConsumerProbeStore) SetFactContext(context.Context, string, interface{}) error {
	return nil
}

func (s *eventConsumerProbeStore) SetAndPublishFactContext(context.Context, string, interface{}) error {
	return nil
}

func (s *eventConsumerProbeStore) GetFactContext(context.Context, string) (interface{}, error) {
	return nil, nil
}

func (s *eventConsumerProbeStore) MGetFactsContext(context.Context, ...string) (map[string]interface{}, error) {
	return nil, nil
}

func (s *eventConsumerProbeStore) SetFact(string, interface{}) error { return nil }

func (s *eventConsumerProbeStore) SetAndPublishFact(string, interface{}) error { return nil }

func (s *eventConsumerProbeStore) GetFact(string) (interface{}, error) { return nil, nil }

func (s *eventConsumerProbeStore) MGetFacts(...string) (map[string]interface{}, error) {
	return nil, nil
}

// ReceiveFacts remains on this test double solely to detect an accidental
// reintroduction of engine-owned event consumption.
func (s *eventConsumerProbeStore) ReceiveFacts() <-chan *redis.Message {
	s.receiveCalls <- struct{}{}
	return make(chan *redis.Message)
}

func TestNewEngineFromFileDoesNotConsumeEvents(t *testing.T) {
	ruleset := &compiler.Ruleset{Rules: []compiler.Rule{{Name: "rule"}}}
	filename := createTestBytecodeFile(t, ruleset)
	defer os.Remove(filename)

	store := &eventConsumerProbeStore{receiveCalls: make(chan struct{}, 1)}
	engine, err := NewEngineFromFile(filename, store, 0)
	require.NoError(t, err)
	require.NotNil(t, engine)

	select {
	case <-store.receiveCalls:
		t.Fatal("engine must not own event consumption")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNewEngineFromFileRejectsRetiredScriptOpcodes(t *testing.T) {
	for _, opcode := range []compiler.Opcode{compiler.SCRIPT_DEF, compiler.SCRIPT_CALL} {
		t.Run(opcode.String(), func(t *testing.T) {
			artifact := mustGenerateBytecode(t, &compiler.Ruleset{Rules: []compiler.Rule{{Name: "rule"}}})
			end := bytes.LastIndexByte(artifact.Instructions, byte(compiler.RULE_END))
			require.GreaterOrEqual(t, end, 0)
			artifact.Instructions = append(artifact.Instructions, 0)
			copy(artifact.Instructions[end+1:], artifact.Instructions[end:])
			artifact.Instructions[end] = byte(opcode)

			filename := t.TempDir() + "/legacy-script.bytecode"
			require.NoError(t, compiler.WriteBytecodeToFile(filename, artifact))
			factStore := &contextCaptureStore{}
			engine, err := NewEngineFromFile(filename, factStore, 0)
			assert.Nil(t, engine)
			assert.ErrorContains(t, err, "scripts are no longer supported")
			assert.ErrorContains(t, err, opcode.String())
			assert.Zero(t, factStore.publishCount)
		})
	}
}

func TestSetScriptsEnabledIsMigrationTripwire(t *testing.T) {
	engine := &Engine{}
	require.NoError(t, engine.SetScriptsEnabled(false))
	assert.ErrorContains(t, engine.SetScriptsEnabled(true), "scripts are no longer supported")
}

func TestNewEngineFromFileRejectsInvalidBytecode(t *testing.T) {
	twoRuleData := validTwoRuleBytecode(t)
	tests := []struct {
		name                string
		mutate              func([]byte) []byte
		recalculateChecksum bool
		wantError           string
	}{
		{
			name: "truncated header",
			mutate: func(data []byte) []byte {
				return data[:compiler.HeaderSize-1]
			},
		},
		{
			name: "previous version",
			mutate: func(data []byte) []byte {
				binary.LittleEndian.PutUint32(data[0:4], compiler.Version-1)
				return data
			},
			recalculateChecksum: true,
			wantError:           "unsupported bytecode version",
		},
		{
			name: "unsupported version",
			mutate: func(data []byte) []byte {
				binary.LittleEndian.PutUint32(data[0:4], compiler.ChangeOnlyVersion+1)
				return data
			},
			recalculateChecksum: true,
			wantError:           "unsupported bytecode version",
		},
		{
			name: "out of bounds rule index offset",
			mutate: func(data []byte) []byte {
				binary.LittleEndian.PutUint32(data[16:20], uint32(len(data)+1))
				return data
			},
			recalculateChecksum: true,
			wantError:           "invalid bytecode section offsets",
		},
		{
			name: "truncated rule index string",
			mutate: func(data []byte) []byte {
				offset := binary.LittleEndian.Uint32(data[16:20])
				binary.LittleEndian.PutUint32(data[offset:offset+4], ^uint32(0))
				return data
			},
			recalculateChecksum: true,
		},
		{
			name: "rule index priority disagrees with instructions",
			mutate: func(data []byte) []byte {
				offset := int(binary.LittleEndian.Uint32(data[16:20]))
				nameLength := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
				priorityOffset := offset + 4 + nameLength + 4
				priority := binary.LittleEndian.Uint32(data[priorityOffset : priorityOffset+4])
				binary.LittleEndian.PutUint32(data[priorityOffset:priorityOffset+4], priority+1)
				return data
			},
			recalculateChecksum: true,
			wantError:           "does not match instruction priority",
		},
		{
			name: "unknown instruction opcode",
			mutate: func(data []byte) []byte {
				data[compiler.HeaderSize] = 0xff
				return data
			},
			recalculateChecksum: true,
		},
		{
			name: "missing rule priority instruction",
			mutate: func(data []byte) []byte {
				ruleStart := compiler.HeaderSize
				nameLength := int(data[ruleStart+1])
				priorityOffset := ruleStart + 2 + nameLength
				data[priorityOffset] = byte(compiler.ACTION_START)
				return data
			},
			recalculateChecksum: true,
			wantError:           "missing its priority instruction",
		},
		{
			name: "zero offset conditional jump",
			mutate: func(data []byte) []byte {
				jumpOffset := firstConditionalJumpOffset(data)
				binary.LittleEndian.PutUint32(data[jumpOffset+1:jumpOffset+5], 0)
				return data
			},
			recalculateChecksum: true,
			wantError:           "without a preceding label",
		},
		{
			name: "conditional jump between instructions",
			mutate: func(data []byte) []byte {
				jumpOffset := firstConditionalJumpOffset(data)
				binary.LittleEndian.PutUint32(data[jumpOffset+1:jumpOffset+5], 2)
				return data
			},
			recalculateChecksum: true,
			wantError:           "targets non-instruction-section offset",
		},
		{
			name: "conditional jump beyond instruction section",
			mutate: func(data []byte) []byte {
				jumpOffset := firstConditionalJumpOffset(data)
				binary.LittleEndian.PutUint32(data[jumpOffset+1:jumpOffset+5], ^uint32(0))
				return data
			},
			recalculateChecksum: true,
			wantError:           "targets out-of-range instruction-section offset",
		},
		{
			name: "conditional jump into a following rule",
			mutate: func(_ []byte) []byte {
				data := append([]byte(nil), twoRuleData...)
				jumpOffset := firstConditionalJumpOffset(data)
				targetOffset := secondRuleLabelResumeOffset(data)
				binary.LittleEndian.PutUint32(data[jumpOffset+1:jumpOffset+5], uint32(targetOffset-(jumpOffset+5)))
				return data
			},
			recalculateChecksum: true,
			wantError:           "crosses rule boundary",
		},
		{
			name: "declared rule count mismatch",
			mutate: func(data []byte) []byte {
				binary.LittleEndian.PutUint32(data[12:16], 2)
				return data
			},
			recalculateChecksum: true,
		},
		{
			name: "checksum mismatch",
			mutate: func(data []byte) []byte {
				data[compiler.HeaderSize] ^= 0xff
				return data
			},
			wantError: "bytecode checksum mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.mutate(validBytecode(t))
			if tt.recalculateChecksum {
				binary.LittleEndian.PutUint32(data[compiler.ChecksumOffset:compiler.ChecksumOffset+compiler.ChecksumSize], compiler.CalculateBytecodeChecksum(data))
			}
			filename := t.TempDir() + "/invalid.bytecode"
			require.NoError(t, os.WriteFile(filename, data, 0o600))

			assert.NotPanics(t, func() {
				engine, err := NewEngineFromFile(filename, &contextCaptureStore{}, 0)
				assert.Nil(t, engine)
				assert.Error(t, err)
				if tt.wantError != "" {
					assert.ErrorContains(t, err, tt.wantError)
				}
			})
		})
	}
}

func TestExecuteActionContextPassesContextToStore(t *testing.T) {
	store := &contextCaptureStore{}
	engine := &Engine{facts: make(map[string]interface{}), store: store}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := engine.executeActionContext(ctx, compiler.Action{
		Type:   "updateStore",
		Target: "status",
		Value:  "ready",
	})
	require.NoError(t, err)
	assert.Same(t, ctx, store.setAndPublishContext)
	assert.Nil(t, store.getContext, "successful actions must not perform a diagnostic read")
}

func TestProcessFactUpdateContextHonorsCancellation(t *testing.T) {
	engine := &Engine{facts: make(map[string]interface{}), store: &contextCaptureStore{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := engine.ProcessFactUpdateContext(ctx, "temperature", 35.0)
	assert.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, engine.facts, "temperature")
}

func TestProcessFactUpdateContextReturnsStoreError(t *testing.T) {
	storeErr := errors.New("store unavailable")
	engine := &Engine{
		facts:               make(map[string]interface{}),
		store:               &contextCaptureStore{mGetErr: storeErr},
		factRuleIndex:       map[string][]string{"temperature": {"temperature_rule"}},
		factDependencyIndex: map[string][]string{"temperature_rule": {"temperature", "humidity"}},
	}

	err := engine.ProcessFactUpdateContext(context.Background(), "temperature", 35.0)
	assert.ErrorIs(t, err, storeErr)
}

func TestProcessFactUpdateContextDoesNotMutateCandidateIndexWhenDependencyIsMissing(t *testing.T) {
	factStore := &contextCaptureStore{mGetValues: map[string]interface{}{"humidity": nil}}
	ruleset := &compiler.Ruleset{Rules: []compiler.Rule{
		{
			Name: "requires_humidity",
			Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{
				{Fact: "temperature", Operator: "GT", Value: 30.0},
				{Fact: "humidity", Operator: "GT", Value: 50.0},
			}},
			Actions: []compiler.Action{{Type: "updateStore", Target: "humid_alert", Value: true}},
		},
		{
			Name: "temperature_alert",
			Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{
				{Fact: "temperature", Operator: "GT", Value: 30.0},
			}},
			Actions: []compiler.Action{{Type: "updateStore", Target: "temperature_alert", Value: true}},
		},
		{
			Name: "temperature_status",
			Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{
				{Fact: "temperature", Operator: "GT", Value: 30.0},
			}},
			Actions: []compiler.Action{{Type: "updateStore", Target: "temperature_status", Value: "hot"}},
		},
	}}

	filename := t.TempDir() + "/rules.bytecode"
	require.NoError(t, compiler.WriteBytecodeToFile(filename, mustGenerateBytecode(t, ruleset)))
	engine, err := NewEngineFromFile(filename, factStore, 0)
	require.NoError(t, err)
	observer := &recordingExecutionObserver{}
	engine.SetExecutionObserver(observer)

	wantCandidates := []string{"requires_humidity", "temperature_alert", "temperature_status"}
	require.Equal(t, wantCandidates, engine.factRuleIndex["temperature"])

	require.NoError(t, engine.ProcessFactUpdateContext(context.Background(), "temperature", 35.0))
	assert.Equal(t, []string{"humidity"}, factStore.mGetKeys)
	assert.Equal(t, wantCandidates, engine.factRuleIndex["temperature"])
	assert.Equal(t, []string{"temperature_alert", "temperature_status"}, observer.rulesFired)

	factStore.mGetValues["humidity"] = 75.0
	observer.rulesFired = nil
	require.NoError(t, engine.ProcessFactUpdateContext(context.Background(), "temperature", 35.0))
	assert.Equal(t, wantCandidates, engine.factRuleIndex["temperature"])
	assert.Equal(t, wantCandidates, observer.rulesFired)
}

func TestProcessFactUpdateContextOrdersRulesByPriorityThenSourceOrder(t *testing.T) {
	ruleset := &compiler.Ruleset{Rules: []compiler.Rule{
		{Name: "priority_ten", Priority: 10},
		{Name: "priority_one_first", Priority: 1},
		{Name: "priority_five", Priority: 5},
		{Name: "priority_one_second", Priority: 1},
	}}
	for i := range ruleset.Rules {
		ruleset.Rules[i].Conditions = compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{
			Fact: "temperature", Operator: "GT", Value: 30.0,
		}}}
		ruleset.Rules[i].Actions = []compiler.Action{{
			Type: "updateStore", Target: ruleset.Rules[i].Name, Value: true,
		}}
	}

	filename := t.TempDir() + "/priority.bytecode"
	require.NoError(t, compiler.WriteBytecodeToFile(filename, mustGenerateBytecode(t, ruleset)))
	engine, err := NewEngineFromFile(filename, &contextCaptureStore{}, 0)
	require.NoError(t, err)
	observer := &recordingExecutionObserver{}
	engine.SetExecutionObserver(observer)

	wantOrder := []string{"priority_one_first", "priority_one_second", "priority_five", "priority_ten"}
	require.Len(t, engine.ruleExecutionIndex, 4)
	assert.Equal(t, []int{10, 1, 5, 1}, []int{
		engine.ruleExecutionIndex["priority_ten"].Priority,
		engine.ruleExecutionIndex["priority_one_first"].Priority,
		engine.ruleExecutionIndex["priority_five"].Priority,
		engine.ruleExecutionIndex["priority_one_second"].Priority,
	})
	assert.Equal(t, wantOrder, engine.factRuleIndex["temperature"])
	require.NoError(t, engine.ProcessFactUpdateContext(context.Background(), "temperature", 35.0))
	assert.Equal(t, wantOrder, observer.rulesFired)
}

func TestProcessFactUpdateContextEmitsCorrelatedRuleAndActionTrace(t *testing.T) {
	output := captureStructuredLogs(t)
	factStore := &contextCaptureStore{}
	ruleset := &compiler.Ruleset{Rules: []compiler.Rule{{
		Name: "temperature_rule",
		Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{
			Fact:     "temperature",
			Operator: "GT",
			Value:    30.0,
		}}},
		Actions: []compiler.Action{{Type: "updateStore", Target: "status", Value: "hot"}},
	}}}

	filename := t.TempDir() + "/rules.bytecode"
	require.NoError(t, compiler.WriteBytecodeToFile(filename, mustGenerateBytecode(t, ruleset)))
	engine, err := NewEngineFromFile(filename, factStore, 0)
	require.NoError(t, err)

	ctx := WithTraceID(context.Background(), "trace-temperature-1")
	require.NoError(t, engine.ProcessFactUpdateContext(ctx, "temperature", 35.0))

	events := structuredLogEvents(t, output)
	candidates := findStructuredEvent(events, "rule_evaluation_candidates")
	require.NotNil(t, candidates)
	assert.Equal(t, "trace-temperature-1", candidates["trace_id"])
	assert.Equal(t, "temperature", candidates["fact_name"])
	assert.Equal(t, []interface{}{"temperature_rule"}, candidates["rule_names"])

	condition := findStructuredEvent(events, "rule_condition_evaluated")
	require.NotNil(t, condition)
	assert.Equal(t, "trace-temperature-1", condition["trace_id"])
	assert.Equal(t, "temperature_rule", condition["rule_name"])
	assert.Equal(t, true, condition["matched"])

	action := findStructuredEvent(events, "action_completed")
	require.NotNil(t, action)
	assert.Equal(t, "trace-temperature-1", action["trace_id"])
	assert.Equal(t, "updateStore", action["action_type"])
	assert.Equal(t, "status", action["action_target"])

	result := findStructuredEvent(events, "rule_evaluation_completed")
	require.NotNil(t, result)
	assert.Equal(t, "trace-temperature-1", result["trace_id"])
	assert.Equal(t, true, result["matched"])
	assert.Equal(t, float64(1), result["actions_attempted"])
}

func TestProcessFactUpdateContextTraceRecordsNonMatchingRule(t *testing.T) {
	output := captureStructuredLogs(t)
	factStore := &contextCaptureStore{}
	ruleset := &compiler.Ruleset{Rules: []compiler.Rule{{
		Name: "temperature_rule",
		Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{
			Fact:     "temperature",
			Operator: "GT",
			Value:    30.0,
		}}},
		Actions: []compiler.Action{{Type: "updateStore", Target: "status", Value: "hot"}},
	}}}

	filename := t.TempDir() + "/rules.bytecode"
	require.NoError(t, compiler.WriteBytecodeToFile(filename, mustGenerateBytecode(t, ruleset)))
	engine, err := NewEngineFromFile(filename, factStore, 0)
	require.NoError(t, err)

	require.NoError(t, engine.ProcessFactUpdateContext(WithTraceID(context.Background(), "trace-temperature-2"), "temperature", 20.0))

	events := structuredLogEvents(t, output)
	condition := findStructuredEvent(events, "rule_condition_evaluated")
	require.NotNil(t, condition)
	assert.Equal(t, "trace-temperature-2", condition["trace_id"])
	assert.Equal(t, false, condition["matched"])
	assert.Nil(t, findStructuredEvent(events, "action_completed"))

	result := findStructuredEvent(events, "rule_evaluation_completed")
	require.NotNil(t, result)
	assert.Equal(t, false, result["matched"])
	assert.Equal(t, float64(0), result["actions_attempted"])
}

func TestExecuteActionContextTraceRecordsFailure(t *testing.T) {
	output := captureStructuredLogs(t)
	storeErr := errors.New("redis unavailable")
	engine := &Engine{
		facts: make(map[string]interface{}),
		store: &contextCaptureStore{setAndPublishErr: storeErr},
	}

	err := engine.executeActionContext(WithTraceID(context.Background(), "trace-action-failure"), compiler.Action{
		Type:   "updateStore",
		Target: "status",
		Value:  "hot",
	})
	require.ErrorIs(t, err, storeErr)

	action := findStructuredEvent(structuredLogEvents(t, output), "action_failed")
	require.NotNil(t, action)
	assert.Equal(t, "trace-action-failure", action["trace_id"])
	assert.Equal(t, "updateStore", action["action_type"])
	assert.Equal(t, "status", action["action_target"])
}

func TestExecutionObserverReceivesRuleAndActionOutcomes(t *testing.T) {
	factStore := &contextCaptureStore{}
	ruleset := &compiler.Ruleset{Rules: []compiler.Rule{{
		Name: "temperature_rule",
		Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{
			Fact:     "temperature",
			Operator: "GT",
			Value:    30.0,
		}}},
		Actions: []compiler.Action{{Type: "updateStore", Target: "status", Value: "hot"}},
	}}}

	filename := t.TempDir() + "/rules.bytecode"
	require.NoError(t, compiler.WriteBytecodeToFile(filename, mustGenerateBytecode(t, ruleset)))
	engine, err := NewEngineFromFile(filename, factStore, 0)
	require.NoError(t, err)
	observer := &recordingExecutionObserver{}
	engine.SetExecutionObserver(observer)

	require.NoError(t, engine.ProcessFactUpdateContext(context.Background(), "temperature", 35.0))
	assert.Equal(t, []string{"temperature_rule"}, observer.rulesFired)
	assert.Equal(t, []string{"updateStore"}, observer.actionsSucceeded)
	assert.Empty(t, observer.actionsSkipped)
	assert.Empty(t, observer.actionFailures)

	factStore.setAndPublishErr = errors.New("redis unavailable")
	require.Error(t, engine.executeActionContext(context.Background(), compiler.Action{Type: "updateStore", Target: "status", Value: "hot"}))
	assert.Equal(t, []string{"updateStore"}, observer.actionFailures)
}

func TestProcessFactUpdateContextEnforcesActionLimit(t *testing.T) {
	factStore := &contextCaptureStore{}
	ruleset := &compiler.Ruleset{Rules: []compiler.Rule{{
		Name: "temperature_rule",
		Conditions: compiler.ConditionGroup{All: []*compiler.ConditionOrGroup{{
			Fact:     "temperature",
			Operator: "GT",
			Value:    30.0,
		}}},
		Actions: []compiler.Action{
			{Type: "updateStore", Target: "first_status", Value: "hot"},
			{Type: "updateStore", Target: "second_status", Value: "hot"},
		},
	}}}

	filename := t.TempDir() + "/rules.bytecode"
	require.NoError(t, compiler.WriteBytecodeToFile(filename, mustGenerateBytecode(t, ruleset)))
	engine, err := NewEngineFromFile(filename, factStore, 0)
	require.NoError(t, err)
	assert.Equal(t, DefaultMaxActionsPerEvaluation, engine.maxActionsPerEvaluation)
	engine.SetMaxActionsPerEvaluation(1)

	err = engine.ProcessFactUpdateContext(context.Background(), "temperature", 35.0)
	require.ErrorContains(t, err, "exceeded action limit of 1")
	assert.Equal(t, 1, factStore.publishCount)
	assert.Equal(t, "hot", engine.facts["first_status"])
	assert.NotContains(t, engine.facts, "second_status")
}

func TestProcessFactUpdate(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	ruleset := &compiler.Ruleset{
		Rules: []compiler.Rule{
			{
				Name: "UpdateRule",
				Conditions: compiler.ConditionGroup{
					All: []*compiler.ConditionOrGroup{
						{
							Fact:     "temperature",
							Operator: "GT",
							Value:    30.0,
						},
					},
				},
				Actions: []compiler.Action{
					{
						Type:   "updateStore",
						Target: "alert",
						Value:  true,
					},
				},
			},
		},
	}

	filename := createTestBytecodeFile(t, ruleset)
	defer os.Remove(filename)

	engine, err := NewEngineFromFile(filename, redisStore, 0)
	assert.NoError(t, err)

	engine.ProcessFactUpdate("temperature", 35.0)

	// Verify the fact was updated in miniredis
	alertValue, err := s.Get("alert")
	assert.NoError(t, err)
	assert.Equal(t, "true", alertValue)
}

func TestMultipleRules(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	ruleset := &compiler.Ruleset{
		Rules: []compiler.Rule{
			{
				Name: "Rule1",
				Conditions: compiler.ConditionGroup{
					All: []*compiler.ConditionOrGroup{
						{
							Fact:     "temperature",
							Operator: "GT",
							Value:    30.0,
						},
					},
				},
				Actions: []compiler.Action{
					{
						Type:   "updateStore",
						Target: "alert",
						Value:  true,
					},
				},
			},
			{
				Name: "Rule2",
				Conditions: compiler.ConditionGroup{
					All: []*compiler.ConditionOrGroup{
						{
							Fact:     "humidity",
							Operator: "LT",
							Value:    40.0,
						},
					},
				},
				Actions: []compiler.Action{
					{
						Type:   "updateStore",
						Target: "humidifier",
						Value:  true,
					},
				},
			},
		},
	}

	filename := createTestBytecodeFile(t, ruleset)
	defer os.Remove(filename)

	engine, err := NewEngineFromFile(filename, redisStore, 0)
	assert.NoError(t, err)

	engine.ProcessFactUpdate("temperature", 35.0)
	alertValue, err := s.Get("alert")
	assert.NoError(t, err)
	assert.Equal(t, "true", alertValue)

	engine.ProcessFactUpdate("humidity", 35.0)
	humidifierValue, err := s.Get("humidifier")
	assert.NoError(t, err)
	assert.Equal(t, "true", humidifierValue)
}

// Add more tests here...

func TestCompare(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name       string
		factValue  interface{}
		constValue interface{}
		opcode     compiler.Opcode
		expected   bool
	}{
		{"EQ_FLOAT True", 5.0, 5.0, compiler.EQ_FLOAT, true},
		{"EQ_FLOAT False", 5.0, 6.0, compiler.EQ_FLOAT, false},
		{"NEQ_FLOAT True", 5.0, 6.0, compiler.NEQ_FLOAT, true},
		{"NEQ_FLOAT False", 5.0, 5.0, compiler.NEQ_FLOAT, false},
		{"LT_FLOAT True", 5.0, 6.0, compiler.LT_FLOAT, true},
		{"LT_FLOAT False", 6.0, 5.0, compiler.LT_FLOAT, false},
		{"LTE_FLOAT True", 5.0, 5.0, compiler.LTE_FLOAT, true},
		{"LTE_FLOAT False", 6.0, 5.0, compiler.LTE_FLOAT, false},
		{"GT_FLOAT True", 6.0, 5.0, compiler.GT_FLOAT, true},
		{"GT_FLOAT False", 5.0, 6.0, compiler.GT_FLOAT, false},
		{"GTE_FLOAT True", 5.0, 5.0, compiler.GTE_FLOAT, true},
		{"GTE_FLOAT False", 5.0, 6.0, compiler.GTE_FLOAT, false},
		{"EQ_STRING True", "test", "test", compiler.EQ_STRING, true},
		{"EQ_STRING False", "test", "Test", compiler.EQ_STRING, false},
		{"NEQ_STRING True", "test", "Test", compiler.NEQ_STRING, true},
		{"NEQ_STRING False", "test", "test", compiler.NEQ_STRING, false},
		{"CONTAINS_STRING True", "teststring", "test", compiler.CONTAINS_STRING, true},
		{"CONTAINS_STRING False", "teststring", "TEST", compiler.CONTAINS_STRING, false},
		{"NOT_CONTAINS_STRING True", "teststring", "TEST", compiler.NOT_CONTAINS_STRING, true},
		{"NOT_CONTAINS_STRING False", "teststring", "test", compiler.NOT_CONTAINS_STRING, false},
		{"EQ_BOOL True", true, true, compiler.EQ_BOOL, true},
		{"EQ_BOOL False", true, false, compiler.EQ_BOOL, false},
		{"NEQ_BOOL True", true, false, compiler.NEQ_BOOL, true},
		{"NEQ_BOOL False", true, true, compiler.NEQ_BOOL, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.compare(tt.factValue, tt.constValue, tt.opcode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCompareRejectsInvalidTypesWithoutPanicking(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name       string
		factValue  interface{}
		constValue interface{}
		opcode     compiler.Opcode
	}{
		{"float fact is string", "5", 5.0, compiler.EQ_FLOAT},
		{"float constant is string", 5.0, "5", compiler.NEQ_FLOAT},
		{"string fact is bool", true, "true", compiler.CONTAINS_STRING},
		{"boolean constant is string", true, "true", compiler.EQ_BOOL},
		{"nil value", nil, 5.0, compiler.GT_FLOAT},
		{"unknown opcode", "value", "value", compiler.Opcode(255)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				assert.False(t, engine.compare(tt.factValue, tt.constValue, tt.opcode))
			})
		})
	}
}

func TestProcessFactUpdateSkipsActionForMalformedComparison(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	engine := createTestEngine(t, redisStore, `{
        "rules": [{
            "name": "temperature_rule",
            "conditions": {
                "all": [{
                    "fact": "temperature",
                    "operator": "GT",
                    "value": 30
                }]
            },
            "actions": [{
                "type": "updateStore",
                "target": "status",
                "value": "hot"
            }]
        }]
    }`)

	assert.NotPanics(t, func() {
		engine.ProcessFactUpdate("temperature", "not-a-number")
	})
	assert.False(t, s.Exists("status"))
}

func TestProcessFactUpdateSimpleRule(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	engine := createTestEngine(t, redisStore, `{
        "rules": [{
            "name": "simple_rule",
            "conditions": {
                "all": [{
                    "fact": "temperature",
                    "operator": "GT",
                    "value": 30
                }]
            },
            "actions": [{
                "type": "updateStore",
                "target": "status",
                "value": "hot"
            }]
        }]
    }`)

	engine.ProcessFactUpdate("temperature", 35)

	status, err := redisStore.GetFact("status")
	assert.NoError(t, err)
	assert.Equal(t, "hot", status)
}

func TestProcessFactUpdateComplexRule(t *testing.T) {
	s, redisStore := setupMiniredis(t)
	defer s.Close()

	engine := createTestEngine(t, redisStore, `{
        "rules": [{
            "name": "complex_rule",
            "conditions": {
                "all": [
                    {
                        "fact": "temperature",
                        "operator": "GT",
                        "value": 30
                    },
                    {
                        "any": [
                            {
                                "fact": "humidity",
                                "operator": "LT",
                                "value": 50
                            },
                            {
                                "fact": "pressure",
                                "operator": "GT",
                                "value": 1000
                            }
                        ]
                    }
                ]
            },
            "actions": [{
                "type": "updateStore",
                "target": "status",
                "value": "alert"
            }]
        }]
    }`)

	// Initialize Redis store with initial facts
	initialFacts := map[string]interface{}{
		"temperature": 25.0,
		"humidity":    49.0,
		"pressure":    900.0,
		"status":      "",
	}
	for k, v := range initialFacts {
		redisStore.SetFact(k, v)
	}

	// Test case 1: Should trigger the rule
	t.Log("Test case 1: Should trigger the rule")
	engine.ProcessFactUpdate("temperature", 35.0)

	status, err := redisStore.GetFact("status")
	assert.NoError(t, err)
	assert.Equal(t, "alert", status, "Rule should have been triggered")

	// Reset status in Redis
	redisStore.SetFact("status", "")
	redisStore.SetFact("temperature", 35)
	redisStore.SetFact("humidity", 60)
	redisStore.SetFact("pressure", 900)

	// Test case 2: Should trigger the rule with different conditions
	t.Log("Test case 2: Should trigger the rule with different conditions")
	engine.ProcessFactUpdate("humidity", 30.0)

	status, err = redisStore.GetFact("status")
	assert.NoError(t, err)
	assert.Equal(t, "alert", status, "Rule should have been triggered")

	// Test case 3: Should not trigger the rule
	t.Log("Test case 3: Should not trigger the rule")
	redisStore.SetFact("status", "")
	engine.ProcessFactUpdate("pressure", 900.0)

	status, err = redisStore.GetFact("status")
	assert.NoError(t, err)
	assert.Equal(t, "", status, "Rule should not have been triggered")
}

// Helper function to create a test engine
func createTestEngine(t *testing.T, redisStore *store.RedisStore, jsonRuleset string) *Engine {
	t.Helper()
	ruleset, err := compiler.Parse([]byte(jsonRuleset))
	require.NoError(t, err)
	bytecodeFile := mustGenerateBytecode(t, ruleset)

	filename := "test_bytecode.bin"
	require.NoError(t, compiler.WriteBytecodeToFile(filename, bytecodeFile))
	defer os.Remove(filename)

	engine, err := NewEngineFromFile(filename, redisStore, 0)
	require.NoError(t, err)

	// Synchronize engine's fact store with Redis store
	facts, _ := redisStore.MGetFacts("temperature", "humidity", "pressure", "status")
	for k, v := range facts {
		engine.facts[k] = v
	}

	return engine
}
