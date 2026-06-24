package goai_test

import (
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestHTTPProxySupportRespectsNoProxyExclusions(t *testing.T) {
	out := runProxyHelper(t, map[string]string{
		"HTTPS_PROXY": "http://proxy.example:8080",
		"NO_PROXY":    "bedrock-runtime.us-east-1.amazonaws.com",
	}, "https://bedrock-runtime.us-east-1.amazonaws.com")
	if out != "<nil>" {
		t.Fatalf("proxy=%s, want <nil> due to NO_PROXY", out)
	}
}

func TestHTTPProxySupportResolvesHTTPSProxyURL(t *testing.T) {
	out := runProxyHelper(t, map[string]string{"HTTPS_PROXY": "http://proxy.example:8080"}, "https://bedrock-runtime.us-east-1.amazonaws.com")
	if out != "http://proxy.example:8080" {
		t.Fatalf("proxy=%s, want http://proxy.example:8080", out)
	}
}

func TestHTTPProxySupportResolvesHTTPProxyURL(t *testing.T) {
	out := runProxyHelper(t, map[string]string{"HTTP_PROXY": "http://proxy.example:8080"}, "http://example.com")
	if out != "http://proxy.example:8080" {
		t.Fatalf("proxy=%s, want http://proxy.example:8080", out)
	}
}

func runProxyHelper(t *testing.T, env map[string]string, target string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestHTTPProxySupportHelperProcess", "--", target)
	cmd.Env = append(os.Environ(), "GOAI_PROXY_HELPER=1")
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy"} {
		cmd.Env = append(cmd.Env, key+"=")
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("proxy helper failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestHTTPProxySupportHelperProcess(t *testing.T) {
	if os.Getenv("GOAI_PROXY_HELPER") != "1" {
		return
	}
	args := os.Args
	target := args[len(args)-1]
	cfg := goai.NoRetryConfig()
	client := cfg.NewHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		panic("expected transport with ProxyFromEnvironment")
	}
	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		panic(err)
	}
	proxy, err := transport.Proxy(req)
	if err != nil {
		panic(err)
	}
	if proxy == nil {
		_, _ = os.Stdout.WriteString("<nil>")
	} else {
		_, _ = os.Stdout.WriteString(proxy.String())
	}
	os.Exit(0)
}
