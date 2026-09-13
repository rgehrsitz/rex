package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

const durableTransactionScript = "script"
const durableTransactionWatch = "watch"

const fencedScriptPrefix = `
local function key_type(key)
  return redis.call('TYPE', key)['ok']
end
local function fence()
  if ARGV[1] ~= '1' then return nil end
  if key_type(KEYS[2]) ~= 'string' or redis.call('GET', KEYS[2]) ~= ARGV[2] then
    return {'OWNER'}
  end
  if key_type(KEYS[3]) ~= 'hash' or redis.call('HGET', KEYS[3], ARGV[3]) ~= ARGV[4] then
    return {'OWNER'}
  end
  return nil
end
local function journal_type()
  local kind = key_type(KEYS[1])
  if kind ~= 'none' and kind ~= 'hash' then return {'TYPE', KEYS[1], kind} end
  return nil
end
local function denied(command, ...)
  if not redis.acl_check_cmd(command, ...) then return {'ACL', command} end
  return nil
end
`

var beginDurableScript = redis.NewScript(fencedScriptPrefix + `
local bad = journal_type(); if bad then return bad end
bad = fence(); if bad then return bad end
local payload = redis.call('HGET', KEYS[1], 'payload')
if payload and payload ~= ARGV[6] then return {'RECON', 'payload'} end
local program = redis.call('HGET', KEYS[1], 'program_id')
if program and program ~= ARGV[7] then return {'PROGRAM', program} end
local terminal = redis.call('HGET', KEYS[1], 'terminal')
local attempts = redis.call('HGET', KEYS[1], 'attempts') or '0'
if terminal then return {'TERMINAL', terminal, attempts} end
bad = denied('HINCRBY', KEYS[1], 'attempts', 1); if bad then return bad end
bad = denied('HSETNX', KEYS[1], 'payload', ARGV[6]); if bad then return bad end
bad = denied('HSETNX', KEYS[1], 'program_id', ARGV[7]); if bad then return bad end
bad = denied('PEXPIRE', KEYS[1], ARGV[5]); if bad then return bad end
attempts = redis.call('HINCRBY', KEYS[1], 'attempts', 1)
redis.call('HSETNX', KEYS[1], 'payload', ARGV[6])
redis.call('HSETNX', KEYS[1], 'program_id', ARGV[7])
redis.call('PEXPIRE', KEYS[1], ARGV[5])
return {'OK', attempts}
`)

var applyInputDurableScript = redis.NewScript(fencedScriptPrefix + `
local bad = journal_type(); if bad then return bad end
bad = fence(); if bad then return bad end
local marker = redis.call('HGET', KEYS[1], 'input_commit')
if marker then
  if marker == ARGV[6] then return {'DONE'} end
  return {'RECON', 'input marker'}
end
for i = 4, #KEYS do bad = denied('SET', KEYS[i], ARGV[i + 3]); if bad then return bad end end
bad = denied('HSET', KEYS[1], 'input_commit', ARGV[6]); if bad then return bad end
bad = denied('PEXPIRE', KEYS[1], ARGV[5]); if bad then return bad end
for i = 4, #KEYS do redis.call('SET', KEYS[i], ARGV[i + 3]) end
redis.call('HSET', KEYS[1], 'input_commit', ARGV[6])
redis.call('PEXPIRE', KEYS[1], ARGV[5])
return {'OK'}
`)

var snapshotReadDurableScript = redis.NewScript(fencedScriptPrefix + `
local bad = journal_type(); if bad then return bad end
bad = fence(); if bad then return bad end
local encoded = redis.call('HGET', KEYS[1], ARGV[6])
if encoded then return {'DONE', encoded} end
return {'MISS'}
`)

var snapshotStoreDurableScript = redis.NewScript(fencedScriptPrefix + `
local bad = journal_type(); if bad then return bad end
bad = fence(); if bad then return bad end
local encoded = redis.call('HGET', KEYS[1], ARGV[6])
if encoded then return {'DONE', encoded} end
bad = denied('HSET', KEYS[1], ARGV[6], ARGV[7]); if bad then return bad end
bad = denied('PEXPIRE', KEYS[1], ARGV[5]); if bad then return bad end
redis.call('HSET', KEYS[1], ARGV[6], ARGV[7])
redis.call('PEXPIRE', KEYS[1], ARGV[5])
return {'OK', ARGV[7]}
`)

