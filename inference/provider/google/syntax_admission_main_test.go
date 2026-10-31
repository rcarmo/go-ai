package google

// Source-derived from the adapters at 81f3cd41 and locked @google/genai 2.21.0:
// generateContentStream awaits HttpResponse.json before yielding SDK chunks.
// Malformed JSON must reject, not be skipped in favour of a later success.
import (
	"fmt"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestMainSourceGoogleSyntaxAdmission(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		for _, tc := range []struct {
			name, bad string
			failed    bool
		}{{"unclosed object", "{", true}, {"trailing comma", `{"candidates":[],}`, true}, {"non JSON text", "not-json", true}, {"valid empty object", "{}", false}} {
			t.Run(fmt.Sprint(api)+"/"+tc.name, func(t *testing.T) {
				calls := 0
				message, _, events := captureMainGoogleStreamEvents(t, api, []string{`{"responseId":"first","candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`, tc.bad, mainGoogleStopChunk("STOP", false)}, func(opts *goai.StreamOptions, _ *goai.Model) {
					opts.OnProviderStreamEvent = func(any, *goai.Model) error { calls++; return nil }
				})
				errors, done := 0, 0
				for _, event := range events {
					switch e := event.(type) {
					case *goai.ErrorEvent:
						errors++
						if e.Err == nil {
							t.Error("missing parse diagnostic")
						}
					case *goai.DoneEvent:
						done++
					}
				}
				if len(message.Content) != 1 || message.Content[0].Text != "hello" || message.ResponseID != "first" {
					t.Errorf("partial/id changed: %#v id=%s", message.Content, message.ResponseID)
				}
				if tc.failed {
					if calls != 1 || errors != 1 || done != 0 || message.StopReason != goai.StopReasonError || message.ErrorMessage == "" {
						t.Errorf("hook=%d errors=%d done=%d reason=%s diagnostic=%q", calls, errors, done, message.StopReason, message.ErrorMessage)
					}
				} else if calls != 3 || done != 1 || errors != 0 || message.StopReason != goai.StopReasonStop {
					t.Errorf("valid control hook=%d done=%d errors=%d reason=%s", calls, done, errors, message.StopReason)
				}
			})
		}
	}
}
