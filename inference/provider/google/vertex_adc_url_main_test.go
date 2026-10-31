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

// Source-derived ADC URL contracts from api/google-vertex.ts at 81f3cd41 and
// locked SDK2.21.0. These tests do not acquire ADC tokens or load credentials.
func TestMainVertexADCURLs(t *testing.T) {
	for _, tc := range []struct{ name, base, location, want string }{
		{"regional", "", "europe-west4", "https://europe-west4-aiplatform.googleapis.com/v1/projects/test-project/locations/europe-west4"},
		{"global", "", "global", "https://aiplatform.googleapis.com/v1/projects/test-project/locations/global"},
		{"multi us", "", "us", "https://aiplatform.us.rep.googleapis.com/v1/projects/test-project/locations/us"},
		{"multi eu", "", "eu", "https://aiplatform.eu.rep.googleapis.com/v1/projects/test-project/locations/eu"},
		{"generated placeholder", "https://ignored.example/{location}", "europe-west4", "https://europe-west4-aiplatform.googleapis.com/v1/projects/test-project/locations/europe-west4"},
		{"blank base", " \t ", "europe-west4", "https://europe-west4-aiplatform.googleapis.com/v1/projects/test-project/locations/europe-west4"},
		{"custom root", "https://proxy.example.com", "europe-west4", "https://proxy.example.com/v1"},
		{"custom v1", "https://proxy.example.com/v1/", "europe-west4", "https://proxy.example.com/v1"},
		{"custom beta", "https://proxy.example.com/v1beta2", "europe-west4", "https://proxy.example.com/v1beta2"},
		{"custom collection", "https://proxy.example.com/v1/projects/foreign/locations/global", "europe-west4", "https://proxy.example.com/v1/projects/foreign/locations/global"},
		{"custom global", "https://proxy.example.com", "global", "https://proxy.example.com/v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &goai.Model{ID: "gemini-3-flash-preview", Api: goai.ApiGoogleVertex, Provider: goai.ProviderGoogleVertex, BaseURL: tc.base}
			got, err := buildStreamURL(model, "<authenticated>", &goai.StreamOptions{Project: "test-project", Location: tc.location})
			want := tc.want + "/publishers/google/models/gemini-3-flash-preview:streamGenerateContent?alt=sse"
			if err != nil || got != want {
				t.Errorf("URL=%q err=%v want=%q", got, err, want)
			}
		})
	}
}

func TestMainVertexADCMissingScope(t *testing.T) {
	for _, missing := range []string{"project", "location"} {
		t.Run(missing, func(t *testing.T) {
			opts := &goai.StreamOptions{Project: "test-project", Location: "global", Env: goai.ProviderEnv{"GOOGLE_CLOUD_PROJECT": "", "GCLOUD_PROJECT": "", "GOOGLE_CLOUD_LOCATION": ""}}
			if missing == "project" {
				opts.Project = ""
			} else {
				opts.Location = ""
			}
			model := &goai.Model{ID: "gemini", Api: goai.ApiGoogleVertex, BaseURL: "https://proxy.example.com"}
			got, err := buildStreamURL(model, "<authenticated>", opts)
			if err == nil || got != "" {
				t.Errorf("missing %s admitted URL %q", missing, got)
			}
		})
	}
}

func TestMainVertexADCCallerAuthHTTP(t *testing.T) {
	// Caller-supplied synthetic Authorization verifies routing/header ownership.
	// It must not be counted as successful ADC token acquisition.
	requests := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", mainGoogleStopChunk("STOP", false))
	}))
	defer server.Close()
	model := &goai.Model{ID: "gemini-3-flash-preview", Api: goai.ApiGoogleVertex, Provider: goai.ProviderGoogleVertex, BaseURL: server.URL}
	opts := &goai.StreamOptions{APIKey: "<authenticated>", Project: "test-project", Location: "global", Headers: map[string]string{"Authorization": "Bearer caller-test-token"}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := 0
	for event := range goai.Stream(ctx, model, &goai.Context{Messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "hello"}}}}}, opts) {
		switch e := event.(type) {
		case *goai.ErrorEvent:
			t.Fatalf("stream: %v", e.Err)
		case *goai.DoneEvent:
			done++
		}
	}
	select {
	case request := <-requests:
		if request.URL.Path != "/v1/publishers/google/models/gemini-3-flash-preview:streamGenerateContent" || request.URL.Query().Has("key") || request.Header.Get("Authorization") != "Bearer caller-test-token" || done != 1 {
			t.Errorf("routing/auth changed: path=%s done=%d", request.URL.Path, done)
		}
	default:
		t.Fatal("missing request")
	}
}