var completeDurableScript = redis.NewScript(fencedScriptPrefix + `
local bad = journal_type(); if bad then return bad end
bad = fence(); if bad then return bad end
local stream_type = key_type(KEYS[4])
if stream_type ~= 'none' and stream_type ~= 'stream' then return {'TYPE', KEYS[4], stream_type} end
local terminal = redis.call('HGET', KEYS[1], 'terminal')
if terminal and terminal ~= 'completed' then return {'RECON', terminal} end
bad = denied('HSET', KEYS[1], 'terminal', 'completed'); if bad then return bad end
bad = denied('PEXPIRE', KEYS[1], ARGV[5]); if bad then return bad end
bad = denied('XACK', KEYS[4], ARGV[6], ARGV[7]); if bad then return bad end
if not terminal then
  redis.call('HSET', KEYS[1], 'terminal', 'completed')
  redis.call('PEXPIRE', KEYS[1], ARGV[5])
end
redis.call('XACK', KEYS[4], ARGV[6], ARGV[7])
return {'OK'}
`)

// commitDurableScript arguments after the common fence arguments are:
// marker field, marker bytes, public-write flag, max stream length, stream
// payload fields, write count, then operation/value pairs. Write keys begin at
// KEYS[5] in the same order.
var commitDurableScript = redis.NewScript(fencedScriptPrefix + `
local bad = journal_type(); if bad then return bad end
local existing = redis.call('HGET', KEYS[1], ARGV[6])
if existing then return {'DONE', existing} end
bad = fence(); if bad then return bad end
if ARGV[8] == '1' then
  local kind = key_type(KEYS[4])
  if kind ~= 'none' and kind ~= 'stream' then return {'TYPE', KEYS[4], kind} end
end
local count = tonumber(ARGV[18])
for i = 1, count do
  local operation = ARGV[17 + i * 2]
  local value = ARGV[18 + i * 2]
  if operation == 'del' then bad = denied('DEL', KEYS[4 + i]) else bad = denied('SET', KEYS[4 + i], value) end
  if bad then return bad end
end
if ARGV[8] == '1' then
  bad = denied('XADD', KEYS[4], 'MAXLEN', '~', ARGV[9], '*',
    'protocol', ARGV[10], 'input_id', ARGV[11], 'input_stream', ARGV[12],
    'namespace', ARGV[13], 'program_id', ARGV[14], 'round', ARGV[15],
    'output_id', ARGV[16], 'payload', ARGV[17]); if bad then return bad end
end
bad = denied('HSET', KEYS[1], ARGV[6], ARGV[7]); if bad then return bad end
bad = denied('PEXPIRE', KEYS[1], ARGV[5]); if bad then return bad end
for i = 1, count do
  local operation = ARGV[17 + i * 2]
  local value = ARGV[18 + i * 2]
  if operation == 'del' then redis.call('DEL', KEYS[4 + i]) else redis.call('SET', KEYS[4 + i], value) end
end
if ARGV[8] == '1' then
  redis.call('XADD', KEYS[4], 'MAXLEN', '~', ARGV[9], '*',
    'protocol', ARGV[10], 'input_id', ARGV[11], 'input_stream', ARGV[12],
    'namespace', ARGV[13], 'program_id', ARGV[14], 'round', ARGV[15],
    'output_id', ARGV[16], 'payload', ARGV[17])
end
redis.call('HSET', KEYS[1], ARGV[6], ARGV[7])
redis.call('PEXPIRE', KEYS[1], ARGV[5])
return {'OK', ARGV[7]}
`)

var durableScripts = []*redis.Script{
	beginDurableScript,
	applyInputDurableScript,
	snapshotReadDurableScript,
	snapshotStoreDurableScript,
	completeDurableScript,
	commitDurableScript,
}

var durableScriptCapability = redis.NewScript(`return redis.acl_check_cmd('TYPE', KEYS[1])`)

func (d *RedisDurable) loadScripts(ctx context.Context) error {
	allowed, err := durableScriptCapability.Run(ctx, d.client, []string{ownershipRegistry}).Bool()
	if err != nil {
		return fmt.Errorf("durable script mode requires Redis 7.0+ and scripting/type ACL permissions: %w", err)
	}
	if !allowed {
		return fmt.Errorf("durable script mode requires scripting and type ACL permissions")
	}
	for _, script := range durableScripts {
		if err := script.Load(ctx, d.client).Err(); err != nil {
			return fmt.Errorf("load durable transaction scripts: %w", err)
		}
	}
	return nil
}

func (d *RedisDurable) scriptKeys(journal string, extra ...string) []string {
	keys := []string{journal, d.ownerKey(), ownershipRegistry}
	return append(keys, extra...)
}

