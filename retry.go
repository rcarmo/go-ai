// HTTP retry logic with exponential backoff, jitter, and configurable limits.
package goai

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryConfig controls retry behavior with exponential backoff.
//
// Backoff formula: delay = min(InitialDelay * BackoffMultiplier^attempt * jitter, MaxDelay)
// where jitter is uniformly distributed in [1-JitterFraction, 1+JitterFraction].
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts (default: 3).
	// Set to 0 to disable retries entirely.
	MaxRetries int

	// InitialDelay is the base delay before the first retry (default: 1s).
	InitialDelay time.Duration

	// MaxDelay caps the computed backoff delay (default: 60s).
	// The actual delay may still exceed this if a server Retry-After header
	// requests a longer wait (subject to MaxRetryDelayMs).
	MaxDelay time.Duration

	// BackoffMultiplier is the exponential base (default: 2.0).
	// Set to 1.0 for constant-interval retries (with jitter).
	BackoffMultiplier float64

	// JitterFraction controls the jitter range as a fraction of the delay (default: 0.25).
	// 0.25 means ±25% jitter. Set to 0 for no jitter.
	JitterFraction float64

	// MaxRetryDelayMs caps the maximum delay the retry logic will honor from
	// a server Retry-After header. If the server requests a longer delay,
	// the request fails immediately with an error containing the requested delay.
	// This lets higher-level retry logic handle it with user visibility.
	// Default: 60000 (60 seconds). Set to 0 to disable the cap.
	MaxRetryDelayMs int

	// ConnectTimeout is the maximum time to wait for a TCP connection (default: 30s).
	// This is separate from the overall request timeout.
	ConnectTimeout time.Duration

	// RequestTimeout is the maximum time for the entire request including body read (default: 10m).
	// For streaming requests, this should be long enough to cover the full stream.
	RequestTimeout time.Duration

	// RetryableStatuses overrides the default set of retryable HTTP status codes.
	// Default: [429, 500, 502, 503, 504].
	// Set to a non-nil empty slice to retry on no status codes.
	RetryableStatuses []int

	// OnRetry is called before each retry attempt with the attempt number (0-based),
	// the delay that will be waited, and the HTTP status that triggered the retry.
	// Use for logging, metrics, or custom backoff logic.
	OnRetry func(attempt int, delay time.Duration, status int)
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

// applyDefaults fills zero-valued fields with defaults.
func (cfg *RetryConfig) applyDefaults() {
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
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = cfg.ConnectTimeout
	return &http.Client{
		Timeout:   cfg.RequestTimeout,
		Transport: transport,
	}
}

// DoWithRetry executes an HTTP request with retry logic.
// Retries on retryable status codes (default: 429, 500, 502, 503, 504).
// Respects Retry-After headers. Uses exponential backoff with jitter.
func DoWithRetry(ctx context.Context, client *http.Client, req *http.Request, cfg RetryConfig) (*http.Response, error) {
	cfg.applyDefaults()

	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt < cfg.MaxRetries {
				delay := computeBackoff(attempt, cfg)
				logWarn("network error, retrying", "attempt", attempt+1,
					"maxRetries", cfg.MaxRetries, "delay", delay, "error", err)
				if cfg.OnRetry != nil {
					cfg.OnRetry(attempt, delay, 0)
				}
				sleep(ctx, delay)
				continue
			}
			return nil, fmt.Errorf("max retries exceeded (network): %w", lastErr)
		}

		if !cfg.isRetryableStatus(resp.StatusCode) {
			return resp, nil
		}

		// Compute delay: prefer server Retry-After, fallback to exponential backoff
		delay := parseRetryAfter(resp.Header)
		if delay <= 0 {
			delay = computeBackoff(attempt, cfg)
		}

		logWarn("retryable HTTP status", "status", resp.StatusCode, "attempt", attempt+1,
			"maxRetries", cfg.MaxRetries, "delay", delay)

		// Check if server-requested delay exceeds our cap
		if cfg.MaxRetryDelayMs > 0 && delay > time.Duration(cfg.MaxRetryDelayMs)*time.Millisecond {
			resp.Body.Close()
			return nil, fmt.Errorf("server requested retry delay of %v exceeds cap of %dms (HTTP %d)",
				delay, cfg.MaxRetryDelayMs, resp.StatusCode)
		}

		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		resp.Body.Close()

		if attempt < cfg.MaxRetries {
			if cfg.OnRetry != nil {
				cfg.OnRetry(attempt, delay, resp.StatusCode)
			}
			sleep(ctx, delay)
		}
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

func (cfg *RetryConfig) isRetryableStatus(code int) bool {
	if cfg.RetryableStatuses != nil {
		for _, s := range cfg.RetryableStatuses {
			if s == code {
				return true
			}
		}
		return false
	}
	return isRetryableStatusDefault(code)
}

func isRetryableStatusDefault(code int) bool {
	switch code {
	case 429, 500, 502, 503, 504:
		return true
	}
	return false
}

// computeBackoff calculates the delay for a retry attempt using exponential
// backoff with configurable multiplier and jitter.
func computeBackoff(attempt int, cfg RetryConfig) time.Duration {
	delay := float64(cfg.InitialDelay) * math.Pow(cfg.BackoffMultiplier, float64(attempt))

	// Apply jitter
	if cfg.JitterFraction > 0 {
		jitterRange := cfg.JitterFraction * 2
		jitter := (1 - cfg.JitterFraction) + rand.Float64()*jitterRange
		delay *= jitter
	}

	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}

	return time.Duration(delay)
}

func parseRetryAfter(headers http.Header) time.Duration {
	// Standard Retry-After header
	val := headers.Get("Retry-After")
	if val != "" {
		// Try as seconds
		if secs, err := strconv.ParseFloat(val, 64); err == nil {
			return time.Duration(secs * float64(time.Second))
		}
		// Try as HTTP-date
		if t, err := http.ParseTime(val); err == nil {
			delay := time.Until(t)
			if delay > 0 {
				return delay
			}
		}
	}

	// Rate limit reset headers (various providers)
	for _, header := range []string{
		"x-ratelimit-reset-after",
		"x-ratelimit-reset-requests",
		"x-ratelimit-reset-tokens",
	} {
		if v := headers.Get(header); v != "" {
			if d := parseDurationString(v); d > 0 {
				return d
			}
		}
	}

	return 0
}

// parseDurationString handles various delay formats:
//
//	"5" → 5 seconds
//	"5s" → 5 seconds
//	"500ms" → 500 milliseconds
//	"1m30s" → 1 minute 30 seconds
func parseDurationString(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	// Try Go duration format first (handles "1m30s", "500ms", "2s", etc.)
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}

	// Try as plain number (seconds)
	if secs, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(secs * float64(time.Second))
	}

	return 0
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
