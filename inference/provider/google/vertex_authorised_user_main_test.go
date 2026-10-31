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
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Locked Google Auth 10.6.2: UserRefreshClient.fromJSON validates three
// fields; OAuth2Client.refreshTokenNoCache posts exactly the refresh grant to
// its canonical endpoint, not a credential-file token_uri or caller headers.
func TestMainVertexAuthorisedUser(t *testing.T) {
	for _, name := range []string{"acquire", "empty key", "caller headers", "token failure", "cancel", "missing client ID", "missing secret", "missing refresh", "malformed URI"} {
		t.Run(name, func(t *testing.T) {
			var tokens, inferences atomic.Int32
			checks := make(chan error, 8)
			started := make(chan struct{}, 1)
			tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tokens.Add(1)
				if err := r.ParseForm(); err != nil {
					checks <- err
				}
				if r.Method != "POST" || len(r.PostForm) != 4 || r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("client_id") != "fixture-client" || r.PostForm.Get("client_secret") != "fixture-secret" || r.PostForm.Get("refresh_token") != "fixture-refresh" || r.Header.Get("Authorization") != "" {
					checks <- fmt.Errorf("refresh grant or credential/header scope differs")
				}
				if name == "cancel" {
					started <- struct{}{}
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if name == "token failure" {
					w.WriteHeader(400)
					fmt.Fprint(w, `{"error":"invalid_grant"}`)
					return
				}
				fmt.Fprint(w, `{"access_token":"user-access-token","token_type":"Bearer","expires_in":3600,"refresh_token":"rotated-fixture-refresh"}`)
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
					cloned := r.Clone(r.Context())
					target, _ := url.Parse(tokenServer.URL + "/token")
					cloned.URL = target
					return base.RoundTrip(cloned)
				})
				return client
			}
			t.Cleanup(func() { newVertexTokenClient = saved })
			wantAuth, wantQuota := "Bearer user-access-token", "user-quota"
			if name == "caller headers" {
				wantAuth, wantQuota = "Bearer caller-token", "caller-quota"
			}
			inference := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				inferences.Add(1)
				if r.Header.Get("Authorization") != wantAuth || r.Header.Get("X-Goog-User-Project") != wantQuota || r.URL.Query().Has("key") {
					checks <- fmt.Errorf("refresh auth/header merging differs")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: %s\n\n", mainGoogleStopChunk("STOP", false))
			}))
			defer inference.Close()
			config := map[string]any{"type": "authorized_user", "client_id": "fixture-client", "client_secret": "fixture-secret", "refresh_token": "fixture-refresh", "quota_project_id": "user-quota", "token_uri": "https://not-an-owned-endpoint.invalid/token"}
			if name == "missing client ID" {
				delete(config, "client_id")
			}
			if name == "missing secret" {
				delete(config, "client_secret")
			}
			if name == "missing refresh" {
				delete(config, "refresh_token")
			}
			if name == "malformed URI" {
				config["token_uri"] = []string{"ignored"}
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "user.json")
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			opts := &goai.StreamOptions{APIKey: "<authenticated>", Project: "fixture-project", Location: "global", Env: goai.ProviderEnv{"GOOGLE_APPLICATION_CREDENTIALS": path, "GOOGLE_CLOUD_API_KEY": "", "GOOGLE_CLOUD_PROJECT": "", "GCLOUD_PROJECT": "", "GOOGLE_CLOUD_QUOTA_PROJECT": ""}}
			if name == "empty key" {
				opts.APIKey = ""
			}
			if name == "caller headers" {
				opts.Headers = map[string]string{"Authorization": wantAuth, "X-Goog-User-Project": wantQuota}
			}
			model := &goai.Model{ID: "gemini", Api: goai.ApiGoogleVertex, Provider: goai.ProviderGoogleVertex, BaseURL: inference.URL}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if name == "cancel" {
				stopped := make(chan struct{})
				go func() {
					defer close(stopped)
					select {
					case <-started:
						cancel()
					case <-ctx.Done():
					}
				}()
				defer func() { cancel(); <-stopped }()
			}
			done, failed := 0, 0
			reason := goai.StopReasonError
			if name == "cancel" {
				reason = goai.StopReasonAborted
			}
			attempts := 1
			if name == "acquire" {
				attempts = 2
			}
			for i := 0; i < attempts; i++ {
				for event := range goai.Stream(ctx, model, &goai.Context{Messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "hello"}}}}}, opts) {
					switch e := event.(type) {
					case *goai.DoneEvent:
						done++
					case *goai.ErrorEvent:
						failed++
						if e.Error == nil || e.Error.Model != "gemini" || e.Error.ErrorMessage == "" || e.Reason != reason {
							t.Error("failure carrier/reason differs")
						}
					}
				}
			}
			invalid := name == "missing client ID" || name == "missing secret" || name == "missing refresh"
			failure := invalid || name == "cancel" || name == "token failure"
			wantTokens := int32(attempts)
			if invalid {
				wantTokens = 0
			}
			if tokens.Load() != wantTokens {
				t.Errorf("tokens=%d want %d", tokens.Load(), wantTokens)
			}
			if failure {
				if done != 0 || failed != 1 || inferences.Load() != 0 {
					t.Errorf("done=%d errors=%d inference=%d", done, failed, inferences.Load())
				}
			} else {
				if done != attempts || failed != 0 || inferences.Load() != int32(attempts) {
					t.Errorf("done=%d errors=%d inference=%d", done, failed, inferences.Load())
				}
			}
			for len(checks) > 0 {
				t.Error(<-checks)
			}
		})
	}
}
