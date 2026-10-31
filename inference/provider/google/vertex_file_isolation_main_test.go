package google

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// 81f3cd41 api/google-vertex.ts constructs a new GoogleGenAI for each stream.
// Explicit file ownership must survive replacement, deletion and concurrent
// callers sharing endpoint/project/key ID. No ambient credentials may win.
func TestMainVertexFileIsolation(t *testing.T) {
	type account struct {
		key *rsa.PrivateKey
		raw []byte
	}
	accounts := map[string]account{}
	for _, id := range []string{"a", "b"} {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(map[string]any{"type": "service_account", "client_email": id + "@example.test", "private_key_id": "fixture-key", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustServiceAccountPKCS8(t, key)})), "quota_project_id": "quota-" + id})
		if err != nil {
			t.Fatal(err)
		}
		accounts[id] = account{key, raw}
	}
	root := t.TempDir()
	shared, separate, ambient := filepath.Join(root, "shared.json"), filepath.Join(root, "separate.json"), filepath.Join(root, "ambient.json")
	for path, data := range map[string][]byte{shared: accounts["a"].raw, separate: accounts["b"].raw, ambient: []byte("not a credential")} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", ambient)
	t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", "ambient-quota-must-not-win")
	var tokens, inferences atomic.Int32
	checks := make(chan error, 64)
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens.Add(1)
		if err := r.ParseForm(); err != nil {
			checks <- err
			http.Error(w, "bad form", 400)
			return
		}
		parts := strings.Split(r.Form.Get("assertion"), ".")
		if len(parts) != 3 {
			checks <- fmt.Errorf("missing assertion")
			http.Error(w, "bad assertion", 400)
			return
		}
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		var claims struct {
			Issuer string `json:"iss"`
		}
		if err == nil {
			err = json.Unmarshal(payload, &claims)
		}
		id := strings.TrimSuffix(claims.Issuer, "@example.test")
		acct, ok := accounts[id]
		if err != nil || !ok {
			checks <- fmt.Errorf("unexpected account")
			http.Error(w, "bad account", 400)
			return
		}
		if err = checkServiceAccountAssertion(r, acct.key, "https://oauth2.googleapis.com/token", id+"@example.test"); err != nil {
			checks <- err
			http.Error(w, "invalid grant", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600}`, "token-"+id)
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
			copy := r.Clone(r.Context())
			target, _ := url.Parse(tokenServer.URL + "/token")
			copy.URL = target
			return base.RoundTrip(copy)
		})
		return client
	}
	t.Cleanup(func() { newVertexTokenClient = saved })
	inference := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inferences.Add(1)
		id := r.Header.Get("X-Fixture-Account")
		quota := "quota-" + id
		if id == "b" {
			quota = "scoped-quota-b"
		}
		if r.Header.Get("Authorization") != "Bearer token-"+id || r.Header.Get("X-Goog-User-Project") != quota {
			checks <- fmt.Errorf("account token/quota isolation failed for %s", id)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", mainGoogleStopChunk("STOP", false))
	}))
	defer inference.Close()
	model := &goai.Model{ID: "gemini", Api: goai.ApiGoogleVertex, Provider: goai.ProviderGoogleVertex, BaseURL: inference.URL}
	options := func(path, id string) *goai.StreamOptions {
		quota := ""
		if id == "b" {
			quota = "scoped-quota-b"
		}
		return &goai.StreamOptions{APIKey: "<authenticated>", Project: "same-project", Location: "global", Headers: map[string]string{"X-Fixture-Account": id}, Env: goai.ProviderEnv{"GOOGLE_APPLICATION_CREDENTIALS": path, "GOOGLE_CLOUD_QUOTA_PROJECT": quota}}
	}
	run := func(ctx context.Context, opts *goai.StreamOptions, success bool, reason goai.StopReason) {
		done, failed := 0, 0
		for event := range goai.Stream(ctx, model, &goai.Context{Messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "hello"}}}}}, opts) {
			switch e := event.(type) {
			case *goai.DoneEvent:
				done++
			case *goai.ErrorEvent:
				failed++
				if e.Error == nil || e.Error.Model != "gemini" || e.Reason != reason {
					checks <- fmt.Errorf("invalid failure carrier")
				}
			}
		}
		if success && (done != 1 || failed != 0) || !success && (done != 0 || failed != 1) {
			checks <- fmt.Errorf("terminal done=%d errors=%d", done, failed)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opts := options(shared, "a")
	run(ctx, opts, true, "")
	run(ctx, opts, true, "")
	replacement := filepath.Join(root, "replacement.json")
	if err := os.WriteFile(replacement, accounts["b"].raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, shared); err != nil {
		t.Fatal(err)
	}
	opts.Headers["X-Fixture-Account"] = "b"
	opts.Env["GOOGLE_CLOUD_QUOTA_PROJECT"] = "scoped-quota-b"
	run(ctx, opts, true, "")
	if err := os.Remove(shared); err != nil {
		t.Fatal(err)
	}
	run(ctx, opts, false, goai.StopReasonError)
	if err := os.WriteFile(shared, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	run(ctx, opts, false, goai.StopReasonError)
	if err := os.WriteFile(shared, accounts["a"].raw, 0600); err != nil {
		t.Fatal(err)
	}
	opts.Headers["X-Fixture-Account"] = "a"
	opts.Env["GOOGLE_CLOUD_QUOTA_PROJECT"] = ""
	run(ctx, opts, true, "")
	var wg sync.WaitGroup
	for _, pair := range [][2]string{{shared, "a"}, {separate, "b"}} {
		wg.Add(1)
		go func(pair [2]string) { defer wg.Done(); run(ctx, options(pair[0], pair[1]), true, "") }(pair)
	}
	wg.Wait()
	stopped, stop := context.WithCancel(ctx)
	stop()
	run(stopped, options(shared, "a"), false, goai.StopReasonAborted)
	if tokens.Load() != 6 || inferences.Load() != 6 {
		t.Errorf("tokens=%d inference=%d; expected 6 fresh acquisitions/requests", tokens.Load(), inferences.Load())
	}
	for len(checks) > 0 {
		t.Error(<-checks)
	}
}
