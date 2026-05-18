package google

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestBuildStreamURLEscapesPathAndQuery(t *testing.T) {
	model := &goai.Model{ID: "models/custom/id", BaseURL: "https://example.test/v1beta/"}
	got := buildStreamURL(model, "a+b/c=")
	if strings.Contains(got, "v1beta//") {
		t.Fatalf("base URL was not normalized: %q", got)
	}
	if !strings.Contains(got, "/models/models%2Fcustom%2Fid:streamGenerateContent") {
		t.Fatalf("model ID was not path-escaped: %q", got)
	}
	if !strings.Contains(got, "key=a%2Bb%2Fc%3D") {
		t.Fatalf("API key was not query-escaped: %q", got)
	}
}

func TestProcessStreamHandlesMultilineSSE(t *testing.T) {
	model := &goai.Model{ID: "gemini", Provider: goai.ProviderGoogle, Api: goai.ApiGoogleGenerativeAI}
	ch := make(chan goai.Event, 16)
	processStream(strings.NewReader("data: {\"responseId\":\"resp_1\",\n"+
		"data: \"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}],\n"+
		"data: \"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":1,\"totalTokenCount\":2}}\n\n"), model, ch)
	close(ch)
	var done *goai.DoneEvent
	var sawText bool
	for ev := range ch {
		switch e := ev.(type) {
		case *goai.TextDeltaEvent:
			sawText = sawText || e.Delta == "ok"
		case *goai.DoneEvent:
			done = e
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	if !sawText || done == nil || done.Message == nil || done.Message.ResponseID != "resp_1" {
		t.Fatalf("expected multiline SSE text and done, sawText=%v done=%#v", sawText, done)
	}
}
