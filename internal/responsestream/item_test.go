package responsestream

import "testing"

func TestFinalItemProjection(t *testing.T) {
	for _, tc := range []struct {
		raw, text, thinking string
		present             bool
	}{
		{`{"type":"message","content":[{"type":"output_text","text":"a"},{"type":"refusal","refusal":"b"}]}`, "ab", "", true},
		{`{"type":"message","content":[]}`, "", "", true},
		{`{"type":"message"}`, "", "", false},
		{`{"type":"reasoning","summary":[{"text":"s"}],"content":[{"text":"c"}]}`, "", "s", false},
		{`{"type":"reasoning","summary":[],"content":[{"text":"a"},{"text":"b"}]}`, "", "a\n\nb", false},
	} {
		text, thinking, present := FinalText([]byte(tc.raw))
		if text != tc.text || thinking != tc.thinking || present != tc.present {
			t.Fatal(tc.raw, text, thinking, present)
		}
	}
	if !MatchesItem([]byte(`{}`), "function_call") || !MatchesItem([]byte(`{"type":"function_call"}`), "function_call") || MatchesItem([]byte(`{"type":"message"}`), "function_call") || MatchesItem([]byte(`[]`), "function_call") {
		t.Fatal("completion kind validation")
	}
}
