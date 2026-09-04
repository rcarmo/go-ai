// HTTP retry logic with exponential backoff, jitter, and configurable limits.
package goai

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	retryutil "github.com/rcarmo/go-ai/internal/retry"
)

// RetryConfig controls retry behavior with exponential backoff.
//
// Backoff formula: delay = min(InitialDelay * BackoffMultiplier^attempt * jitter, MaxDelay)
// where jitter is uniformly distributed in [1-JitterFraction, 1+JitterFraction].
type RetryConfig struct {
	MaxRetries        int                                                // max retry attempts (default: 3). 0 disables retries.
	InitialDelay      time.Duration                                      // base delay before first retry (default: 1s)
	MaxDelay          time.Duration                                      // cap on computed backoff (default: 60s)
	BackoffMultiplier float64                                            // exponential base (default: 2.0). 1.0 = constant interval.
	JitterFraction    float64                                            // jitter range as fraction (default: 0.25 = ±25%). 0 = none.
	MaxRetryDelayMs   int                                                // cap on server Retry-After delay. 0 = no cap. (default: 60000)
	ConnectTimeout    time.Duration                                      // TCP connection timeout (default: 30s)
	RequestTimeout    time.Duration                                      // full request timeout (default: 10m)
	RetryableStatuses []int                                              // override retryable codes (default: [429,500,502,503,504])
	OnRetry           func(attempt int, delay time.Duration, status int) // called before each retry
}

// DefaultRetryConfig returns production-ready defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:        3,
		InitialDelay:      time.Second,
		MaxDelay:          60 * time.Second,
		BackoffMultiplier: 2.0,
		JitterFraction:    0.25,
		MaxRetryDelayMs:   60000,
		ConnectTimeout:    30 * time.Second,
		RequestTimeout:    10 * time.Minute,
	}
}

// NoRetryConfig returns a config that disables retries entirely.
func NoRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     0,
		ConnectTimeout: 30 * time.Second,
		RequestTimeout: 10 * time.Minute,
	}
}

func (cfg *RetryConfig) applyDefaults() {
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = time.Second
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 60 * time.Second
	}
	if cfg.BackoffMultiplier <= 0 {
		cfg.BackoffMultiplier = 2.0
	}
	if cfg.JitterFraction < 0 {
		cfg.JitterFraction = 0
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 30 * time.Second
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 10 * time.Minute
	}
}

// NewHTTPClient creates an http.Client with the timeout settings from this config.
func (cfg *RetryConfig) NewHTTPClient() *http.Client {
	cfg.applyDefaults()
	transport := &http.Transport{}
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = defaultTransport.Clone()
	}
	transport.Proxy = proxyFromEnvironmentV0850
	transport.DialContext = (&net.Dialer{Timeout: cfg.ConnectTimeout}).DialContext
	transport.TLSHandshakeTimeout = cfg.ConnectTimeout
	transport.ResponseHeaderTimeout = cfg.ConnectTimeout
	return &http.Client{
		Timeout:   cfg.RequestTimeout,
		Transport: transport,
	}
}

// RetryConfigFromOptions resolves per-request retry behavior from StreamOptions.
//
// By default, requests do not retry unless RetryConfig is provided or legacy
// MaxRetryDelayMs is set.
func proxyFromEnvironmentV0850(req *http.Request) (*url.URL, error) {
	if req != nil && req.URL != nil && noProxyMatchV0850(req.URL.Hostname()) {
		return nil, nil
	}
	return http.ProxyFromEnvironment(req)
}

func noProxyMatchV0850(host string) bool {
	host = normalizeNoProxyHost(host)
	if host == "" {
		return false
	}
	raw := os.Getenv("NO_PROXY")
	if raw == "" {
		raw = os.Getenv("no_proxy")
	}
	for _, entry := range strings.Split(raw, ",") {
		entry = normalizeNoProxyHost(entry)
		if entry == "" {
			continue
		}
		if entry == "*" || host == entry || strings.HasSuffix(host, "."+entry) {
			return true
		}
		if strings.HasPrefix(entry, ".") && (host == strings.TrimPrefix(entry, ".") || strings.HasSuffix(host, entry)) {
			return true
		}
	}
	return false
}

func normalizeNoProxyHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	return host
}

