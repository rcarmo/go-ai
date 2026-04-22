// HTTP retry logic with exponential backoff and Retry-After header support.
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

// RetryConfig controls retry behavior.
type RetryConfig struct {
	MaxRetries      int           // max number of retries (default: 3)
	InitialDelay    time.Duration // initial backoff delay (default: 1s)
	MaxDelay        time.Duration // max backoff delay (default: 60s)
	MaxRetryDelayMs int           // cap on server-requested delay; 0 = no cap
}

// DefaultRetryConfig returns sensible defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:      3,
		InitialDelay:    time.Second,
		MaxDelay:        60 * time.Second,
		MaxRetryDelayMs: 60000,
	}
}

// DoWithRetry executes an HTTP request with retry logic.
// Retries on 429, 500, 502, 503, 504. Respects Retry-After header.
func DoWithRetry(ctx context.Context, client *http.Client, req *http.Request, cfg RetryConfig) (*http.Response, error) {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = time.Second
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 60 * time.Second
	}

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
				sleep(ctx, backoff(attempt, cfg))
				continue
			}
			return nil, lastErr
		}

		if !isRetryableStatus(resp.StatusCode) {
			return resp, nil
		}

		// Check Retry-After header
		delay := parseRetryAfter(resp.Header)
		if delay <= 0 {
			delay = backoff(attempt, cfg)
		}

		logWarn("retryable HTTP status", "status", resp.StatusCode, "attempt", attempt+1,
			"maxRetries", cfg.MaxRetries, "delay", delay)

		// Check if server-requested delay exceeds our cap
		if cfg.MaxRetryDelayMs > 0 && delay > time.Duration(cfg.MaxRetryDelayMs)*time.Millisecond {
			return nil, fmt.Errorf("server requested retry delay of %v exceeds cap of %dms (HTTP %d)",
				delay, cfg.MaxRetryDelayMs, resp.StatusCode)
		}

		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		resp.Body.Close()

		if attempt < cfg.MaxRetries {
			sleep(ctx, delay)
		}
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

func isRetryableStatus(code int) bool {
	switch code {
	case 429, 500, 502, 503, 504:
		return true
	}
	return false
}

func backoff(attempt int, cfg RetryConfig) time.Duration {
	delay := float64(cfg.InitialDelay) * math.Pow(2, float64(attempt))
	// Add jitter (±25%)
	jitter := 0.75 + rand.Float64()*0.5
	delay *= jitter
	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}
	return time.Duration(delay)
}

func parseRetryAfter(headers http.Header) time.Duration {
	val := headers.Get("Retry-After")
	if val == "" {
		return 0
	}
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

	// Try x-ratelimit-reset-after style
	val = headers.Get("x-ratelimit-reset-after")
	if val != "" {
		if secs, err := strconv.ParseFloat(val, 64); err == nil {
			return time.Duration(secs * float64(time.Second))
		}
	}

	// Try parsing "Xs" or "Xms" from error body patterns
	if strings.HasSuffix(val, "ms") {
		if ms, err := strconv.ParseFloat(strings.TrimSuffix(val, "ms"), 64); err == nil {
			return time.Duration(ms * float64(time.Millisecond))
		}
	}
	if strings.HasSuffix(val, "s") {
		if secs, err := strconv.ParseFloat(strings.TrimSuffix(val, "s"), 64); err == nil {
			return time.Duration(secs * float64(time.Second))
		}
	}

	return 0
}

func sleep(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}
