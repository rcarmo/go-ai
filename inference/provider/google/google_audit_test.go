package google

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestBuildStreamURLEscapesPathAndQuery(t *testing.T) {
	model := &goai.Model{ID: "models/custom/id", BaseURL: "https://example.test/v1beta/"}
	got, err := buildStreamURL(model, "a+b/c=", nil)
	if err != nil {
		t.Fatal(err)
	}
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

func TestBuildVertexStreamURLUsesProjectAndLocationOptions(t *testing.T) {
	model := &goai.Model{ID: "gemini-2.5-pro", Api: goai.ApiGoogleVertex, Provider: goai.ProviderGoogleVertex, BaseURL: "https://{location}-aiplatform.googleapis.com"}
	got, err := buildStreamURL(model, "vertex-key", &goai.StreamOptions{Project: "proj-1", Location: "europe-west4"})
	if err != nil {
		t.Fatal(err)
	}
	want := "https://europe-west4-aiplatform.googleapis.com/v1/projects/proj-1/locations/europe-west4/publishers/google/models/gemini-2.5-pro:streamGenerateContent?alt=sse&key=vertex-key"
	if got != want {
		t.Fatalf("unexpected Vertex URL:\n got %q\nwant %q", got, want)
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
