package openairesponses

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestResponsesTerminalReasonMain(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiOpenAIResponses, goai.ApiAzureOpenAIResponses} {
		for _, reason := range []string{"max_output_tokens", "content_filter", ""} {
			t.Run(string(api)+"/"+reason, func(t *testing.T) {
				raw, _ := json.Marshal(map[string]any{"type": "response.incomplete", "response": map[string]any{"status": "incomplete", "incomplete_details": map[string]any{"reason": reason}}})
				var final *goai.Message
				done, failures := 0, 0
				processStream(strings.NewReader("data: "+string(raw)+"\n\n"), &goai.Model{Api: api}, func(event goai.Event) {
					switch e := event.(type) {
					case *goai.DoneEvent:
						done++
						final = e.Message
					case *goai.ErrorEvent:
						failures++
						final = e.Error
						if e.Err == nil {
							t.Error("missing terminal error")
						}
					}
				})
				if final == nil {
					t.Fatal("missing terminal message")
				}
				want := goai.StopReasonError
				message := "Response incomplete without a provider reason"
				rawReason := "incomplete"
				if reason != "" {
					message = "Response incomplete: " + reason
					rawReason += "." + reason
				}
				if reason == "max_output_tokens" {
					want = goai.StopReasonLength
					message = ""
				}
				if want == goai.StopReasonError {
					if failures != 1 || done != 0 {
						t.Fatal("failed output delivered as done", done, failures)
					}
				} else if done != 1 || failures != 0 {
					t.Fatal(done, failures)
				}
				if final.StopReason != want || final.ErrorMessage != message || final.RawStopReason != rawReason {
					t.Fatal("incomplete reason mismatch", final)
				}
			})
		}
		t.Run(string(api)+"/unknown", func(t *testing.T) {
			done, failures := 0, 0
			processStream(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"unknown\"}}\n\n"), &goai.Model{Api: api}, func(event goai.Event) {
				switch event.(type) {
				case *goai.DoneEvent:
					done++
				case *goai.ErrorEvent:
					failures++
				}
			})
			if failures != 1 || done != 0 {
				t.Fatal("unknown status accepted", done, failures)
			}
		})
	}
}
