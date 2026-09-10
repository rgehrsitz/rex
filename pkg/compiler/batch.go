package compiler

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// BatchVersion is the untyped batch contract. TypedFactsVersion extends its
// structured representation with fact declarations. Version remains the legacy
// v3 jump-program version for explicitly compatible embedded callers.
const BatchVersion uint32 = 4
const TypedFactsVersion uint32 = 5
const TemporalVersion uint32 = 6
const ChangeOnlyVersion uint32 = 7
const CapabilityChangeOnly = "change_only"
const CapabilityTemporal = "temporal"
const CapabilityTypedFacts = "typed_facts"
const EmitOnChange = "on_change"
const MaxProgramBytes = 8 << 20
const MaxProgramRules = 10000
const MaxConditionNodes = 100000
const MaxDependencies = 65536
const MaxTemporalConditions = 10000
const MaxTemporalDuration = 365 * 24 * time.Hour
const batchHeaderSize = 16

// ErrTypedFactContract classifies source errors caused by v5 declarations.
var ErrTypedFactContract = errors.New("typed fact contract")

func IsTypedFactError(err error) bool { return errors.Is(err, ErrTypedFactContract) }

func typedFactErrorf(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrTypedFactContract, fmt.Sprintf(format, args...))
}

func IsBatchVersion(version uint32) bool {
	return version == BatchVersion || version == TypedFactsVersion || version == TemporalVersion || version == ChangeOnlyVersion
}

func BatchArtifactVersion(data []byte) (uint32, error) {
	if len(data) < batchHeaderSize {
		return 0, fmt.Errorf("invalid batch artifact size")
	}
	version := binary.LittleEndian.Uint32(data)
	if !IsBatchVersion(version) {
		return 0, fmt.Errorf("unsupported batch artifact version %d", version)
	}
	return version, nil
}

// ParseBatch validates the bounded, script-free batch source contract.
func ParseBatch(data []byte) (*Ruleset, error) {
	rules, _, err := parseBatch(data, false)
	return rules, err
}

