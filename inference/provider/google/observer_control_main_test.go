package google

import (
	"errors"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source-derived: both adapters await the hook before processing a chunk,
// and catch its rejection with the already accumulated output at 81f3cd41.
func TestMainSourceGoogleObserverFailure(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		t.Run(string(api), func(t *testing.T) {
			calls := 0
			chunks := []string{`{"responseId":"first","candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`, `{"responseId":"unprocessed","candidates":[{"finishReason":"STOP"}]}`, `{"candidates":[{"content":{"parts":[{"text":"unreached"}]}}]}`}
			message, _ := captureMainGoogleStream(t, api, chunks, func(opts *goai.StreamOptions, _ *goai.Model) {
				opts.OnProviderStreamEvent = func(any, *goai.Model) error {
					calls++
					if calls == 2 {
						return errors.New("observer rejected")
					}
					return nil
				}
			})
			if calls != 2 || message.StopReason != goai.StopReasonError || message.ErrorMessage != "observer rejected" || message.ResponseID != "first" || len(message.Content) != 1 || message.Content[0].Text != "hello" {
				t.Fatalf("calls=%d message=%#v", calls, message)
			}
		})
	}
}

// Native ownership/typed-authority regression. Upstream SDK chunks are shared
// mutable objects; this detached projection is an explicit unresolved difference.
func TestNativeGoogleObserverProjectionIsolation(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		t.Run(string(api), func(t *testing.T) {
			calls := 0
			message, _ := captureMainGoogleStream(t, api, []string{`{"responseId":"original","unknown":{"retained":true},"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"hello"}]}}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1,"totalTokenCount":3}}`}, func(opts *goai.StreamOptions, _ *goai.Model) {
				opts.OnProviderStreamEvent = func(value any, _ *goai.Model) error {
					calls++
					object, ok := value.(map[string]any)
					if !ok {
						return errors.New("observer projection is not an object")
					}
					unknown, ok := object["unknown"].(map[string]any)
					if !ok || unknown["retained"] != true {
						return errors.New("unknown field lost")
					}
					object["candidates"] = "wrong type"
					object["responseId"] = "rewritten"
					object["usageMetadata"] = nil
					return nil
				}
			})
			if calls != 1 || message.StopReason != goai.StopReasonStop || message.ResponseID != "original" || len(message.Content) != 1 || message.Content[0].Text != "hello" || message.Usage.Input != 2 || message.Usage.Output != 1 || message.Usage.TotalTokens != 3 {
				t.Fatalf("calls=%d message=%#v", calls, message)
			}
		})
	}
}
