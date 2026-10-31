package google

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"net/http"
)

func googleRetryConfig(opts *goai.StreamOptions) goai.RetryConfig {
	// Explicit native configuration retains its legacy policy and precedence.
	if opts != nil && opts.RetryConfig != nil {
		return goai.RetryConfigFromOptions(opts)
	}
	cfg := goai.NoRetryConfig()
	cfg.MaxRetryDelayMs = 60000
	if opts != nil {
		if opts.MaxRetries != nil {
			cfg.MaxRetries = max(0, *opts.MaxRetries)
		}
		if opts.MaxRetryDelayMs != nil {
			cfg.MaxRetryDelayMs = *opts.MaxRetryDelayMs
		}
	}
	return cfg
}

func doGoogleRequest(ctx context.Context, client *http.Client, req *http.Request, cfg goai.RetryConfig, opts *goai.StreamOptions) (*http.Response, error) {
	if opts != nil && opts.RetryConfig != nil {
		return goai.DoWithRetry(ctx, client, req, cfg)
	}
	return goai.DoProviderRequestWithRetry(ctx, client, req, cfg)
}