func parseBatch(data []byte, artifact bool) (*Ruleset, bool, error) {
	if len(data) > MaxProgramBytes {
		return nil, false, fmt.Errorf("program exceeds %d bytes", MaxProgramBytes)
	}
	if !utf8.Valid(data) {
		return nil, false, fmt.Errorf("program must be valid UTF-8")
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
				return nil, false, fmt.Errorf("program nesting exceeds 64")
			}
		}
		if c == '}' || c == ']' {
			depth--
		}
	}
	if err := validateFactDeclarationShapes(data); err != nil {
		return nil, false, err
	}
	rules, err := Parse(data)
	if err != nil {
		return nil, false, err
	}
	declaresCapabilities, err := validateBatchShapes(data, artifact)
	if err != nil {
		return nil, false, err
	}
	if len(rules.Rules) > MaxProgramRules {
		return nil, false, fmt.Errorf("program exceeds %d rules", MaxProgramRules)
	}
	if err := validateFactDeclarations(rules); err != nil {
		return nil, false, err
	}

	nodes := 0
	temporalConditions := 0
	facts := map[string]bool{}
	changeTargets := map[string]bool{}
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
				if n.For != "" {
					duration, err := time.ParseDuration(n.For)
					if err != nil || duration <= 0 || duration > MaxTemporalDuration {
						return fmt.Errorf("temporal condition %q requires for duration between 1ns and %s", n.Fact, MaxTemporalDuration)
					}
					temporalConditions++
					if temporalConditions > MaxTemporalConditions {
						return fmt.Errorf("program exceeds %d temporal conditions", MaxTemporalConditions)
					}
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
			return nil, false, err
		}
		if err := check(r.Conditions.Any); err != nil {
			return nil, false, err
		}
		if r.Scripts != nil {
			return nil, false, fmt.Errorf("scripts are no longer supported: rule %q declares scripts", r.Name)
		}
		if r.Emit == EmitOnChange {
			for _, action := range r.Actions {
				changeTargets[action.Target] = true
			}
		}
		for actionIndex, a := range r.Actions {
			if a.Value == nil {
				return nil, false, fmt.Errorf("rule %q action %d: null action values are unsupported", r.Name, actionIndex)
			}
			if v, ok := a.Value.(string); ok && strings.HasPrefix(v, "{") && strings.HasSuffix(v, "}") {
				return nil, false, fmt.Errorf("scripts are no longer supported: rule %q action %d calls %q", r.Name, actionIndex, v)
			}
			if rules.Facts != nil {
				declaration, ok := rules.Facts[a.Target]
				if !ok {
					return nil, false, typedFactErrorf("rule %q action %d target %q is undeclared", r.Name, actionIndex, a.Target)
				}
				if !declaration.Accepts(a.Value) {
					return nil, false, typedFactErrorf("rule %q action %d target %q requires %s", r.Name, actionIndex, a.Target, declaration.Type)
				}
			}
		}
	}
	for target := range changeTargets {
		facts[target] = true
	}
	if len(facts)+temporalConditions > MaxDependencies {
		return nil, false, fmt.Errorf("program exceeds 65536 dependencies")
	}
	const reserved = "__rex_temporal_"
	for name := range rules.Facts {
		if strings.HasPrefix(name, reserved) {
			return nil, false, fmt.Errorf("fact name %q uses reserved temporal state prefix", name)
		}
	}
	for _, rule := range rules.Rules {
		for _, action := range rule.Actions {
			if strings.HasPrefix(action.Target, reserved) {
				return nil, false, fmt.Errorf("action target %q uses reserved temporal state prefix", action.Target)
			}
		}
	}
	for fact := range facts {
		if strings.HasPrefix(fact, reserved) {
			return nil, false, fmt.Errorf("condition fact %q uses reserved temporal state prefix", fact)
		}
	}
	return rules, declaresCapabilities, nil
}

func validateFactDeclarations(rules *Ruleset) error {
	if rules.Facts == nil {
		return nil
	}
	if len(rules.Facts) == 0 || len(rules.Facts) > MaxDependencies {
		return typedFactErrorf("declarations must contain 1..%d facts", MaxDependencies)
	}
	names := make([]string, 0, len(rules.Facts))
	for name := range rules.Facts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		declaration := rules.Facts[name]
		if err := validateBytecodeString("Fact declaration name", name); err != nil {
			return typedFactErrorf("declaration %q: %v", name, err)
		}
		if !declaration.Valid() {
			return typedFactErrorf("declaration %q has unsupported type %q", name, declaration.Type)
		}
	}
	var check func([]*ConditionOrGroup) error
	check = func(nodes []*ConditionOrGroup) error {
		for _, node := range nodes {
			if node.Fact != "" {
				declaration, ok := rules.Facts[node.Fact]
				if !ok {
					return typedFactErrorf("condition fact %q is undeclared", node.Fact)
				}
				if !declaration.Accepts(node.Value) {
					return typedFactErrorf("condition fact %q requires a %s constant", node.Fact, declaration.Type)
				}
			}
			if err := check(node.All); err != nil {
				return err
			}
			if err := check(node.Any); err != nil {
				return err
			}
		}
		return nil
	}
	for _, rule := range rules.Rules {
		if err := check(rule.Conditions.All); err != nil {
			return err
		}
		if err := check(rule.Conditions.Any); err != nil {
			return err
		}
	}
	return nil
}

