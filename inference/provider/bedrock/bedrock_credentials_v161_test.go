package bedrock

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV161BedrockCredentialPriority(t *testing.T) {
	for _, row := range []struct {
		name, explicit, scoped, ambient, signer string
		proxied                                 bool
	}{
		{name: "explicit", explicit: "explicit-profile", signer: "EXPLICITEXAMPLE"},
		{name: "scoped", scoped: "scoped-profile", signer: "SCOPEDEXAMPLE"},
		{name: "static", signer: "AKIAEXAMPLE"},
		{name: "proxy-static", signer: "AKIAEXAMPLE", proxied: true},
		{name: "ambient", ambient: "ambient-profile", signer: "AKIAEXAMPLE"},
	} {
		t.Run(row.name, func(t *testing.T) {
			home := t.TempDir()
			cfg := filepath.Join(home, "config")
			creds := filepath.Join(home, "credentials")
			writeBedrockPriorityFile(t, cfg, "[profile explicit-profile]\nregion = us-east-1\n[profile scoped-profile]\nregion = us-east-1\n[profile ambient-profile]\nregion = us-east-1\n")
			writeBedrockPriorityFile(t, creds, "[explicit-profile]\naws_access_key_id = EXPLICITEXAMPLE\naws_secret_access_key = explicit-secret-example\n[scoped-profile]\naws_access_key_id = SCOPEDEXAMPLE\naws_secret_access_key = scoped-secret-example\n[ambient-profile]\naws_access_key_id = AMBIENTEXAMPLE\naws_secret_access_key = ambient-secret-example\n")
			wire := make(chan [3]string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				auth := req.Header.Get("Authorization")
				_, signer, ok := strings.Cut(auth, "Credential=")
				if ok {
					signer, _, _ = strings.Cut(signer, "/")
				}
				select {
				case wire <- [3]string{signer, req.Host, req.URL.Path}:
				default:
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, `{"message":"local fixture refused the signed request"}`)
			}))
			defer server.Close()
			endpoint, expectedHost := server.URL, strings.TrimPrefix(server.URL, "http://")
			proxy := ""
			if row.proxied {
				endpoint, expectedHost, proxy = "http://bedrock.example.invalid", "bedrock.example.invalid", server.URL
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBedrockCredentialPriorityFixtureChild$")
			cmd.Env = []string{
				"HOME=" + home, "PATH=" + os.Getenv("PATH"),
				"AWS_CONFIG_FILE=" + cfg, "AWS_SHARED_CREDENTIALS_FILE=" + creds,
				"AWS_REGION=us-east-1", "AWS_EC2_METADATA_DISABLED=true", "AWS_MAX_ATTEMPTS=1",
				"AWS_ACCESS_KEY_ID=AKIAEXAMPLE", "AWS_SECRET_ACCESS_KEY=secretexample", "AWS_PROFILE=" + row.ambient,
				"HTTP_PROXY=" + proxy, "HTTPS_PROXY=" + proxy,
				"GOAI_TEST_BEDROCK_PRIORITY_URL=" + endpoint, "GOAI_TEST_BEDROCK_PRIORITY_EXPLICIT=" + row.explicit, "GOAI_TEST_BEDROCK_PRIORITY_SCOPED=" + row.scoped,
			}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("isolated client: %v\n%s", err, out)
			}
			select {
			case received := <-wire:
				want := [3]string{row.signer, expectedHost, "/model/us.anthropic.claude-opus-4-8/converse-stream"}
				if received != want {
					t.Errorf("signed request=%q; want %q", received, want)
				}
			default:
				t.Fatal("signed request never reached the local fixture")
			}
			if len(wire) != 0 {
				t.Error("unexpected second request")
			}
		})
	}
}

func TestBedrockCredentialPriorityFixtureChild(t *testing.T) {
	base := os.Getenv("GOAI_TEST_BEDROCK_PRIORITY_URL")
	if base == "" {
		return // The parent table supplies isolated SDK configuration to the child.
	}
	model := &goai.Model{ID: "us.anthropic.claude-opus-4-8", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, BaseURL: base}
	opts := &goai.StreamOptions{CacheRetention: "none", Profile: os.Getenv("GOAI_TEST_BEDROCK_PRIORITY_EXPLICIT")}
	if scoped := os.Getenv("GOAI_TEST_BEDROCK_PRIORITY_SCOPED"); scoped != "" {
		opts.Env = goai.ProviderEnv{"AWS_PROFILE": scoped}
	}
	_, err := goai.Complete(t.Context(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts)
	if err == nil || !strings.Contains(err.Error(), "local fixture refused the signed request") {
		t.Fatalf("fixture terminal error = %v", err)
	}
}

func writeBedrockPriorityFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
