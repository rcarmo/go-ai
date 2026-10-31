package google

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
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
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Reference: 81f3cd41 Vertex keyFilename -> locked SDK NodeAuth -> GoogleAuth.
// Actual acquisition must precede inference even with caller Authorization;
// cloud-platform scope is required. All keys/files/endpoints are test-owned.
func TestMainVertexServiceAccountAcquisition(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", "must-not-leak-global-quota")
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustServiceAccountPKCS8(t, private)}))
	for _, name := range []string{"acquire", "empty key", "caller headers", "token failure", "cancel", "unsupported type", "invalid JSON", "suppressed headers", "ignored malformed URI", "redirect"} {
		t.Run(name, func(t *testing.T) {
			var tokenCalls, inferenceCalls, foreignTokenAttempts atomic.Int32
			tokenChecks := make(chan error, 2)
			tokenStarted := make(chan struct{}, 1)
			var tokenURL, inferenceURL string
			tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tokenCalls.Add(1)
				tokenChecks <- checkServiceAccountAssertion(r, private, "https://oauth2.googleapis.com/token")
				if name == "cancel" {
					tokenStarted <- struct{}{}
					<-r.Context().Done()
					return
				}
				if name == "redirect" {
					w.Header().Set("Location", inferenceURL)
					w.WriteHeader(http.StatusTemporaryRedirect)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if name == "token failure" {
					w.WriteHeader(400)
					fmt.Fprint(w, `{"error":"invalid_grant"}`)
					return
				}
				fmt.Fprint(w, `{"access_token":"acquired-test-token","token_type":"Bearer","expires_in":3600}`)
			}))
			defer tokenServer.Close()
			tokenURL = tokenServer.URL
			savedClientFactory := newVertexTokenClient
			newVertexTokenClient = func(opts *goai.StreamOptions) *http.Client {
				client := savedClientFactory(opts)
				transport := client.Transport
				client.Transport = vertexTokenWireFunc(func(request *http.Request) (*http.Response, error) {
					if request.URL.String() != "https://oauth2.googleapis.com/token" {
						foreignTokenAttempts.Add(1)
						return nil, fmt.Errorf("unexpected token endpoint")
					}
					target, _ := url.Parse(tokenURL)
					cloned := request.Clone(request.Context())
					copied := *request.URL
					cloned.URL = &copied
					cloned.URL.Scheme = target.Scheme
					cloned.URL.Host = target.Host
					return transport.RoundTrip(cloned)
				})
				return client
			}
			t.Cleanup(func() { newVertexTokenClient = savedClientFactory })
			requests := make(chan *http.Request, 2)
			inference := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				inferenceCalls.Add(1)
				requests <- r.Clone(r.Context())
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: %s\n\n", mainGoogleStopChunk("STOP", false))
			}))
			defer inference.Close()
			inferenceURL = inference.URL
			config := map[string]any{"type": "service_account", "client_email": "fixture@example.test", "private_key_id": "fixture-key", "private_key": encoded, "token_uri": tokenURL, "quota_project_id": "fixture-quota"}
			if name == "ignored malformed URI" {
				config["token_uri"] = map[string]any{"invalid": "ignored by Node JWT.fromJSON"}
			}
			if name == "unsupported type" {
				config["type"] = "external_account"
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if name == "invalid JSON" {
				raw = []byte("{")
			}
			path := filepath.Join(t.TempDir(), "credentials.json")
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			model := &goai.Model{ID: "gemini", Api: goai.ApiGoogleVertex, Provider: goai.ProviderGoogleVertex, BaseURL: inference.URL}
			opts := &goai.StreamOptions{APIKey: "<authenticated>", Project: "fixture-project", Location: "global", Env: goai.ProviderEnv{"GOOGLE_CLOUD_QUOTA_PROJECT": "", "GOOGLE_APPLICATION_CREDENTIALS": path, "GOOGLE_CLOUD_API_KEY": "", "GOOGLE_CLOUD_PROJECT": "", "GCLOUD_PROJECT": "", "GOOGLE_CLOUD_LOCATION": ""}}
			if name == "empty key" {
				opts.APIKey = ""
			}
			wantAuth, wantQuota := "Bearer acquired-test-token", "fixture-quota"
			if name == "caller headers" {
				wantAuth, wantQuota = "Bearer caller-test-token", "caller-quota"
				opts.Headers = map[string]string{"Authorization": wantAuth, "X-Goog-User-Project": wantQuota}
			}
			if name == "suppressed headers" {
				opts.SuppressHeaders = []string{"Authorization", "X-Goog-User-Project"}
				wantAuth, wantQuota = "", ""
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if name == "cancel" {
				stopped := make(chan struct{})
				go func() {
					defer close(stopped)
					select {
					case <-tokenStarted:
						cancel()
					case <-ctx.Done():
					}
				}()
				defer func() { cancel(); <-stopped }()
			}
			done, failed := 0, 0
			var failure *goai.ErrorEvent
			for event := range goai.Stream(ctx, model, &goai.Context{Messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "hello"}}}}}, opts) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					done++
				case *goai.ErrorEvent:
					failed++
					failure = e
				}
			}
			wantsFailure := name == "token failure" || name == "cancel" || name == "unsupported type" || name == "invalid JSON" || name == "redirect"
			if wantsFailure {
				if done != 0 || failed != 1 || inferenceCalls.Load() != 0 {
					t.Errorf("done=%d errors=%d inference=%d", done, failed, inferenceCalls.Load())
				}
				if failure == nil || failure.Error == nil || failure.Error.Model != "gemini" || failure.Error.ErrorMessage == "" {
					t.Error("missing assistant failure carrier")
				}
				if name == "cancel" && (failure == nil || failure.Reason != goai.StopReasonAborted) {
					t.Error("auth cancellation must abort")
				}
			} else {
				if done != 1 || failed != 0 || inferenceCalls.Load() != 1 {
					t.Errorf("done=%d errors=%d inference=%d", done, failed, inferenceCalls.Load())
				}
				select {
				case request := <-requests:
					if request.Header.Get("Authorization") != wantAuth || request.Header.Get("X-Goog-User-Project") != wantQuota || request.URL.Query().Has("key") {
						t.Error("acquired/default/caller auth headers differ")
					}
				default:
					t.Error("no inference request")
				}
			}
			expectedTokens := int32(1)
			if name == "unsupported type" || name == "invalid JSON" {
				expectedTokens = 0
			}
			if tokenCalls.Load() != expectedTokens {
				t.Errorf("token requests=%d want %d", tokenCalls.Load(), expectedTokens)
			}
			if foreignTokenAttempts.Load() != 0 {
				t.Error("token client attempted noncanonical endpoint or redirect")
			}
			for len(tokenChecks) > 0 {
				if err := <-tokenChecks; err != nil {
					t.Error(err)
				}
			}
		})
	}
}

