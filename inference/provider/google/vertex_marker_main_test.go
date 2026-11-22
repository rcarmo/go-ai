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

// Reference: actual resolveApiKey/isPlaceholderApiKey helpers at 81f3cd41,
// transpiled/executed with Bun. Source helper reads options only; it does not
// fall through from an explicit placeholder to a process environment key.
func TestMainVertexMarkerURLClassification(t *testing.T) {
	for _, tc := range []struct{ name, input, key string }{
		{"open angle", "<", "<"}, {"unfinished", "<token", "<token"}, {"empty angle", "<>", "<>"}, {"inner close", "<a>b>", "<a>b>"},
		{"authenticated", "<authenticated>", ""}, {"credential marker", "gcp-vertex-credentials", ""}, {"trim marker", "  <authenticated>\t", ""},
		{"trim real", " \t real-key \r\n", "real-key"}, {"trim BOM", "\ufeffreal-key\ufeff", "real-key"}, {"retain NEL", "\u0085real-key\u0085", "\u0085real-key\u0085"},
		{"multiline marker", "<a\nb>", ""}, {"blank", "   ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &goai.Model{ID: "gemini", Api: goai.ApiGoogleVertex, Provider: goai.ProviderGoogleVertex}
			opts := &goai.StreamOptions{APIKey: tc.input, Project: "test-project", Location: "global", Env: goai.ProviderEnv{
				"GOOGLE_CLOUD_API_KEY": "", "GOOGLE_APPLICATION_CREDENTIALS": "", "HOME": "", "APPDATA": "",
			}}
			resolved := resolveVertexAPIKey(model, opts)
			key := resolved
			if isVertexADCMarker(key) {
				key = ""
			}
			if key != tc.key {
				t.Error("key classification/normalisation differs from source")
			}
			got, err := buildStreamURL(model, resolved, opts)
			base := "https://aiplatform.googleapis.com/v1"
			if tc.key == "" {
				base += "/projects/test-project/locations/global"
			}
			want := base + "/publishers/google/models/gemini:streamGenerateContent?alt=sse"
			if err != nil || got != want {
				t.Errorf("Vertex URL = %q, %v; want %q", got, err, want)
			}
		})
	}
}

func TestMainVertexMarkerHTTPResolution(t *testing.T) {
	for _, tc := range []struct {
		name, option, model, env, want string
		adc, alias                     bool
	}{
		{name: "trim option", option: "  option-key\t", env: "ignored-key", want: "option-key"},
		{name: "unfinished option", option: "<token", env: "ignored-key", want: "<token"},
		{name: "empty angle option", option: "<>", env: "ignored-key", want: "<>"},
		{name: "trim model", model: "\ufeffmodel-key\ufeff", env: "ignored-key", want: "model-key"},
		{name: "trim environment", env: " \t env-key \n", want: "env-key"},
		{name: "trim placeholder blocks env", option: "  <authenticated> \t", env: "must-not-use", adc: true},
		{name: "placeholder blocks env", option: "<authenticated>", env: "must-not-use", adc: true},
		{name: "environment placeholder", env: "<authenticated>", adc: true},
		{name: "alias isolation", env: "  alias-key ", want: "alias-key", alias: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan *http.Request, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.Clone(r.Context())
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: %s\n\n", mainGoogleStopChunk("STOP", false))
			}))
			defer server.Close()
			model := &goai.Model{ID: "gemini", Api: goai.ApiGoogleVertex, Provider: goai.ProviderGoogleVertex, BaseURL: server.URL, APIKey: tc.model}
			opts := &goai.StreamOptions{APIKey: tc.option, Env: goai.ProviderEnv{"HOME": "", "APPDATA": "", "GOOGLE_APPLICATION_CREDENTIALS": "", "GOOGLE_CLOUD_API_KEY": tc.env, "GOOGLE_CLOUD_PROJECT": "", "GCLOUD_PROJECT": "", "GOOGLE_CLOUD_LOCATION": ""}}
			if tc.alias {
				model.Provider = goai.Provider("vertex-marker-alias")
				opts.Env["GOOGLE_CLOUD_API_KEY"] = "must-not-use-global"
				opts.Env["VERTEX_MARKER_ALIAS_API_KEY"] = tc.env
			}
			if tc.adc {
				opts.Project = "test-project"
				opts.Location = "global"
				opts.Headers = map[string]string{"Authorization": "Bearer caller-test-token"}
			}
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
				if done != 1 || request.URL.Query().Has("key") || request.Header.Get("X-Goog-Api-Key") != tc.want || (len(request.Header.Values("X-Goog-Api-Key")) == 0) != tc.adc {
					t.Errorf("key selection/mode differs; done=%d", done)
				}
				if tc.adc && request.Header.Get("Authorization") != "Bearer caller-test-token" {
					t.Error("caller auth changed")
				}
			default:
				t.Fatal("missing HTTP request")
			}
			if opts.APIKey != tc.option || model.APIKey != tc.model {
				t.Error("key resolution mutated caller inputs")
			}
		})
	}
}
