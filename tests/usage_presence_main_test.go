package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestUsagePresenceMainJSON(t *testing.T) {
	for _, test := range []struct {
		name, input  string
		long, reason bool
	}{{"absent", `{}`, false, false}, {"zero", `{"cacheWrite1h":0,"reasoning":0}`, true, true}, {"positive", `{"cacheWrite1h":3,"reasoning":4}`, true, true}, {"null", `{"cacheWrite1h":null,"reasoning":null}`, false, false}} {
		t.Run(test.name, func(t *testing.T) {
			var usage goai.Usage
			if err := json.Unmarshal([]byte(test.input), &usage); err != nil {
				t.Fatal(err)
			}
			if usage.CacheWrite1hSet != test.long || usage.ReasoningSet != test.reason {
				t.Fatalf("presence=%+v", usage)
			}
			wire, err := json.Marshal(usage)
			if err != nil {
				t.Fatal(err)
			}
			var obj map[string]any
			if err = json.Unmarshal(wire, &obj); err != nil {
				t.Fatal(err)
			}
			_, long := obj["cacheWrite1h"]
			_, reason := obj["reasoning"]
			if long != test.long || reason != test.reason {
				t.Fatalf("wire fields=%#v", obj)
			}
			var again goai.Usage
			if err = json.Unmarshal(wire, &again); err != nil {
				t.Fatal(err)
			}
			if again.CacheWrite1h != usage.CacheWrite1h || again.Reasoning != usage.Reasoning || again.CacheWrite1hSet != usage.CacheWrite1hSet || again.ReasoningSet != usage.ReasoningSet {
				t.Fatalf("round-trip=%+v", again)
			}
		})
	}
	for _, usage := range []goai.Usage{{CacheWrite1hSet: true, ReasoningSet: true}, {CacheWrite1h: 1, Reasoning: 2}} {
		wire, err := json.Marshal(usage)
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]any
		if err = json.Unmarshal(wire, &obj); err != nil {
			t.Fatal(err)
		}
		if _, ok := obj["reasoning"]; !ok {
			t.Fatal("caller value lost")
		}
		if _, ok := obj["cacheWrite1h"]; !ok {
			t.Fatal("caller value lost")
		}
	}
}

func TestUsagePresenceMainPublicCompatibility(t *testing.T) {
	var usage goai.Usage
	for _, data := range []string{`{"reasoning":0,"extra":1}`, `{"cost":{"total":1,"extra":1}}`} {
		if err := json.Unmarshal([]byte(data), &usage); err != nil {
			t.Fatal(err)
		}
		if !usage.HasUnknownJSONFields() {
			t.Fatal("strict adapter lost unknown-field evidence")
		}
	}
	if err := json.Unmarshal([]byte(`{"input":7,"reasoning":2}`), &usage); err != nil {
		t.Fatal(err)
	}
	if usage.HasUnknownJSONFields() {
		t.Fatal("stale unknown marker")
	}
	if err := json.Unmarshal([]byte(`{}`), &usage); err != nil {
		t.Fatal(err)
	}
	if usage.Input != 7 || usage.Reasoning != 2 || !usage.ReasoningSet {
		t.Fatal("omitted update lost prior fields")
	}
	if err := json.Unmarshal([]byte(`null`), &usage); err != nil || usage.Input != 7 {
		t.Fatal("null changed struct", err)
	}
	for _, data := range []string{`{"reasoning":"zero"}`, `{"cost":{"extra":1},"reasoning":"bad"}`, `[]`, `{"reasoning":0} {}`} {
		if err := json.Unmarshal([]byte(data), &usage); err == nil {
			t.Fatal("invalid usage accepted", data)
		}
	}
}
