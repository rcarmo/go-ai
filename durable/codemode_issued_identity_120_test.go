package durable

import (
	"strings"
	"testing"
)

// An issued-but-undelivered call has no delivery prefix to protect its role.
// Replacement must freeze Global independently of later delivery validation.
func TestCodemode120IssuedPrefixFreezesGlobalRole(t *testing.T) {
	l := DefaultLimits()
	before, err := dtoObject(codemodeJournalState{Issued: []CodemodeCall{{ID: 1, Name: "searchTools", Arguments: JSON{"query": "leaf"}}}, Deliveries: []CodemodeDelivery{}}, l)
	if err != nil {
		t.Fatal(err)
	}
	after, err := copyObject(before, l)
	if err != nil {
		t.Fatal(err)
	}
	after["issued"].([]any)[0].(map[string]any)["global"] = true
	doc := Document{Kind: codemodeJournalKind, Value: before}
	if err = validateCodemodeReplacement(doc, Document{Kind: codemodeJournalKind, Value: after}, l); err == nil || !strings.Contains(err.Error(), "issued prefix changed") {
		t.Fatalf("issued role mutation accepted: %v", err)
	}
	if err = validateCodemodeReplacement(doc, doc, l); err != nil {
		t.Fatal("unchanged prefix rejected", err)
	}
}