func (d *RedisDurable) scriptArgs(extra ...interface{}) []interface{} {
	fenced := "0"
	if d.ownership != nil {
		fenced = "1"
	}
	args := []interface{}{fenced, d.ownerID, d.options.Namespace, d.claim(), d.options.JournalTTL.Milliseconds()}
	return append(args, extra...)
}

func runDurableScript(ctx context.Context, client redis.Scripter, script *redis.Script, keys []string, args ...interface{}) ([]interface{}, error) {
	value, err := script.Run(ctx, client, keys, args...).Result()
	if err != nil {
		return nil, err
	}
	reply, ok := value.([]interface{})
	if !ok || len(reply) == 0 {
		return nil, fmt.Errorf("invalid durable script reply %T", value)
	}
	if _, ok := reply[0].(string); !ok {
		return nil, fmt.Errorf("invalid durable script status %T", reply[0])
	}
	return reply, nil
}

func scriptStatus(reply []interface{}) string { return reply[0].(string) }

func scriptString(reply []interface{}, index int) (string, error) {
	if index >= len(reply) {
		return "", fmt.Errorf("durable script reply is missing field %d", index)
	}
	value, ok := reply[index].(string)
	if !ok {
		return "", fmt.Errorf("durable script reply field %d has type %T", index, reply[index])
	}
	return value, nil
}

func durableScriptError(reply []interface{}) error {
	switch scriptStatus(reply) {
	case "OWNER":
		return ErrDurableOwnership
	case "RECON":
		detail, _ := scriptString(reply, 1)
		return fmt.Errorf("%w: %s", ErrDurableReconciliation, detail)
	case "PROGRAM":
		program, _ := scriptString(reply, 1)
		return fmt.Errorf("%w: event requires program %s", ErrDurableProgramMismatch, program)
	case "TYPE":
		key, _ := scriptString(reply, 1)
		kind, _ := scriptString(reply, 2)
		return fmt.Errorf("%w: key %q has type %s", errDurablePreflight, key, kind)
	case "ACL":
		command, _ := scriptString(reply, 1)
		return fmt.Errorf("%w: ACL denies %s", errDurablePreflight, command)
	default:
		return fmt.Errorf("unexpected durable script status %q", scriptStatus(reply))
	}
}

func (d *RedisDurable) beginScript(ctx context.Context, event DurableEvent, programID string) (JournalStatus, error) {
	reply, err := runDurableScript(ctx, d.client, beginDurableScript, d.scriptKeys(d.journalKey(event.ID)), d.scriptArgs(event.Payload, programID)...)
	if err != nil {
		return JournalStatus{}, infrastructureFailure("begin durable event", err)
	}
	switch scriptStatus(reply) {
	case "OK":
		attempts, ok := reply[1].(int64)
		if !ok {
			return JournalStatus{}, infrastructureFailure("begin durable event", fmt.Errorf("invalid attempt count %T", reply[1]))
		}
		return JournalStatus{Attempts: attempts}, nil
	case "TERMINAL":
		terminal, terminalErr := scriptString(reply, 1)
		attemptText, attemptErr := scriptString(reply, 2)
		attempts, parseErr := strconv.ParseInt(attemptText, 10, 64)
		if err := errors.Join(terminalErr, attemptErr, parseErr); err != nil {
			return JournalStatus{}, infrastructureFailure("begin durable event", err)
		}
		return JournalStatus{Attempts: attempts, Terminal: terminal}, nil
	default:
		err := durableScriptError(reply)
		if errors.Is(err, ErrDurableReconciliation) || errors.Is(err, ErrDurableProgramMismatch) {
			return JournalStatus{}, err
		}
		return JournalStatus{}, infrastructureFailure("begin durable event", err)
	}
}

func (d *RedisDurable) applyInputScript(ctx context.Context, journal, marker string, encoded map[string][]byte) error {
	keys := d.scriptKeys(journal)
	args := d.scriptArgs(marker)
	for key, value := range encoded {
		keys = append(keys, key)
		args = append(args, value)
	}
	reply, err := runDurableScript(ctx, d.client, applyInputDurableScript, keys, args...)
	if err != nil {
		return infrastructureFailure("input transaction outcome unknown", err)
	}
	if status := scriptStatus(reply); status == "OK" || status == "DONE" {
		return nil
	}
	err = durableScriptError(reply)
	if errors.Is(err, ErrDurableReconciliation) {
		return err
	}
	return infrastructureFailure("input transaction outcome unknown", err)
}

