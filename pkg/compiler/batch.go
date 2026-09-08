package compiler

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"strings"
	"unicode/utf8"
)

// BatchVersion is the current artifact/execution contract. Version remains the
// legacy v3 jump-program version for explicitly compatible embedded callers.
const BatchVersion uint32 = 4
const MaxProgramBytes = 8 << 20
const MaxProgramRules = 10000
const MaxConditionNodes = 100000
const MaxDependencies = 65536
const batchHeaderSize = 16

// ParseBatch validates the bounded, script-free v4 source contract.
func ParseBatch(data []byte) (*Ruleset, error) {
	if len(data) > MaxProgramBytes {
		return nil, fmt.Errorf("program exceeds %d bytes", MaxProgramBytes)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("program must be valid UTF-8")
	}
	// Bound nesting before recursive source validation. Ignore brackets in strings.
	depth := 0
	quoted, escaped := false, false
	for _, c := range data {
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
		}
		if c == '{' || c == '[' {
			depth++
			if depth > 64 {
				return nil, fmt.Errorf("program nesting exceeds 64")
			}
		}
		if c == '}' || c == ']' {
			depth--
		}
	}
	rules, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if err := validateBatchShapes(data); err != nil {
		return nil, err
	}
	if len(rules.Rules) > MaxProgramRules {
		return nil, fmt.Errorf("program exceeds %d rules", MaxProgramRules)
	}

	nodes := 0
	facts := map[string]bool{}
	var check func([]*ConditionOrGroup) error
	check = func(children []*ConditionOrGroup) error {
		for _, n := range children {
			nodes++
			if nodes > MaxConditionNodes {
				return fmt.Errorf("program exceeds 100000 condition nodes")
			}
			if n.Fact != "" {
				facts[n.Fact] = true
				valid := false
				switch n.Value.(type) {
				case float64:
					valid = n.Operator == "EQ" || n.Operator == "NEQ" || n.Operator == "LT" || n.Operator == "LTE" || n.Operator == "GT" || n.Operator == "GTE"
				case string:
					valid = n.Operator == "EQ" || n.Operator == "NEQ" || n.Operator == "CONTAINS" || n.Operator == "NOT_CONTAINS"
				case bool:
					valid = n.Operator == "EQ" || n.Operator == "NEQ"
				}
				if !valid {
					return fmt.Errorf("v4 condition %q requires a scalar constant and matching operator", n.Fact)
				}
			}
			if err := check(n.All); err != nil {
				return err
			}
			if err := check(n.Any); err != nil {
				return err
			}
		}
		return nil
	}
	for _, r := range rules.Rules {
		if err := check(r.Conditions.All); err != nil {
			return nil, err
		}
		if err := check(r.Conditions.Any); err != nil {
			return nil, err
		}
		if len(r.Scripts) > 0 {
			return nil, fmt.Errorf("v4 scripts unavailable until M6: rule %q", r.Name)
		}
		for _, a := range r.Actions {
			if v, ok := a.Value.(string); ok && strings.HasPrefix(v, "{") && strings.HasSuffix(v, "}") {
				return nil, fmt.Errorf("v4 script calls unavailable: rule %q", r.Name)
			}
		}
	}
	if len(facts) > MaxDependencies {
		return nil, fmt.Errorf("program exceeds 65536 dependencies")
	}
	return rules, nil
}

// CompileBatch preserves boolean grouping in a deterministic structured IR.
// Header: version, CRC32(payload), payload length, magic REXB, then canonical JSON.
func CompileBatch(source []byte) ([]byte, error) {
	rules, err := ParseBatch(source)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(rules)
	if err != nil {
		return nil, err
	}
	if len(payload) > MaxProgramBytes {
		return nil, fmt.Errorf("canonical program exceeds byte limit")
	}
	out := make([]byte, batchHeaderSize, len(payload)+batchHeaderSize)
	binary.LittleEndian.PutUint32(out, BatchVersion)
	binary.LittleEndian.PutUint32(out[4:], crc32.ChecksumIEEE(payload))
	binary.LittleEndian.PutUint32(out[8:], uint32(len(payload)))
	copy(out[12:], "REXB")
	return append(out, payload...), nil
}

func DecodeBatch(data []byte) (*Ruleset, error) {
	if len(data) < batchHeaderSize || len(data) > MaxProgramBytes+batchHeaderSize {
		return nil, fmt.Errorf("invalid v4 artifact size")
	}
	if binary.LittleEndian.Uint32(data) != BatchVersion || string(data[12:16]) != "REXB" {
		return nil, fmt.Errorf("unsupported batch artifact header")
	}
	payload := data[batchHeaderSize:]
	if uint64(binary.LittleEndian.Uint32(data[8:])) != uint64(len(payload)) {
		return nil, fmt.Errorf("batch artifact length mismatch")
	}
	if binary.LittleEndian.Uint32(data[4:]) != crc32.ChecksumIEEE(payload) {
		return nil, fmt.Errorf("batch artifact checksum mismatch")
	}
	return ParseBatch(payload)
}

// validateBatchShapes checks field presence that the shared legacy AST loses
// (for example, an explicitly empty all beside a populated any).
func validateBatchShapes(data []byte) error {
	var root struct {
		Rules []map[string]json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	var group func(json.RawMessage, bool) error
	group = func(raw json.RawMessage, leafAllowed bool) error {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		all, hasAll := fields["all"]
		any, hasAny := fields["any"]
		if hasAll == hasAny {
			if !hasAll && leafAllowed {
				return nil
			} // shared parser validates leaf fields
			return fmt.Errorf("v4 condition group requires exactly one of all or any")
		}
		if len(fields) != 1 {
			return fmt.Errorf("v4 group cannot contain leaf fields")
		}
		children := all
		if hasAny {
			children = any
		}
		var nodes []json.RawMessage
		if err := json.Unmarshal(children, &nodes); err != nil {
			return err
		}
		if len(nodes) == 0 {
			return fmt.Errorf("v4 condition group cannot be empty")
		}
		for _, node := range nodes {
			if err := group(node, true); err != nil {
				return err
			}
		}
		return nil
	}
	for i, r := range root.Rules {
		if _, ok := r["scripts"]; ok {
			name := fmt.Sprintf("rules[%d]", i)
			if rawName, ok := r["name"]; ok {
				var ruleName string
				if json.Unmarshal(rawName, &ruleName) == nil && ruleName != "" {
					name = fmt.Sprintf("rule %q", ruleName)
				}
			}
			return fmt.Errorf("v4 scripts unavailable until M6: %s", name)
		}
		if err := group(r["conditions"], false); err != nil {
			return err
		}
	}
	return nil
}
