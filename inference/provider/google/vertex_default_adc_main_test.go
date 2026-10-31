package google

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestMainVertexKnownADCFile(t *testing.T) {
	for _, name := range []string{"process home", "scoped home", "marker", "explicit wins", "missing explicit", "corrupt default", "empty home", "real key bypass", "absent default"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			processHome, scopedHome := filepath.Join(root, "process"), filepath.Join(root, "scoped")
			defaultPath := func(home string) string {
				if runtime.GOOS == "windows" {
					return filepath.Join(home, "gcloud", "application_default_credentials.json")
				}
				return filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
			}
			write := func(path, id string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(map[string]any{"type": "authorized_user", "client_id": id, "client_secret": "fixture-secret", "refresh_token": "fixture-refresh", "quota_project_id": "quota-" + id})
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(defaultPath(processHome), "process")
			write(defaultPath(scopedHome), "scoped")
			explicit := filepath.Join(root, "explicit.json")
			write(explicit, "explicit")
			t.Setenv("HOME", processHome)
			t.Setenv("APPDATA", processHome)
			t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(root, "ambient-trap.json"))
			t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", "ambient-quota")
			env := goai.ProviderEnv{"HOME": scopedHome, "APPDATA": scopedHome, "GOOGLE_APPLICATION_CREDENTIALS": "", "GOOGLE_CLOUD_QUOTA_PROJECT": "", "GOOGLE_CLOUD_API_KEY": "", "GOOGLE_CLOUD_PROJECT": "fixture-project", "GCLOUD_PROJECT": "", "GOOGLE_CLOUD_LOCATION": "global"}
			wantID := "scoped"
			if name == "process home" {
				delete(env, "HOME")
				delete(env, "APPDATA")
				wantID = "process"
			}
			if name == "explicit wins" {
				env["GOOGLE_APPLICATION_CREDENTIALS"] = explicit
				wantID = "explicit"
			}
			if name == "missing explicit" {
				env["GOOGLE_APPLICATION_CREDENTIALS"] = filepath.Join(root, "missing.json")
			}
			if name == "corrupt default" {
				if err := os.WriteFile(defaultPath(scopedHome), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if name == "empty home" {
				env["HOME"] = ""
				env["APPDATA"] = ""
			}
			if name == "absent default" {
				if err := os.Remove(defaultPath(scopedHome)); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(defaultPath(processHome)); err != nil {
					t.Fatal(err)
				}
			}
			var tokens, inferences atomic.Int32
			checks := make(chan error, 8)
			tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tokens.Add(1)
				if err := r.ParseForm(); err != nil {
					checks <- err
				}
				if r.PostForm.Get("client_id") != wantID || r.PostForm.Get("client_secret") != "fixture-secret" || r.PostForm.Get("refresh_token") != "fixture-refresh" || r.PostForm.Get("grant_type") != "refresh_token" {
					checks <- fmt.Errorf("selected wrong file/account")
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600}`, "token-"+wantID)
			}))
			defer tokenServer.Close()
			saved := newVertexTokenClient
			newVertexTokenClient = func(opts *goai.StreamOptions) *http.Client {
				client := saved(opts)
				base := client.Transport
				client.Transport = vertexTokenWireFunc(func(r *http.Request) (*http.Response, error) {
					if r.URL.String() != "https://oauth2.googleapis.com/token" {
						return nil, fmt.Errorf("unexpected token endpoint")
					}
					clone := r.Clone(r.Context())
					target, _ := url.Parse(tokenServer.URL + "/token")
					clone.URL = target
					return base.RoundTrip(clone)
				})
				return client
			}
			t.Cleanup(func() { newVertexTokenClient = saved })
			inference := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				inferences.Add(1)
				if name == "real key bypass" {
					if r.URL.Query().Get("key") != "real-test-key" || r.Header.Get("Authorization") != "" {
						checks <- fmt.Errorf("real key default-file bypass differs")
					}
				} else if r.Header.Get("Authorization") != "Bearer token-"+wantID || r.Header.Get("X-Goog-User-Project") != "quota-"+wantID || r.URL.Query().Has("key") {
					checks <- fmt.Errorf("default ADC auth headers differ")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: %s\n\n", mainGoogleStopChunk("STOP", false))
			}))
			defer inference.Close()
			opts := &goai.StreamOptions{Env: env}
			if name == "marker" {
				opts.APIKey = "<authenticated>"
			}
			if name == "real key bypass" {
				opts.APIKey = "real-test-key"
				env["GOOGLE_APPLICATION_CREDENTIALS"] = filepath.Join(root, "must-not-read.json")
			}
			model := &goai.Model{ID: "gemini", Api: goai.ApiGoogleVertex, Provider: goai.ProviderGoogleVertex, BaseURL: inference.URL}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			done, failed := 0, 0
			for event := range goai.Stream(ctx, model, &goai.Context{Messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "hello"}}}}}, opts) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					done++
				case *goai.ErrorEvent:
					failed++
					if e.Error == nil || e.Error.Model != "gemini" || e.Error.ErrorMessage == "" {
						t.Error("missing failure carrier")
					}
				}
			}
			failure := name == "missing explicit" || name == "corrupt default" || name == "empty home" || name == "absent default"
			wantTokens := int32(1)
			if failure || name == "real key bypass" {
				wantTokens = 0
			}
			if tokens.Load() != wantTokens {
				t.Errorf("token requests=%d want %d", tokens.Load(), wantTokens)
			}
			if failure {
				if done != 0 || failed != 1 || inferences.Load() != 0 {
					t.Errorf("done=%d errors=%d inference=%d", done, failed, inferences.Load())
				}
			} else {
				if done != 1 || failed != 0 || inferences.Load() != 1 {
					t.Errorf("done=%d errors=%d inference=%d", done, failed, inferences.Load())
				}
			}
			if name == "empty home" && goai.GetEnvAPIKeyWithEnv(goai.ProviderGoogleVertex, env) != "" {
				t.Error("presence detector ignored scoped home suppression")
			}
			for len(checks) > 0 {
				t.Error(<-checks)
			}
		})
	}
}
