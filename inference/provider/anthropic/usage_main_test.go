package anthropic

import (
	"fmt"
	"math"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func runUsageMain(t *testing.T, start string, deltas []string, truncated bool, model *goai.Model) (*goai.Message, bool) {
	t.Helper()
	wire := fmt.Sprintf("event: message_start\ndata: {\"message\":{\"id\":\"r\",\"model\":\"test\",\"usage\":%s}}\n\n", start)
	for _, delta := range deltas {
		wire += "event: message_delta\ndata: " + delta + "\n\n"
	}
	if !truncated {
		wire += "event: message_stop\ndata: {}\n\n"
	}
	var message *goai.Message
	failed := false
	processAnthropicStream(strings.NewReader(wire), model, nil, "", func(e goai.Event) {
		switch v := e.(type) {
		case *goai.DoneEvent:
			message = v.Message
		case *goai.ErrorEvent:
			failed = true
			message = v.Error
		}
	})
	if message == nil || message.Usage == nil {
		t.Fatal("no terminal usage")
	}
	return message, failed
}

func usageModelMain() *goai.Model {
	return &goai.Model{ID: "test", Api: goai.ApiAnthropicMessages, Provider: "openrouter", Cost: goai.ModelCost{Input: 1, Output: 2, CacheRead: .1, CacheWrite: 3}}
}

func TestAnthropicMainPartialUsageAndBilledCostRawSSE(t *testing.T) {
	start := `{"input_tokens":100,"output_tokens":5,"cache_read_input_tokens":20,"cache_creation_input_tokens":30,"cache_creation":{"ephemeral_1h_input_tokens":10}}`
	full := `{"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":500,"output_tokens":100,"cache_read_input_tokens":40,"cache_creation_input_tokens":50,"cache_creation":{"ephemeral_1h_input_tokens":35},"output_tokens_details":{"thinking_tokens":20},"cost":0.1,"is_byok":true,"cost_details":{"upstream_inference_cost":0.9}}}`
	for _, test := range []struct {
		name                                           string
		deltas                                         []string
		want                                           float64
		billed                                         bool
		input, output, read, write, oneHour, reasoning int
	}{
		{"byok delta updates all fields", []string{full}, 1, true, 500, 100, 40, 50, 35, 20},
		{"null fields retain previous values", []string{full, `{"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":null,"output_tokens":null,"cache_read_input_tokens":null,"cache_creation_input_tokens":null,"cache_creation":{"ephemeral_1h_input_tokens":null},"output_tokens_details":{"thinking_tokens":null},"cost":0,"is_byok":false}}`}, 0, true, 500, 100, 40, 50, 35, 20},
		{"omitted usage keeps tokens and recalculates estimate", []string{full, `{"delta":{"stop_reason":"end_turn"}}`}, .000819, false, 500, 100, 40, 50, 35, 20},
		{"null usage keeps tokens and recalculates estimate", []string{full, `{"delta":{"stop_reason":"end_turn"},"usage":null}`}, .000819, false, 500, 100, 40, 50, 35, 20},
		{"output only retains initial cache and input", []string{`{"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`}, .000202, false, 100, 10, 20, 30, 10, 0},
		{"zero is an update", []string{full, `{"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_1h_input_tokens":0},"output_tokens_details":{"thinking_tokens":0}}}`}, 0, false, 0, 0, 0, 0, 0, 0},
		{"malformed accounting does not drop valid usage", []string{`{"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10,"cost":"0.1","is_byok":"false"}}`}, .000202, false, 100, 10, 20, 30, 10, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			message, failed := runUsageMain(t, start, test.deltas, false, usageModelMain())
			if failed {
				t.Fatalf("unexpected error: %+v", message)
			}
			u := message.Usage
			if u.Input != test.input || u.Output != test.output || u.CacheRead != test.read || u.CacheWrite != test.write || u.CacheWrite1h != test.oneHour || u.Reasoning != test.reasoning || u.TotalTokens != test.input+test.output+test.read+test.write || math.Abs(u.Cost.Total-test.want) > 1e-12 || u.Cost.TotalFromProvider == nil || *u.Cost.TotalFromProvider != test.billed {
				t.Fatalf("usage=%+v cost=%+v", u, u.Cost)
			}
		})
	}
}

func TestAnthropicMainInitialUsageSurvivesStreamFailure(t *testing.T) {
	message, failed := runUsageMain(t, `{"input_tokens":100,"output_tokens":5,"cost":99,"is_byok":false}`, nil, true, usageModelMain())
	u := message.Usage
	if !failed || u.Input != 100 || u.Output != 5 || u.TotalTokens != 105 || math.Abs(u.Cost.Total-.00011) > 1e-12 || u.Cost.TotalFromProvider == nil || *u.Cost.TotalFromProvider {
		t.Fatalf("failed=%v usage=%+v", failed, u)
	}
}

func TestAnthropicMainOtherProviderIgnoresBilledCost(t *testing.T) {
	model := usageModelMain()
	model.Provider = "custom"
	model.BaseURL = "https://example.com"
	message, failed := runUsageMain(t, `{"input_tokens":100}`, []string{`{"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5,"cost":99,"is_byok":false}}`}, false, model)
	if failed || math.Abs(message.Usage.Cost.Total-.00011) > 1e-12 || *message.Usage.Cost.TotalFromProvider {
		t.Fatal(message.Usage)
	}
	model.BaseURL = "https://openrouter.ai/api/v1"
	message, failed = runUsageMain(t, `{"input_tokens":100}`, []string{`{"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5,"cost":99,"is_byok":false}}`}, false, model)
	if failed || message.Usage.Cost.Total != 99 || !*message.Usage.Cost.TotalFromProvider {
		t.Fatal(message.Usage)
	}
}

func TestAnthropicMainOptionalZeroPresence(t *testing.T) {
	message, failed := runUsageMain(t, `{"input_tokens":1}`, []string{`{"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens_details":{"thinking_tokens":0},"cache_creation":{"ephemeral_1h_input_tokens":0}}}`, `{"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens_details":{"thinking_tokens":null}}}`}, false, usageModelMain())
	if failed || !message.Usage.ReasoningSet || !message.Usage.CacheWrite1hSet {
		t.Fatal("lost optional zero presence")
	}
	initial, failed := runUsageMain(t, `{"input_tokens":1}`, nil, true, usageModelMain())
	if !failed || !initial.Usage.CacheWrite1hSet || initial.Usage.ReasoningSet {
		t.Fatal("initial optional presence differs from upstream")
	}
}