func validateFactDeclarationShapes(data []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil // The authoritative parser reports malformed JSON with location.
	}
	raw, present := root["facts"]
	if !present {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return typedFactErrorf("declarations must be an object")
	}
	var declarations map[string]json.RawMessage
	if err := json.Unmarshal(raw, &declarations); err != nil || declarations == nil {
		return typedFactErrorf("declarations must be an object")
	}
	if len(declarations) == 0 || len(declarations) > MaxDependencies {
		return typedFactErrorf("declarations must contain 1..%d facts", MaxDependencies)
	}
	names := make([]string, 0, len(declarations))
	for name := range declarations {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		declarationJSON := declarations[name]
		var declaration FactDeclaration
		decoder := json.NewDecoder(bytes.NewReader(declarationJSON))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&declaration); err != nil {
			return typedFactErrorf("declaration %q: %v", name, err)
		}
		if !declaration.Valid() {
			return typedFactErrorf("declaration %q has unsupported type %q", name, declaration.Type)
		}
	}
	return nil
}

func (d FactDeclaration) Valid() bool {
	return d.Type == FactNumber || d.Type == FactString || d.Type == FactBoolean
}

func (d FactDeclaration) Accepts(value interface{}) bool {
	if value == nil {
		return d.Nullable
	}
	switch d.Type {
	case FactNumber:
		number, ok := value.(float64)
		return ok && !math.IsNaN(number) && !math.IsInf(number, 0)
	case FactString:
		_, ok := value.(string)
		return ok
	case FactBoolean:
		_, ok := value.(bool)
		return ok
	default:
		return false
	}
}

// CompileBatch preserves boolean grouping in a deterministic structured IR.
// Header: version, CRC32(payload), payload length, magic REXB, then canonical JSON.
func CompileBatch(source []byte) ([]byte, error) {
	rules, _, err := parseBatch(source, false)
	if err != nil {
		return nil, err
	}
	capabilities := observedCapabilities(rules)
	version := BatchVersion
	if HasChangeOnlyRules(rules) {
		version = ChangeOnlyVersion
		rules.Capabilities = capabilities
	} else if HasTemporalConditions(rules) {
		version = TemporalVersion
	} else if rules.Facts != nil {
		version = TypedFactsVersion
	}
	payload, err := json.Marshal(rules)
	if err != nil {
		return nil, err
	}
	if len(payload) > MaxProgramBytes {
		return nil, fmt.Errorf("canonical program exceeds byte limit")
	}
	out := make([]byte, batchHeaderSize, len(payload)+batchHeaderSize)
	binary.LittleEndian.PutUint32(out, version)
	binary.LittleEndian.PutUint32(out[4:], crc32.ChecksumIEEE(payload))
	binary.LittleEndian.PutUint32(out[8:], uint32(len(payload)))
	copy(out[12:], "REXB")
	return append(out, payload...), nil
}

func DecodeBatch(data []byte) (*Ruleset, error) {
	if len(data) < batchHeaderSize || len(data) > MaxProgramBytes+batchHeaderSize {
		return nil, fmt.Errorf("invalid batch artifact size")
	}
	version := binary.LittleEndian.Uint32(data)
	if !IsBatchVersion(version) || string(data[12:16]) != "REXB" {
		return nil, fmt.Errorf("unsupported batch artifact header")
	}
	payload := data[batchHeaderSize:]
	if uint64(binary.LittleEndian.Uint32(data[8:])) != uint64(len(payload)) {
		return nil, fmt.Errorf("batch artifact length mismatch")
	}
	if binary.LittleEndian.Uint32(data[4:]) != crc32.ChecksumIEEE(payload) {
		return nil, fmt.Errorf("batch artifact checksum mismatch")
	}
	rules, declaresCapabilities, err := parseBatch(payload, true)
	if err != nil {
		return nil, err
	}
	if version == BatchVersion && rules.Facts != nil {
		return nil, fmt.Errorf("v4 artifact cannot contain typed fact declarations")
	}
	if version == TypedFactsVersion && rules.Facts == nil {
		return nil, fmt.Errorf("v5 artifact requires typed fact declarations")
	}
	temporal := temporalConditionCount(rules)
	changeOnly := HasChangeOnlyRules(rules)
	if version != ChangeOnlyVersion && declaresCapabilities {
		return nil, fmt.Errorf("v%d artifact cannot declare capabilities", version)
	}
	if (version == BatchVersion || version == TypedFactsVersion) && temporal > 0 {
		return nil, fmt.Errorf("v%d artifact cannot contain temporal conditions", version)
	}
	if version == TemporalVersion && temporal == 0 {
		return nil, fmt.Errorf("v6 artifact requires temporal conditions")
	}
	if version != ChangeOnlyVersion && changeOnly {
		return nil, fmt.Errorf("v%d artifact cannot contain change-only emission", version)
	}
	if version == ChangeOnlyVersion {
		observed := observedCapabilities(rules)
		if !declaresCapabilities || !changeOnly || !slices.Equal(rules.Capabilities, observed) {
			return nil, fmt.Errorf("v7 artifact capabilities must canonically match its features")
		}
	}
	return rules, nil
}

