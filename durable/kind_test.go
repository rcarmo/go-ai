package durable

import (
	"regexp"
	"strings"
	"testing"
)

func TestKindScannerMatchesIdentifierGrammar(t *testing.T) {
	reference := regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)
	cases := []string{"", "a", "task.pi-compaction", "a_0", strings.Repeat("a", 128), strings.Repeat("a", 129), "A", "1a", "a\n", "a🙂", "a\x00", "à"}
	for first := 0; first < 256; first++ {
		cases = append(cases, string([]byte{byte(first)}))
		for second := 0; second < 256; second++ {
			cases = append(cases, string([]byte{byte(first), byte(second)}))
		}
	}
	for _, value := range cases {
		if got, want := validKind(value), reference.MatchString(value); got != want {
			t.Fatalf("identifier %q: got %v want %v", value, got, want)
		}
	}
}
func BenchmarkKindScanner(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if !validKind("task.pi.subagent-reporter") {
			b.Fatal("invalid")
		}
	}
}
