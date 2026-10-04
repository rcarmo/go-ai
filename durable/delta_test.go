package durable

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"
)

type deltaOracleCase struct {
	Name   string      `json:"name"`
	Input  any         `json:"input"`
	Ops    []Operation `json:"ops"`
	Output any         `json:"output"`
	After  any         `json:"after"`
}
type deltaOracle struct {
	SourceCommit   string            `json:"sourceCommit"`
	Valid          []deltaOracleCase `json:"valid"`
	Invalid        []deltaOracleCase `json:"invalid"`
	NativeRejected []deltaOracleCase `json:"nativeRejected"`
	Diffs          []deltaOracleCase `json:"diffs"`
	TrackerCases   []deltaOracleCase `json:"trackerCases"`
	Replay         struct {
		Input   any           `json:"input"`
		Batches [][]Operation `json:"batches"`
		Output  any           `json:"output"`
	} `json:"replay"`
}

func readDeltaOracle(t *testing.T) deltaOracle {
	t.Helper()
	data, err := os.ReadFile("testdata/delta-oracle.json")
	if err != nil {
		t.Fatal(err)
	}
	var result deltaOracle
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err = d.Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.SourceCommit != "a13d35a742c6ef8462812a28fbe1d8c8b7431c32" || len(result.Valid) != 80 || len(result.Invalid) != 43 || len(result.NativeRejected) != 3 {
		t.Fatal("fixture provenance/inventory")
	}
	return result
}
func TestDeltaPinnedIndependentOracle(t *testing.T) {
	oracle := readDeltaOracle(t)
	for _, c := range oracle.Valid {
		t.Run(c.Name, func(t *testing.T) {
			before, err := ownJSONValue(c.Input, DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			got, err := ApplyOperations(c.Input, c.Ops, DefaultLimits())
			if err != nil || !equalDeltaJSON(got, c.Output) {
				t.Fatalf("independent result mismatch: %v", err)
			}
			if !equalDeltaJSON(c.Input, before) {
				t.Fatal("caller input mutated")
			}
		})
	}
	for _, c := range oracle.Invalid {
		t.Run("reject-"+c.Name, func(t *testing.T) {
			if _, err := ApplyOperations(c.Input, c.Ops, DefaultLimits()); err == nil {
				t.Fatal("malformed decoded operation accepted")
			}
		})
	}
	for _, c := range oracle.NativeRejected {
		t.Run("strict-unicode-"+c.Name, func(t *testing.T) {
			// JS accepted an unpaired surrogate; native strict JSON deliberately
			// rejects that result rather than repairing/changing string offsets.
			var rejected *StorageRejected
			if _, err := ApplyOperations(c.Input, c.Ops, DefaultLimits()); !errors.As(err, &rejected) {
				t.Fatal("surrogate split did not reject", err)
			}
		})
	}
}
func TestDeltaStructuralDiffAndBatches(t *testing.T) {
	oracle := readDeltaOracle(t)
	for _, c := range oracle.Diffs {
		t.Run(c.Name, func(t *testing.T) {
			ops, err := diffOperations(c.Input, c.After, DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			got, err := ApplyOperations(c.Input, ops, DefaultLimits())
			if err != nil || !equalDeltaJSON(got, c.Output) {
				t.Fatal("native noncanonical diff replay", err)
			}
			if c.Name == "deep-equal" && len(ops) != 0 {
				t.Fatal("deep no-op not normalised")
			}
		})
	}
	value := oracle.Replay.Input
	for _, batch := range oracle.Replay.Batches {
		var err error
		value, err = ApplyOperations(value, batch, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
	}
	if !equalDeltaJSON(value, oracle.Replay.Output) {
		t.Fatal("batch result mismatch")
	}
	for _, c := range oracle.TrackerCases {
		got, err := ApplyOperations(c.Input, c.Ops, DefaultLimits())
		if err != nil || !equalDeltaJSON(got, c.Output) {
			t.Fatal("tracker replay", err)
		}
		if c.Name == "push-pop-empty" && len(c.Ops) != 0 {
			t.Fatal("reference empty-batch contract")
		}
		if c.Name == "shift-unshift-structural" && len(c.Ops) == 0 {
			t.Fatal("reference structural intent contract")
		}
	}
}
func TestDeltaPerPlacementOwnershipAndAtomicFailure(t *testing.T) {
	payload := map[string]any{"xs": []any{1}}
	ops := []Operation{{"s", []any{"a"}, payload}, {"s", []any{"b"}, payload}}
	input := map[string]any{"old": []any{true}}
	got, err := ApplyOperations(input, ops, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	root := got.(map[string]any)
	root["a"].(map[string]any)["xs"].([]any)[0] = 99
	if root["b"].(map[string]any)["xs"].([]any)[0].(json.Number) != "1" || payload["xs"].([]any)[0] != 1 {
		t.Fatal("placement alias")
	}
	payload["xs"].([]any)[0] = 3
	ops[0][1].([]any)[0] = "changed"
	if root["b"].(map[string]any)["xs"].([]any)[0].(json.Number) != "1" {
		t.Fatal("retained input authority")
	}
	if value, err := ApplyOperations(input, []Operation{{"s", []any{"old"}, false}, {"s", []any{"missing", "x"}, 1}}, DefaultLimits()); err == nil || value != nil {
		t.Fatal("partial batch returned", err)
	}
	if input["old"].([]any)[0] != true {
		t.Fatal("failed batch mutated base")
	}
	cycle := map[string]any{}
	cycle["self"] = cycle
	if _, err = ApplyOperations(input, []Operation{{"s", []any{"x"}, cycle}}, DefaultLimits()); err == nil {
		t.Fatal("cycle")
	}
}
func TestDeltaStrictBudgetsAndExactNumbers(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxDocumentBytes = 512
	limits.MaxRecordBytes = 2048
	limits.MaxStringBytes = 1024
	value := map[string]any{"s": ""}
	if _, err := ApplyOperations(value, []Operation{{"a", []any{"s"}, string(bytes.Repeat([]byte{'x'}, 504))}}, limits); err != nil {
		t.Fatal("exact512", err)
	}
	if _, err := ApplyOperations(value, []Operation{{"a", []any{"s"}, string(bytes.Repeat([]byte{'x'}, 505))}, {"t", []any{"s"}, 505}}, limits); err == nil {
		t.Fatal("oversized intermediate hidden by truncation")
	}
	for _, v := range []any{math.NaN(), math.Inf(1), string([]byte{0xff}), struct{ N int }{1}, func() {}} {
		if _, err := ApplyOperations(value, []Operation{{"s", []any{"x"}, v}}, limits); err == nil {
			t.Fatal("non-strict payload")
		}
	}
	for _, n := range []json.Number{"9007199254740992", "1e999999999", "1e-999999999", "0.1", "-1"} {
		if _, err := ApplyOperations(value, []Operation{{"t", []any{"s"}, n}}, limits); err == nil {
			t.Fatal("invalid index", n)
		}
	}
	for _, n := range []json.Number{"2.00", "2e0", "200e-2", "-0"} {
		if _, err := ApplyOperations(map[string]any{"s": "abc"}, []Operation{{"t", []any{"s"}, n}}, limits); err != nil {
			t.Fatal("exact integer spelling", n, err)
		}
	}
	if !equalDeltaJSON(json.Number("1e999999999"), json.Number("10e999999998")) || equalDeltaJSON(json.Number("1"), json.Number("1.0000000000000000001")) {
		t.Fatal("bounded exact numeric equality")
	}
	limits = DefaultLimits()
	limits.MaxMembers = 8
	if _, err := ApplyOperations(map[string]any{"xs": []any{1, 2, 3, 4, 5, 6, 7, 8}}, []Operation{{"p", []any{"xs"}, 8, 0, []any{9}}, {"p", []any{"xs"}, 0, 1, []any{}}}, limits); err == nil {
		t.Fatal("member intermediate growth")
	}
	limits = DefaultLimits()
	limits.MaxNodes = 20
	if _, err := ApplyOperations(map[string]any{}, []Operation{{"s", []any{"x"}, []any{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}}}, limits); err == nil {
		t.Fatal("aggregate node budget")
	}
}

func TestDeltaDepthStringOperationAndEncodedBudgets(t *testing.T) {
	for _, name := range []string{"depth", "string", "operations", "encoded"} {
		t.Run(name, func(t *testing.T) {
			l := DefaultLimits()
			value := map[string]any{}
			ops := []Operation{}
			switch name {
			case "depth":
				l.MaxDepth = 4
				ops = []Operation{{"s", []any{"x"}, map[string]any{"a": map[string]any{"b": map[string]any{"c": map[string]any{}}}}}}
			case "string":
				l.MaxStringBytes = 512
				ops = []Operation{{"s", []any{"x"}, string(bytes.Repeat([]byte{'x'}, 513))}}
			case "operations":
				ops = make([]Operation, 4097)
				for i := range ops {
					ops[i] = Operation{"r", nil}
				}
			case "encoded":
				l.MaxRecordBytes = 1024
				l.MaxDocumentBytes = 512
				l.MaxStringBytes = 512
				ops = []Operation{{"s", []any{"a"}, string(bytes.Repeat([]byte{'x'}, 500))}, {"s", []any{"b"}, string(bytes.Repeat([]byte{'x'}, 500))}}
			}
			if _, e := ApplyOperations(value, ops, l); e == nil {
				t.Fatal("delta budget accepted")
			}
		})
	}
}
