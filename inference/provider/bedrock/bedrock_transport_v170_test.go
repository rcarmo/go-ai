package bedrock

import (
	"context"
	"encoding/pem"
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

// Ported from the unchanged official pi v1.7.0 originals at
// fb0fe823539f6d04850f6e34748c01f01f212e1f:
//   - bedrock-credentials.test.ts
//   - bedrock-custom-headers.test.ts
//   - bedrock-endpoint-resolution.test.ts
//
// The assertions use Bedrock's production SDK path and a local request sink.
func TestV170BedrockGenericAPIKeyUsesBearerAuth(t *testing.T) {
	received := make(chan http.Header, 1)
	server, certificatePath := newBedrockTLSRequestSink(t, func(req *http.Request) {
		received <- req.Header.Clone()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestV170BedrockBearerFixtureChild$")
	cmd.Env = []string{
		"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"),
		"AWS_EC2_METADATA_DISABLED=true", "AWS_MAX_ATTEMPTS=1",
		"SSL_CERT_FILE=" + certificatePath,
		"GOAI_TEST_BEDROCK_BEARER_URL=" + server.URL,
		"GOAI_TEST_BEDROCK_API_KEY=bedrock-api-key",
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated bearer client: %v\n%s", err, out)
	}
	select {
	case headers := <-received:
		if got := headers.Get("Authorization"); got != "Bearer bedrock-api-key" {
			t.Fatal("generic apiKey was not selected for Bedrock bearer authentication")
		}
	default:
		t.Fatal("Bedrock request did not reach local fixture")
	}
}

func TestV170BedrockBearerFixtureChild(t *testing.T) {
	baseURL := os.Getenv("GOAI_TEST_BEDROCK_BEARER_URL")
	if baseURL == "" {
		return
	}
	model := &goai.Model{ID: "us.anthropic.claude-opus-4-8", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, BaseURL: baseURL}
	_, err := goai.Complete(t.Context(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: os.Getenv("GOAI_TEST_BEDROCK_API_KEY"), Region: "us-east-1"})
	if err == nil || !strings.Contains(err.Error(), "local fixture refused the request") {
		t.Fatal("expected local fixture terminal error")
	}
}

func TestV170BedrockCustomHeadersThroughProductionTransport(t *testing.T) {
	configureEmptyBedrockAmbient(t)
	requests := make(chan *http.Request, 1)
	server := newBedrockRequestSink(t, func(req *http.Request) {
		requests <- req.Clone(req.Context())
	})
	model := &goai.Model{ID: "us.anthropic.claude-opus-4-8", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, BaseURL: server.URL}
	_, err := goai.Complete(t.Context(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{
		Region: "us-east-1",
		Env:    goai.ProviderEnv{"AWS_BEDROCK_SKIP_AUTH": "1"},
		Headers: map[string]string{
			"authorization":        "evil",
			"Authorization":        "evil2",
			"x-amz-date":           "evil-date",
			"X-Amz-Date":           "evil-date2",
			"x-amz-security-token": "evil-token",
			"HOST":                 "evil-host",
			"x-allowed":            "ok",
		},
	})
	if err == nil {
		t.Fatal("expected local fixture terminal error")
	}
	select {
	case req := <-requests:
		if got := req.Header.Get("X-Allowed"); got != "ok" {
			t.Fatalf("allowed header = %q", got)
		}
		if got := req.Header.Get("Authorization"); got == "" || got == "evil" || got == "evil2" {
			t.Fatal("reserved Authorization header was not left to AWS signing")
		}
		if got := req.Header.Get("X-Amz-Date"); got == "" || strings.HasPrefix(got, "evil") {
			t.Fatal("reserved X-Amz-Date header was not left to AWS signing")
		}
		if got := req.Header.Get("X-Amz-Security-Token"); strings.HasPrefix(got, "evil") {
			t.Fatal("reserved security token header was overwritten")
		}
		if req.Host != strings.TrimPrefix(server.URL, "http://") {
			t.Fatal("reserved Host header was overwritten")
		}
	default:
		t.Fatal("Bedrock request did not reach local fixture")
	}
}

func TestV170BedrockAmbientProfileRegionThroughProductionTransport(t *testing.T) {
	configureEmptyBedrockAmbient(t)
	home := t.TempDir()
	configPath := filepath.Join(home, "config")
	credentialsPath := filepath.Join(home, "credentials")
	writeV170BedrockFile(t, configPath, "[profile bedrock-profile]\nregion = eu-central-1\n")
	writeV170BedrockFile(t, credentialsPath, "[bedrock-profile]\naws_access_key_id = PROFILEEXAMPLE\naws_secret_access_key = fixture-secret\n")
	t.Setenv("AWS_PROFILE", "bedrock-profile")
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsPath)

	requests := make(chan http.Header, 1)
	server := newBedrockRequestSink(t, func(req *http.Request) {
		requests <- req.Header.Clone()
	})
	model := &goai.Model{ID: "custom-model", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, BaseURL: server.URL}
	_, err := goai.Complete(t.Context(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, nil)
	if err == nil {
		t.Fatal("expected local fixture terminal error")
	}
	select {
	case headers := <-requests:
		auth := headers.Get("Authorization")
		if !strings.Contains(auth, "Credential=PROFILEEXAMPLE/") {
			t.Fatal("ambient profile credentials were not used")
		}
		if !strings.Contains(auth, "/eu-central-1/bedrock/aws4_request") {
			t.Fatal("ambient profile region was overridden")
		}
	default:
		t.Fatal("Bedrock request did not reach local fixture")
	}
}

func TestV170BedrockEndpointCatalogAndCustomEndpoint(t *testing.T) {
	goai.RegisterBuiltinModels()
	eu := goai.GetModel(goai.ProviderAmazonBedrock, "eu.anthropic.claude-sonnet-4-5-20250929-v1:0")
	if eu == nil || eu.BaseURL != "https://bedrock-runtime.eu-central-1.amazonaws.com" {
		t.Fatalf("EU Bedrock endpoint = %#v", eu)
	}
	configureEmptyBedrockAmbient(t)
	received := make(chan string, 1)
	server := newBedrockRequestSink(t, func(req *http.Request) {
		received <- req.URL.Path
	})
	model := &goai.Model{ID: "us.anthropic.claude-opus-4-8", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, BaseURL: server.URL}
	_, err := goai.Complete(t.Context(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{Region: "us-west-2", Env: goai.ProviderEnv{"AWS_BEDROCK_SKIP_AUTH": "1"}})
	if err == nil {
		t.Fatal("expected local fixture terminal error")
	}
	select {
	case path := <-received:
		if path != "/model/us.anthropic.claude-opus-4-8/converse-stream" {
			t.Fatalf("custom endpoint request path = %q", path)
		}
	default:
		t.Fatal("custom endpoint was not used")
	}
}

func configureEmptyBedrockAmbient(t *testing.T) {
	t.Helper()
	for _, name := range []string{"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_BEARER_TOKEN_BEDROCK", "AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE", "HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY"} {
		t.Setenv(name, "")
	}
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_MAX_ATTEMPTS", "1")
}

func newBedrockTLSRequestSink(t *testing.T, observe func(*http.Request)) (*httptest.Server, string) {
	t.Helper()
	server := httptest.NewTLSServer(bedrockRequestSinkHandler(observe))
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	certificatePath := filepath.Join(t.TempDir(), "fixture-ca.pem")
	if err := os.WriteFile(certificatePath, certificate, 0o600); err != nil {
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return server, certificatePath
}

func newBedrockRequestSink(t *testing.T, observe func(*http.Request)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(bedrockRequestSinkHandler(observe))
	t.Cleanup(server.Close)
	return server
}

func bedrockRequestSinkHandler(observe func(*http.Request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if observe != nil {
			observe(req)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"local fixture refused the request"}`)
	})
}

func writeV170BedrockFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