func (d *RedisDurable) readSnapshotScript(ctx context.Context, metadata durableEventContext, keys []string) (map[string]Fact, error) {
	journal := d.journalKey(metadata.EventID)
	field := "snapshot:" + strconv.Itoa(metadata.Round)
	reply, err := runDurableScript(ctx, d.client, snapshotReadDurableScript, d.scriptKeys(journal), d.scriptArgs(field)...)
	if err != nil {
		return nil, infrastructureFailure("read historical snapshot", err)
	}
	switch scriptStatus(reply) {
	case "DONE":
		encoded, valueErr := scriptString(reply, 1)
		if valueErr != nil {
			return nil, infrastructureFailure("read historical snapshot", valueErr)
		}
		return decodeSnapshot([]byte(encoded))
	case "MISS":
	default:
		return nil, infrastructureFailure("snapshot transaction", durableScriptError(reply))
	}
	snapshot, err := d.store.readRedisSnapshot(ctx, keys)
	if err != nil {
		return nil, infrastructureFailure("read snapshot", err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode historical snapshot: %w", err)
	}
	reply, err = runDurableScript(ctx, d.client, snapshotStoreDurableScript, d.scriptKeys(journal), d.scriptArgs(field, encoded)...)
	if err != nil {
		return nil, infrastructureFailure("record historical snapshot", err)
	}
	if status := scriptStatus(reply); status != "OK" && status != "DONE" {
		return nil, infrastructureFailure("record historical snapshot", durableScriptError(reply))
	}
	winner, err := scriptString(reply, 1)
	if err != nil {
		return nil, infrastructureFailure("record historical snapshot", err)
	}
	return decodeSnapshot([]byte(winner))
}

func (d *RedisDurable) completeScript(ctx context.Context, eventID string) error {
	journal := d.journalKey(eventID)
	reply, err := runDurableScript(ctx, d.client, completeDurableScript, d.scriptKeys(journal, d.options.Stream), d.scriptArgs(d.options.Group, eventID)...)
	if err != nil {
		return infrastructureFailure("complete durable event", err)
	}
	if scriptStatus(reply) == "OK" {
		return nil
	}
	err = durableScriptError(reply)
	if errors.Is(err, ErrDurableReconciliation) {
		return err
	}
	return infrastructureFailure("complete durable event", err)
}

func (d *RedisDurable) commitScript(ctx context.Context, metadata durableEventContext, request CommitRequest, journal, field string, encoded [][]byte, marker, notification []byte, outputID string, public bool, result CommitResult) (CommitResult, error) {
	keys := d.scriptKeys(journal, d.options.OutputStream)
	publicArg := "0"
	if public {
		publicArg = "1"
	}
	args := d.scriptArgs(field, marker, publicArg, d.options.OutputMaxLen, durableProtocolVersion,
		metadata.EventID, d.options.Stream, d.options.Namespace, metadata.ProgramID, request.Round, outputID, notification, len(request.Writes))
	for i, write := range request.Writes {
		keys = append(keys, write.Key)
		operation := "set"
		if write.Delete {
			operation = "del"
		}
		args = append(args, operation, encoded[i])
	}
	reply, err := runDurableScript(ctx, d.client, commitDurableScript, keys, args...)
	if err != nil {
		return CommitResult{Outcome: Unknown}, infrastructureFailure("durable transaction outcome unknown", err)
	}
	switch scriptStatus(reply) {
	case "OK":
		return result, nil
	case "DONE":
		stored, valueErr := scriptString(reply, 1)
		if valueErr != nil {
			return CommitResult{Outcome: Unknown}, infrastructureFailure("read commit marker", valueErr)
		}
		var committed CommitResult
		if err := json.Unmarshal([]byte(stored), &committed); err != nil {
			return CommitResult{Outcome: Unknown}, fmt.Errorf("%w: decode durable commit marker: %w", ErrDurableReconciliation, err)
		}
		return committed, nil
	default:
		err = durableScriptError(reply)
		if errors.Is(err, ErrDurableReconciliation) {
			return CommitResult{Outcome: Unknown}, err
		}
		if errors.Is(err, errDurablePreflight) || errors.Is(err, ErrDurableOwnership) {
			return CommitResult{Outcome: NotCommitted}, infrastructureFailure("durable transaction preflight", err)
		}
		return CommitResult{Outcome: Unknown}, infrastructureFailure("durable transaction outcome unknown", err)
	}
}