func observedCapabilities(rules *Ruleset) []string {
	var capabilities []string
	if HasChangeOnlyRules(rules) {
		capabilities = append(capabilities, CapabilityChangeOnly)
	}
	if HasTemporalConditions(rules) {
		capabilities = append(capabilities, CapabilityTemporal)
	}
	if rules != nil && rules.Facts != nil {
		capabilities = append(capabilities, CapabilityTypedFacts)
	}
	return capabilities
}

func HasChangeOnlyRules(rules *Ruleset) bool {
	if rules == nil {
		return false
	}
	for _, rule := range rules.Rules {
		if rule.Emit == EmitOnChange {
			return true
		}
	}
	return false
}

func temporalConditionCount(rules *Ruleset) int {
	count := 0
	var walk func([]*ConditionOrGroup)
	walk = func(nodes []*ConditionOrGroup) {
		for _, node := range nodes {
			if node.For != "" {
				count++
			}
			walk(node.All)
			walk(node.Any)
		}
	}
	for _, rule := range rules.Rules {
		walk(rule.Conditions.All)
		walk(rule.Conditions.Any)
	}
	return count
}

func HasTemporalConditions(rules *Ruleset) bool {
	return rules != nil && temporalConditionCount(rules) > 0
}

// validateBatchShapes checks field presence that the shared legacy AST loses
// (for example, an explicitly empty all beside a populated any).
func validateBatchShapes(data []byte, artifact bool) (bool, error) {
	var root struct {
		Capabilities json.RawMessage              `json:"capabilities"`
		Rules        []map[string]json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return false, err
	}
	declaresCapabilities := root.Capabilities != nil
	if declaresCapabilities && !artifact {
		return true, fmt.Errorf("capabilities is compiler-owned artifact metadata")
	}
	var group func(json.RawMessage, bool) error
	group = func(raw json.RawMessage, leafAllowed bool) error {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		all, hasAll := fields["all"]
		any, hasAny := fields["any"]
		if rawFor, present := fields["for"]; present {
			var duration string
			if json.Unmarshal(rawFor, &duration) != nil || duration == "" {
				return fmt.Errorf("temporal condition for must be a nonempty duration string")
			}
		}
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
		if rawEmit, present := r["emit"]; present {
			var emit string
			if json.Unmarshal(rawEmit, &emit) != nil || emit != EmitOnChange {
				return declaresCapabilities, fmt.Errorf("rules[%d] emit must be \"on_change\" when present", i)
			}
		}
		if _, ok := r["scripts"]; ok {
			name := fmt.Sprintf("rules[%d]", i)
			if rawName, ok := r["name"]; ok {
				var ruleName string
				if json.Unmarshal(rawName, &ruleName) == nil && ruleName != "" {
					name = fmt.Sprintf("rule %q", ruleName)
				}
			}
			return declaresCapabilities, fmt.Errorf("scripts are no longer supported: %s declares scripts", name)
		}
		if err := group(r["conditions"], false); err != nil {
			return declaresCapabilities, err
		}
	}
	return declaresCapabilities, nil
}
