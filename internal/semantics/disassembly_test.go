package semantics

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"rgehrsitz/rex/pkg/compiler"
)

// disassemble is deliberately internal test tooling, not a public CLI or an
// oracle dependency. It decodes the script-free corpus with explicit bounds.
func disassemble(b compiler.BytecodeFile) (string, error) {
	var out strings.Builder
	code := b.Instructions
	for offset := 0; offset < len(code); {
		start := offset
		op := compiler.Opcode(code[offset])
		offset++
		operand := ""
		take := func(n int) ([]byte, error) {
			if n > len(code)-offset {
				return nil, fmt.Errorf("truncated operand at %d", start)
			}
			v := code[offset : offset+n]
			offset += n
			return v, nil
		}
		switch op {
		case compiler.RULE_START, compiler.LOAD_FACT_FLOAT, compiler.LOAD_FACT_STRING, compiler.LOAD_FACT_BOOL, compiler.LOAD_CONST_STRING, compiler.ACTION_TYPE, compiler.ACTION_TARGET, compiler.ACTION_VALUE_STRING:
			n, err := take(1)
			if err != nil {
				return "", err
			}
			v, err := take(int(n[0]))
			if err != nil {
				return "", err
			}
			operand = fmt.Sprintf(" %q", v)
		case compiler.PRIORITY, compiler.LABEL, compiler.JUMP_IF_TRUE, compiler.JUMP_IF_FALSE:
			v, err := take(4)
			if err != nil {
				return "", err
			}
			number := binary.LittleEndian.Uint32(v)
			operand = fmt.Sprintf(" %d", number)
			if op == compiler.JUMP_IF_TRUE || op == compiler.JUMP_IF_FALSE {
				operand += fmt.Sprintf(" -> %d", uint64(offset)+uint64(number))
			}
		case compiler.LOAD_CONST_FLOAT, compiler.ACTION_VALUE_FLOAT:
			v, err := take(8)
			if err != nil {
				return "", err
			}
			operand = fmt.Sprintf(" %g", math.Float64frombits(binary.LittleEndian.Uint64(v)))
		case compiler.LOAD_CONST_BOOL, compiler.ACTION_VALUE_BOOL:
			v, err := take(1)
			if err != nil {
				return "", err
			}
			operand = fmt.Sprintf(" %t", v[0] != 0)
		case compiler.EQ_FLOAT, compiler.NEQ_FLOAT, compiler.LT_FLOAT, compiler.LTE_FLOAT, compiler.GT_FLOAT, compiler.GTE_FLOAT, compiler.EQ_STRING, compiler.NEQ_STRING, compiler.CONTAINS_STRING, compiler.NOT_CONTAINS_STRING, compiler.EQ_BOOL, compiler.NEQ_BOOL, compiler.RULE_END, compiler.ACTION_START, compiler.ACTION_END:
		default:
			return "", fmt.Errorf("unsupported opcode %d at %d", op, start)
		}
		fmt.Fprintf(&out, "%04d %s%s\n", start, op.String(), operand)
	}
	out.WriteString("execution-index\n")
	for _, r := range b.RuleExecIndex {
		fmt.Fprintf(&out, "%s offset=%d priority=%d\n", r.RuleName, r.ByteOffset, r.Priority)
	}
	keys := []string{}
	for key := range b.FactRuleLookupIndex {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out.WriteString("fact-index\n")
	for _, key := range keys {
		fmt.Fprintf(&out, "%s: %s\n", key, strings.Join(b.FactRuleLookupIndex[key], ","))
	}
	return out.String(), nil
}
func TestDisassemblyGoldens(t *testing.T) {
	quiet(t)
	for _, s := range loadScenarios(t)[:3] {
		t.Run(s.Name, func(t *testing.T) {
			parsed, err := compiler.Parse(s.Rules)
			if err != nil {
				t.Fatal(err)
			}
			b, err := compiler.GenerateBytecode(parsed)
			if err != nil {
				t.Fatal(err)
			}
			got, err := disassemble(b)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", s.Name+".disasm")
			if os.Getenv("REX_UPDATE_GOLDENS") == "1" {
				if err = os.WriteFile(path, []byte(got), 0644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != got {
				t.Fatalf("disassembly changed; review contract before updating %s\n%s", path, got)
			}
			again, err := compiler.GenerateBytecode(parsed)
			if err != nil {
				t.Fatal(err)
			}
			repeat, err := disassemble(again)
			if err != nil || repeat != got {
				t.Fatal("nondeterministic disassembly", err)
			}
		})
	}
}
func TestDisassemblyRejectsTruncatedOperands(t *testing.T) {
	for _, op := range []compiler.Opcode{compiler.RULE_START, compiler.LOAD_CONST_FLOAT, compiler.PRIORITY, compiler.LOAD_CONST_BOOL} {
		if _, err := disassemble(compiler.BytecodeFile{Instructions: []byte{byte(op)}}); err == nil {
			t.Fatalf("accepted truncated %s", op)
		}
	}
}
