package bedrock

import (
	"context"
	"crypto/sha1" // The SDK uses this digest as its SSO cache file name.
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// TestSourceV161BedrockProfileUsesProxy uses a fresh child process so proxy
// and shared-config state cannot be inherited from earlier SDK requests.
func TestSourceV161BedrockProfileUsesProxy(t *testing.T) {
	const child = "GOAI_BEDROCK_V161_PROXY_CHILD"
	if os.Getenv(child) == "1" {
		goai.RegisterBuiltinModels()
		model := goai.GetModel(goai.ProviderAmazonBedrock, "us.anthropic.claude-opus-4-8")
		if model == nil {
			t.Fatal("missing pinned Bedrock model")
		}
		ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
		var reason goai.StopReason
		terminal := 0
		for event := range goai.Stream(t.Context(), model, ctx, &goai.StreamOptions{Profile: "sso", CacheRetention: goai.CacheRetentionNone}) {
			switch e := event.(type) {
			case *goai.ErrorEvent:
				terminal++
				reason = e.Reason
			case *goai.DoneEvent:
				terminal++
				reason = e.Reason
			}
		}
		if terminal != 1 || reason != goai.StopReasonError {
			t.Fatalf("terminal events = %d with reason %q; want one error", terminal, reason)
		}
		return
	}

	home := t.TempDir()
	config := filepath.Join(home, "config")
	const startURL = "https://example.awsapps.com/start"
	const profile = "[profile sso]\nsso_start_url = " + startURL + "\nsso_region = us-east-1\nsso_account_id = 123456789012\nsso_role_name = Example\n"
	if err := os.WriteFile(config, []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(home, ".aws", "sso", "cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha1.Sum([]byte(startURL))
	const token = `{"startUrl":"` + startURL + `","region":"us-east-1","accessToken":"example-token","expiresAt":"2099-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(cache, hex.EncodeToString(digest[:])+".json"), []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var targets []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			mu.Lock()
			targets = append(targets, r.RequestURI)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestSourceV161BedrockProfileUsesProxy$", "-test.timeout=9s")
	cmd.Env = []string{
		child + "=1", "HOME=" + home, "USERPROFILE=" + home,
		"AWS_CONFIG_FILE=" + config, "AWS_SHARED_CREDENTIALS_FILE=" + filepath.Join(home, "credentials"),
		"AWS_REGION=us-east-1", "AWS_ENDPOINT_URL_SSO=https://sso.invalid", "AWS_EC2_METADATA_DISABLED=true",
		"AWS_MAX_ATTEMPTS=1", "HTTPS_PROXY=" + proxy.URL,
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated Bedrock child: %v\n%s", err, out)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(targets, "sso.invalid:443") {
		t.Fatalf("proxy CONNECT targets = %q; missing sso.invalid:443", targets)
	}
}
