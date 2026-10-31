package google

// Source-derived at 81f3cd41: both adapters use responseId ||= chunk.responseId,
// update finish state per candidate, and create provider diagnostics only in
// their post-stream throw/catch (after closing active blocks).
// Native snapshots freeze publication-time state; upstream EventStream queues
// shared output references. Snapshot checks adapt that native contract and do
// not claim JavaScript consumer reference/timing equivalence.
import (
	"fmt"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestMainSourceGoogleFirstResponseID(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		for _, tc := range []struct {
			name   string
			chunks []string
			want   string
		}{
			{"retain first", []string{`{"responseId":"first","candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`, `{"responseId":"later","candidates":[{"finishReason":"STOP"}]}`}, "first"},
			{"late first metadata only", []string{`{"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`, `{"responseId":"late"}`, `{"candidates":[{"finishReason":"STOP"}]}`}, "late"},
			{"ignore later empty", []string{`{"responseId":"first","candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`, `{"responseId":"","candidates":[{"finishReason":"STOP"}]}`}, "first"},
		} {
			t.Run(string(api)+"/"+tc.name, func(t *testing.T) {
				message, _ := captureMainGoogleStream(t, api, tc.chunks, nil)
				if message.ResponseID != tc.want || message.StopReason != goai.StopReasonStop || len(message.Content) != 1 || message.Content[0].Text != "hello" {
					t.Fatalf("id=%q want %q reason=%s content=%#v", message.ResponseID, tc.want, message.StopReason, message.Content)
				}
			})
		}
	}
}

func TestMainSourceGoogleFinishDiagnosticLifetime(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		for _, tc := range []struct {
			first, last     string
			want            goai.StopReason
			raw, diagnostic string
		}{
			{"SAFETY", "STOP", goai.StopReasonStop, "STOP", ""},
			{"SAFETY", "MAX_TOKENS", goai.StopReasonLength, "MAX_TOKENS", ""},
			{"STOP", "SAFETY", goai.StopReasonError, "SAFETY", "Provider stopped with: SAFETY"},
			{"SAFETY", "MALFORMED_FUNCTION_CALL", goai.StopReasonError, "MALFORMED_FUNCTION_CALL", "Provider stopped with: MALFORMED_FUNCTION_CALL"},
			{"SAFETY", "", goai.StopReasonError, "SAFETY", "Provider stopped with: SAFETY"},
		} {
			t.Run(fmt.Sprintf("%s/%s-to-%s", api, tc.first, tc.last), func(t *testing.T) {
				second := fmt.Sprintf(`{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":%q}]}`, tc.last)
				message, _, events := captureMainGoogleStreamEvents(t, api, []string{mainGoogleStopChunk(tc.first, false), second}, nil)
				if message.StopReason != tc.want || message.RawStopReason != tc.raw || message.ErrorMessage != tc.diagnostic {
					t.Errorf("final reason=%s raw=%s diagnostic=%q want %s/%s/%q", message.StopReason, message.RawStopReason, message.ErrorMessage, tc.want, tc.raw, tc.diagnostic)
				}
				deltas, ends, errors, done := 0, 0, 0, 0
				for _, event := range events {
					var partial *goai.Message
					switch e := event.(type) {
					case *goai.TextDeltaEvent:
						deltas++
						partial = e.Partial
					case *goai.TextEndEvent:
						ends++
						partial = e.Partial
					case *goai.ErrorEvent:
						errors++
						if e.Err == nil || e.Err.Error() != tc.diagnostic {
							t.Errorf("terminal error %v want %s", e.Err, tc.diagnostic)
						}
					case *goai.DoneEvent:
						done++
					}
					if partial != nil && partial.ErrorMessage != "" {
						t.Errorf("premature diagnostic %q", partial.ErrorMessage)
					}
				}
				if deltas != 1 || ends != 1 {
					t.Errorf("text events delta=%d end=%d", deltas, ends)
				}
				if tc.want == goai.StopReasonError {
					if errors != 1 || done != 0 {
						t.Errorf("errors=%d done=%d", errors, done)
					}
				} else if errors != 0 || done != 1 {
					t.Errorf("errors=%d done=%d", errors, done)
				}
			})
		}
	}
}
