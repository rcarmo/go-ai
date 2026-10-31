package google

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source: api/google-vertex.ts createApiKeyClient/buildHttpOptions and the
// nine-case API-key-resolution suite at 81f3cd41. API-key clients omit project
// and location; locked SDK2.21.0 uses the Express collection endpoint.
func TestMainVertexAPIKeyCollectionURLs(t *testing.T) {
	for _, tc := range []struct {
		name, base, want string
		opts             *goai.StreamOptions
	}{
		{name: "default", want: "https://aiplatform.googleapis.com/v1"},
		{name: "blank", base: " \t ", want: "https://aiplatform.googleapis.com/v1"},
		{name: "generated placeholder", base: "https://{location}-aiplatform.googleapis.com", want: "https://aiplatform.googleapis.com/v1"},
		{name: "custom root", base: "https://proxy.example.com", want: "https://proxy.example.com/v1"},
		{name: "custom v1", base: "https://proxy.example.com/v1", want: "https://proxy.example.com/v1"},
		{name: "custom beta", base: "https://proxy.example.com/v1beta2/", want: "https://proxy.example.com/v1beta2"},
		{name: "custom collection", base: "https://proxy.example.com/v1/projects/test-project/locations/global", want: "https://proxy.example.com/v1/projects/test-project/locations/global"},
		{name: "project hints ignored", want: "https://aiplatform.googleapis.com/v1", opts: &goai.StreamOptions{Project: "unused", Location: "us-central1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &goai.Model{ID: "gemini-3-flash-preview", Api: goai.ApiGoogleVertex, Provider: goai.ProviderGoogleVertex, BaseURL: tc.base}
			opts := tc.opts
			if opts == nil {
				opts = &goai.StreamOptions{}
			}
			copyOpts := *opts
			copyOpts.Env = goai.ProviderEnv{"GOOGLE_CLOUD_PROJECT": "", "GCLOUD_PROJECT": "", "GOOGLE_CLOUD_LOCATION": ""}
			got, err := buildStreamURL(model, "vertex-test-key", &copyOpts)
			want := tc.want + "/publishers/google/models/gemini-3-flash-preview:streamGenerateContent?alt=sse&key=vertex-test-key"
			if err != nil || got != want {
				t.Errorf("URL=%q err=%v want=%q", got, err, want)
			}
		})
	}
}

func TestMainVertexAPIKeyHTTPResolution(t *testing.T) {
	for _, mode := range []string{"option", "model", "environment"} {
		t.Run(mode, func(t *testing.T) {
			requests := make(chan *http.Request, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.Clone(r.Context())
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: %s\n\n", mainGoogleStopChunk("STOP", false))
			}))
			defer server.Close()
			model := &goai.Model{ID: "gemini-3-flash-preview", Api: goai.ApiGoogleVertex, Provider: goai.ProviderGoogleVertex, BaseURL: server.URL}
			opts := &goai.StreamOptions{Headers: map[string]string{"X-Vertex-Source": mode, "User-Agent": "vertex-explicit-agent"}, Env: goai.ProviderEnv{"GOOGLE_CLOUD_PROJECT": "", "GCLOUD_PROJECT": "", "GOOGLE_CLOUD_LOCATION": "", "GOOGLE_CLOUD_API_KEY": "env-vertex-key"}}
			switch mode {
			case "option":
				opts.APIKey = "option-vertex-key"
			case "model":
				model.APIKey = "model-vertex-key"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var message *goai.Message
			for event := range goai.Stream(ctx, model, &goai.Context{Messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "hello"}}}}}, opts) {
				switch e := event.(type) {
				case *goai.ErrorEvent:
					t.Fatalf("stream: %v", e.Err)
				case *goai.DoneEvent:
					message = e.Message
				}
			}
			if message == nil {
				t.Fatal("missing done")
			}
			var request *http.Request
			select {
			case request = <-requests:
			default:
				t.Fatal("missing HTTP request")
			}
			received := request.Header
			wantKey := map[string]string{"option": "option-vertex-key", "model": "model-vertex-key", "environment": "env-vertex-key"}[mode]
			if request.URL.Path != "/v1/publishers/google/models/gemini-3-flash-preview:streamGenerateContent" || request.URL.Query().Get("key") != wantKey {
				t.Errorf("request path=%q key selection mismatch", request.URL.Path)
			}
			if message.StopReason != goai.StopReasonStop {
				t.Fatalf("unexpected terminal: %s %s", message.StopReason, message.ErrorMessage)
			}
			if received.Get("User-Agent") != "vertex-explicit-agent" || received.Get("X-Vertex-Source") != mode {
				t.Errorf("headers differ")
			}
		})
	}
}
