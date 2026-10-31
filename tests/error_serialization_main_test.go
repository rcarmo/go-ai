package goai_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestProviderErrorSerializationTextMain(t *testing.T) {
	for _, tc := range []struct{ name, value, text string }{
		{"html", "<blocked>&gone>", `{"error":"<blocked>&gone>"}`},
		{"line-separators", "before\u2028middle\u2029after", "{\"error\":\"before\u2028middle\u2029after\"}"},
		{"literal-escapes", `\u2028\u2029`, `{"error":"\\u2028\\u2029"}`},
		{"control-escapes", "\"\t\n\x00", `{"error":"\"\t\n\u0000"}`},
		{"utf8", "é😃", `{"error":"é😃"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"error": tc.value}
			message := "403: " + tc.text
			norm := goai.NormalizeProviderError(providerErr{msg: message, status: 403, hasStatus: true, body: body, hasBody: true})
			if norm.Body != tc.text || !norm.MessageCarriesBody || goai.FormatProviderError(norm) != message {
				t.Fatal("body duplicated or altered", norm)
			}
			if got := goai.SafeJSONStringify(body); got != tc.text {
				t.Fatal("serialization text", got, tc.text)
			}
			var decoded map[string]string
			if err := json.Unmarshal([]byte(norm.Body), &decoded); err != nil || decoded["error"] != tc.value {
				t.Fatal("JSON meaning changed", err, decoded)
			}
		})
	}
}

func TestErrorJSONSeparatorEscapeParityMain(t *testing.T) {
	for n := 0; n < 9; n++ {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			value := strings.Repeat("\\", n) + "\u2028" + `\u2029`
			want := `"` + strings.Repeat(`\\`, n) + "\u2028" + `\\u2029"`
			got := goai.SafeJSONStringify(value)
			if got != want {
				t.Fatal("escape parity", got, want)
			}
			var decoded string
			if err := json.Unmarshal([]byte(got), &decoded); err != nil || decoded != value {
				t.Fatal("bad separator round trip", err, decoded)
			}
		})
	}
	key := "<key>\u2028"
	body := map[string]any{key: "&\u2029"}
	want := "{\"<key>\u2028\":\"&\u2029\"}"
	if got := goai.SafeJSONStringify(body); got != want {
		t.Fatal("key escaping", got, want)
	}
}
