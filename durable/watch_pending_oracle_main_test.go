package durable

import (
	"encoding/json"
	"os"
	"testing"
)

// Fixtures retain the inputs and explicit golden/count assertions of the pinned
// PendingOps adversarial tests. Native codec/safety checks are supplementary.
func TestMainPendingUpstreamAdversarialOracles(t *testing.T) {
	data, err := os.ReadFile("testdata/upstream-pending-81f3cd41.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Revision string `json:"revision"`
		Source   string `json:"source"`
		Cases    []struct {
			Name      string      `json:"name"`
			Initial   JSON        `json:"initial"`
			Ops       []Operation `json:"ops"`
			WantSame  bool        `json:"wantSame"`
			WantCount int         `json:"wantCount"`
			WantOps   []Operation `json:"wantOps"`
			WantValue JSON        `json:"wantValue"`
		} `json:"cases"`
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Revision != "81f3cd412758ebdd92324c7d63b669a189408d0a" || len(fixture.Cases) != 17 {
		t.Fatal("wrong oracle corpus")
	}
	limits := DefaultLimits()
	for _, c := range fixture.Cases {
		t.Run(c.Name, func(t *testing.T) {
			ops, err := ownOperations(c.Ops, limits)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := ownOperations(ops, limits)
			if err != nil {
				t.Fatal(err)
			}
			batches := make([][]Operation, len(ops))
			for i, op := range ops {
				batches[i] = []Operation{op}
			}
			folded, overflow, err := foldWatchOperations(batches, limits)
			if err != nil || overflow {
				t.Fatal("unexpected folding refusal", overflow, err)
			}
			if c.WantSame {
				assertConversationOperations(t, folded, c.Ops, limits)
			}
			if c.WantOps != nil {
				assertConversationOperations(t, folded, c.WantOps, limits)
			}
			if c.WantCount > 0 && len(folded) != c.WantCount {
				t.Fatalf("source count: got %d want %d", len(folded), c.WantCount)
			}
			expected, err := ApplyOperations(c.Initial, ops, limits)
			if err != nil || !equalJSONValue(expected, c.WantValue) {
				t.Fatal("native input replay differs from upstream applyImmutable", expected, c.WantValue, err)
			}
			actual, err := ApplyOperations(c.Initial, folded, limits)
			if err != nil || !equalJSONValue(actual, c.WantValue) {
				t.Fatal("source replica assertion", actual, expected, err)
			}
			assertConversationOperations(t, ops, saved, limits)
			// The source test additionally replays its own wire codec. This native
			// round trip checks the Go DTO contract, not Chord wire-format equivalence.
			encoded, err := encodeBounded(JSON{"ops": folded}, limits, limits.MaxRecordBytes)
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				Ops []Operation `json:"ops"`
			}
			if err = decodeStrict(encoded, limits, limits.MaxRecordBytes, &decoded); err != nil {
				t.Fatal(err)
			}
			replay, err := ApplyOperations(c.Initial, decoded.Ops, limits)
			if err != nil || !equalJSONValue(replay, c.WantValue) {
				t.Fatal("native wire replay", err)
			}
		})
	}
}
