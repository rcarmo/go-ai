package responsestream

import "testing"

func TestTextSignatureMain(t *testing.T) {
	for _, tc := range []struct{ raw, id, phase string }{{"legacy_id", "legacy_id", ""}, {`{"v":1,"id":"m","phase":"commentary"}`, "m", "commentary"}, {`{"v":1,"id":"m","phase":"final_answer"}`, "m", "final_answer"}, {`{"v":1,"id":"m","phase":"unknown"}`, "m", ""}, {`{"v":1,"id":"m","phase":42}`, "m", ""}, {`{"v":1.0,"id":"m"}`, "m", ""}, {`{"id":"m"}`, `{"id":"m"}`, ""}, {`{"v":2,"id":"m"}`, `{"v":2,"id":"m"}`, ""}, {" {\"v\":1,\"id\":\"m\"}", " {\"v\":1,\"id\":\"m\"}", ""}, {"{bad", "{bad", ""}} {
		id, phase := ParseTextSignature(tc.raw)
		if id != tc.id || phase != tc.phase {
			t.Fatal(tc, id, phase)
		}
	}
	encoded := MessageTextSignature([]byte(`{"id":"m","phase":"final_answer"}`))
	id, phase := ParseTextSignature(encoded)
	if id != "m" || phase != "final_answer" || !IsFinalAnswer([]byte(`{"phase":"final_answer"}`)) {
		t.Fatal(encoded)
	}
	if IsFinalAnswer([]byte(`{"phase":"commentary"}`)) {
		t.Fatal("commentary marked final")
	}
}
