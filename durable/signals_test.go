package durable

import "testing"

// Official v1.6.0 harness-signals.test.ts:84, defineSignal.
func TestSourceV160DefineSignal(t *testing.T) {
	str := JSON{"type": "string"}
	obj := func(props JSON) JSON { return JSON{"type": "object", "properties": props} }
	for _, tc := range []struct {
		definition SignalDefinition
		message    string
	}{
		{SignalDefinition{Name: "a/b", Version: 1, Schema: str}, `Signal name "a/b" must be non-empty, without "/"`},
		{SignalDefinition{Name: "a", Version: 0, Schema: str}, "Signal a version must be a positive integer"},
		{SignalDefinition{Name: "a", Version: 1, Schema: obj(JSON{"n": JSON{"type": "bigint"}})}, "Signal a schema admits non-JSON values (bigint)"},
		{SignalDefinition{Name: "a", Version: 1, Schema: obj(JSON{"b": JSON{"type": "bytes"}})}, "Signal a schema admits non-JSON values (bytes)"},
		{SignalDefinition{Name: "a", Version: 1, Schema: JSON{"type": "object", "dependencies": JSON{"a": []any{"b"}, "b": obj(JSON{"n": JSON{"type": "bigint"}})}}}, "Signal a schema admits non-JSON values (bigint)"},
	} {
		if token, err := DefineSignal(tc.definition); err == nil || err.Error() != tc.message || token != nil {
			t.Errorf("DefineSignal(%+v) = %v, %v; want %q", tc.definition, token, err, tc.message)
		}
	}
	annotation := obj(JSON{"type": str})
	annotation["examples"] = []any{JSON{"type": "bigint"}}
	if _, err := DefineSignal(SignalDefinition{Name: "a", Version: 1, Schema: annotation}); err != nil {
		t.Fatalf("annotations are not schema nodes: %v", err)
	}
	answer, err := DefineSignal(SignalDefinition{Name: "test.answer", Version: 1, Schema: obj(JSON{"text": str})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := answer.Key(""); err == nil || err.Error() != "Signal test.answer key must be non-empty" {
		t.Fatalf("empty signal key: %v", err)
	}
	if key, err := answer.Key("k"); err != nil || key != (SignalKey{Signal: "test.answer", Key: "k"}) {
		t.Fatalf("signal key = %+v, %v", key, err)
	}
}
