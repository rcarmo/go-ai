package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"cloud.google.com/go/auth/credentials"
	goai "github.com/rcarmo/go-ai"
)

const vertexCloudScope = "https://www.googleapis.com/auth/cloud-platform"
const vertexTokenURL = "https://oauth2.googleapis.com/token"
const vertexCredentialLimit = 4 * 1024 * 1024

var vertexAuthLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

var openVertexCredentialFile = openVertexCredentialPath

// Transport-only seam for isolated canonical-endpoint tests; auth stays real.
var newVertexTokenClient = func(opts *goai.StreamOptions) *http.Client {
	cfg := googleRetryConfig(opts)
	client := cfg.NewHTTPClient()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client
}

// Resolve only a known file. Never invoke library ADC detection: it can bypass
// provider-scoped environment and probe GCE metadata. Explicit missing paths
// must produce their file error rather than silently falling back.
func vertexCredentialFile(opts *goai.StreamOptions) string {
	env := goai.ProviderEnvFromOptions(opts)
	path := goai.VertexADCCredentialPath(env)
	if goai.GetProviderEnvValue("GOOGLE_APPLICATION_CREDENTIALS", env) != "" {
		return path
	}
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// Typed file acquisition, with no library ambient/default ADC detection.
// Sources are request-owned, with synchronous refresh and no global token cache.
func applyVertexFileAuth(ctx context.Context, request *http.Request, path string, opts *goai.StreamOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("vertex ADC credentials: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > vertexCredentialLimit {
		return fmt.Errorf("vertex ADC requires a regular credential file of at most 4 MiB")
	}
	file, err := openVertexCredentialFile(path)
	if err != nil {
		return fmt.Errorf("vertex ADC credentials: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = file.Close()
		return err
	}
	// The path may have changed after Stat. Enforce the same bounds on the
	// descriptor we actually opened, before attempting any read.
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("vertex ADC credentials: %w", err)
	}
	if !opened.Mode().IsRegular() || opened.Size() > vertexCredentialLimit {
		_ = file.Close()
		return fmt.Errorf("vertex ADC requires a regular credential file of at most 4 MiB")
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, vertexCredentialLimit+1))
	closeErr := file.Close()
	if readErr != nil {
		return fmt.Errorf("vertex ADC credentials: %w", readErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if len(raw) > vertexCredentialLimit {
		return fmt.Errorf("vertex ADC credential file exceeds 4 MiB")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var config struct {
		Type     string `json:"type"`
		Universe string `json:"universe_domain"`
		Quota    string `json:"quota_project_id"`
	}
	if err = json.Unmarshal(raw, &config); err != nil {
		return fmt.Errorf("vertex ADC credential JSON: %w", err)
	}
	credentialType := credentials.CredType(config.Type)
	var scopes []string
	switch credentialType {
	case credentials.ServiceAccount:
		scopes = []string{vertexCloudScope}
	case credentials.AuthorizedUser:
		// Node UserRefreshClient rejects missing fields before requesting a
		// token. Validate here rather than relying on later grant failure.
		var user struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			RefreshToken string `json:"refresh_token"`
		}
		if err = json.Unmarshal(raw, &user); err != nil {
			return fmt.Errorf("vertex ADC authorized-user JSON: %w", err)
		}
		if user.ClientID == "" || user.ClientSecret == "" || user.RefreshToken == "" {
			return fmt.Errorf("vertex ADC authorized-user credentials require client_id, client_secret and refresh_token")
		}
	default:
		return fmt.Errorf("vertex ADC credential type is not supported")
	}
	if config.Universe != "" && config.Universe != "googleapis.com" {
		return fmt.Errorf("vertex ADC non-default universe is not supported")
	}
	// Locked Node JWT/UserRefreshClient ignore token_uri; internal gtoken and
	// OAuth2Client defaults use Google's canonical endpoint. Go's JWT loader honours
	// that file field. Preserve raw values while normalising only this field.
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("vertex ADC credential JSON: %w", err)
	}
	fields["token_uri"] = json.RawMessage(fmt.Sprintf("%q", vertexTokenURL))
	raw, err = json.Marshal(fields)
	if err != nil {
		return fmt.Errorf("vertex ADC credential JSON: %w", err)
	}
	tokenClient := newVertexTokenClient(opts)
	defer tokenClient.CloseIdleConnections()
	creds, err := credentials.NewCredentialsFromJSON(credentialType, raw, &credentials.DetectOptions{Scopes: scopes, DisableAsyncRefresh: true, Client: tokenClient, Logger: vertexAuthLogger})
	if err != nil {
		return fmt.Errorf("vertex ADC credentials: %w", err)
	}
	token, err := creds.Token(ctx)
	if err != nil {
		return fmt.Errorf("vertex ADC token: %w", err)
	}
	if token == nil || token.Value == "" {
		return fmt.Errorf("vertex ADC returned an empty token")
	}
	kind := token.Type
	if kind == "" {
		kind = "Bearer"
	}
	if strings.ContainsAny(kind+token.Value, "\r\n") {
		return fmt.Errorf("vertex ADC returned an invalid token header")
	}
	// Do not use Credentials.QuotaProjectID here: it reads process-global env
	// even when the caller supplied a provider-scoped empty override.
	quota := goai.GetProviderEnvValue("GOOGLE_CLOUD_QUOTA_PROJECT", goai.ProviderEnvFromOptions(opts))
	if quota == "" {
		quota = config.Quota
	}
	goai.ApplyDefaultHeaders(request.Header, map[string]string{"Authorization": kind + " " + token.Value})
	if quota != "" {
		if strings.ContainsAny(quota, "\r\n") {
			return fmt.Errorf("vertex ADC invalid quota header")
		}
		goai.ApplyDefaultHeaders(request.Header, map[string]string{"X-Goog-User-Project": quota})
	}
	return nil
}