func RetryConfigFromOptions(opts *StreamOptions) RetryConfig {
	if opts == nil {
		return NoRetryConfig()
	}
	if opts.RetryConfig != nil {
		cfg := *opts.RetryConfig
		if opts.MaxRetryDelayMs != nil && cfg.MaxRetryDelayMs == 0 {
			cfg.MaxRetryDelayMs = *opts.MaxRetryDelayMs
		}
		return cfg
	}
	if opts.MaxRetryDelayMs != nil {
		cfg := DefaultRetryConfig()
		cfg.MaxRetryDelayMs = *opts.MaxRetryDelayMs
		return cfg
	}
	return NoRetryConfig()
}

// DoWithRetry executes an HTTP request with retry logic.
//
// If all retries are exhausted on HTTP status responses, the final response is
// returned with a nil error so callers can inspect the provider error payload;
// callers remain responsible for closing resp.Body. Network/setup failures are
// returned as errors.
func DoWithRetry(ctx context.Context, client *http.Client, req *http.Request, cfg RetryConfig) (*http.Response, error) {
	cfg.applyDefaults()
	if client == nil {
		client = cfg.NewHTTPClient()
	}
	if req == nil {
		return nil, fmt.Errorf("nil request")
	}
	if req.Body != nil && req.GetBody == nil && cfg.MaxRetries > 0 {
		return nil, fmt.Errorf("retry requires request.GetBody for replayable request body")
	}

	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		attemptReq := req.Clone(ctx)
		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("clone request body: %w", err)
			}
			attemptReq.Body = body
		}

		resp, err := client.Do(attemptReq)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt < cfg.MaxRetries {
				delay := retryutil.ComputeBackoff(attempt, cfg.InitialDelay, cfg.MaxDelay, cfg.BackoffMultiplier, cfg.JitterFraction)
				logWarn("network error, retrying", "attempt", attempt+1, "maxRetries", cfg.MaxRetries, "delay", delay, "error", err)
				if cfg.OnRetry != nil {
					cfg.OnRetry(attempt, delay, 0)
				}
				sleep(ctx, delay)
				continue
			}
			logWarn("max retries exceeded", "kind", "network", "maxRetries", cfg.MaxRetries, "error", lastErr)
			return nil, fmt.Errorf("max retries exceeded (network): %w", lastErr)
		}

		if !cfg.isRetryable(resp.StatusCode) {
			return resp, nil
		}

		delay := retryutil.ParseRetryAfter(resp.Header)
		if delay <= 0 {
			delay = retryutil.ComputeBackoff(attempt, cfg.InitialDelay, cfg.MaxDelay, cfg.BackoffMultiplier, cfg.JitterFraction)
		}

		logWarn("retryable HTTP status", "status", resp.StatusCode, "attempt", attempt+1, "maxRetries", cfg.MaxRetries, "delay", delay)

		if cfg.MaxRetryDelayMs > 0 && delay > time.Duration(cfg.MaxRetryDelayMs)*time.Millisecond {
			logWarn("retry delay exceeds cap", "status", resp.StatusCode, "delay", delay, "capMs", cfg.MaxRetryDelayMs)
			resp.Body.Close()
			return nil, fmt.Errorf("server requested retry delay of %v exceeds cap of %dms (HTTP %d)",
				delay, cfg.MaxRetryDelayMs, resp.StatusCode)
		}

		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		if attempt < cfg.MaxRetries {
			resp.Body.Close()
			if cfg.OnRetry != nil {
				cfg.OnRetry(attempt, delay, resp.StatusCode)
			}
			sleep(ctx, delay)
			continue
		}

		logWarn("max retries exhausted; returning final response", "status", resp.StatusCode, "maxRetries", cfg.MaxRetries)
		return resp, nil
	}

	// Defensive fallback for future loop changes; current HTTP status paths
	// return either a non-retryable response or the final retryable response.
	logWarn("max retries exceeded", "kind", "http", "maxRetries", cfg.MaxRetries, "error", lastErr)
	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

func (cfg *RetryConfig) isRetryable(code int) bool {
	if cfg.RetryableStatuses != nil {
		for _, s := range cfg.RetryableStatuses {
			if s == code {
				return true
			}
		}
		return false
	}
	return retryutil.IsRetryableStatus(code)
}

func sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}
