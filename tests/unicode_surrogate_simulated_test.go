package goai_test

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const unicodeToolResultFixture = `Test with emoji 🙈 and other characters:
- Monkey emoji: 🙈
- Thumbs up: 👍
- Heart: ❤️
- Thinking face: 🤔
- Rocket: 🚀
- Mixed text: Mario Zechner wann? Wo? Bin grad äußersr eventuninformiert 🙈
- Japanese: こんにちは
- Chinese: 你好
- Mathematical symbols: ∑∫∂√
- Special quotes: "curly" 'quotes'`

func TestUnicodeSurrogateSimulatedToolResultJSONRoundTrip(t *testing.T) {
	msg := goai.Message{Role: goai.RoleToolResult, ToolCallID: "test_1", ToolName: "test_tool", Content: []goai.ContentBlock{{Type: "text", Text: unicodeToolResultFixture}}}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatalf("invalid JSON: %s", raw)
	}
	if strings.Contains(string(raw), `\ud800`) || strings.Contains(string(raw), `\udc00`) {
		t.Fatalf("unexpected unpaired surrogate escape: %s", raw)
	}
	var out goai.Message
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.Content[0].Text; got != unicodeToolResultFixture {
		t.Fatalf("roundtrip text mismatch\n got: %q\nwant: %q", got, unicodeToolResultFixture)
	}
}

func TestUnicodeSurrogateSanitizesInvalidUTF8Bytes(t *testing.T) {
	input := string([]byte{'o', 'k', 0xed, 0xa0, 0x80, '!'})
	got := goai.SanitizeSurrogates(input)
	if !strings.Contains(got, "ok") || !strings.Contains(got, "!") {
		t.Fatalf("sanitized=%q", got)
	}
	if !json.Valid([]byte(`{"text":` + strconvQuote(got) + `}`)) {
		t.Fatalf("sanitized string not JSON-safe: %q", got)
	}
}

func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }
