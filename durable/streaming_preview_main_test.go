package durable

import (
	"math"
	"testing"

	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func TestStreamingPreviewMainDoesNotRelaxPersistedJSON(t *testing.T) {
	// The preview adapter has different semantics from the persisted JSON domain.
	// Neither nonfinite values nor raw invalid UTF-8 gain an implicit exemption.
	for _, input := range []string{`{"n":1e400}`, `{"\ud800":true}`, `{"x":"\ud800"}`} {
		parser := jsonparse.StreamingJSONParser{}
		parser.Append(input)
		value, present := parser.Snapshot()
		if !present {
			t.Fatal("missing preview")
		}
		if _, err := encodeBounded(value, DefaultLimits(), 4096); err == nil {
			t.Fatal("preview bypassed persisted JSON contract", input)
		}
	}
	if _, err := encodeBounded(JSON{"n": math.Inf(1)}, DefaultLimits(), 4096); err == nil {
		t.Fatal("nonfinite guard weakened")
	}
	if _, err := copyObject(JSON{"key": string([]byte{0xff})}, DefaultLimits()); err == nil {
		t.Fatal("UTF-8 guard weakened")
	}
}