type vertexTokenWireFunc func(*http.Request) (*http.Response, error)

func (f vertexTokenWireFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func mustServiceAccountPKCS8(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	data, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func checkServiceAccountAssertion(r *http.Request, key *rsa.PrivateKey, audience string) error {
	if err := r.ParseForm(); err != nil {
		return err
	}
	if r.Method != "POST" || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
		return fmt.Errorf("invalid token grant")
	}
	pieces := strings.Split(r.Form.Get("assertion"), ".")
	if len(pieces) != 3 {
		return fmt.Errorf("invalid assertion")
	}
	header, err := base64.RawURLEncoding.DecodeString(pieces[0])
	if err != nil {
		return err
	}
	var protected struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	if err = json.Unmarshal(header, &protected); err != nil {
		return err
	}
	if protected.Algorithm != "RS256" || protected.KeyID != "fixture-key" {
		return fmt.Errorf("invalid assertion protected header")
	}
	payload, err := base64.RawURLEncoding.DecodeString(pieces[1])
	if err != nil {
		return err
	}
	var claims struct {
		Issuer   string `json:"iss"`
		Audience string `json:"aud"`
		Scope    string `json:"scope"`
		Issued   int64  `json:"iat"`
		Expires  int64  `json:"exp"`
	}
	if err = json.Unmarshal(payload, &claims); err != nil {
		return err
	}
	if claims.Issuer != "fixture@example.test" || claims.Audience != audience || claims.Scope != "https://www.googleapis.com/auth/cloud-platform" || claims.Expires <= time.Now().Unix() || claims.Expires <= claims.Issued || claims.Issued < time.Now().Unix()-60 || claims.Issued > time.Now().Unix()+10 {
		return fmt.Errorf("invalid assertion claims")
	}
	signature, err := base64.RawURLEncoding.DecodeString(pieces[2])
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(pieces[0] + "." + pieces[1]))
	return rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature)
}
