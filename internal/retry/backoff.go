// Package retry provides HTTP retry helpers with exponential backoff.
package retry

import (
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ComputeBackoff calculates the delay for a retry attempt.
// Formula: min(initialDelay * multiplier^attempt * jitter, maxDelay)
func ComputeBackoff(attempt int, initialDelay, maxDelay time.Duration, multiplier, jitterFraction float64) time.Duration {
	delay := float64(initialDelay) * math.Pow(multiplier, float64(attempt))
	if jitterFraction > 0 {
		jitterRange := jitterFraction * 2
		jitter := (1 - jitterFraction) + rand.Float64()*jitterRange
		delay *= jitter
	}
	if delay > float64(maxDelay) {
		delay = float64(maxDelay)
	}
	return time.Duration(delay)
}

// ParseRetryAfter extracts a retry delay from HTTP response headers.
// Checks Retry-After, x-ratelimit-reset-after, and related headers.
func ParseRetryAfter(headers http.Header) time.Duration {
	val := headers.Get("Retry-After")
	if val != "" {
		if secs, err := strconv.ParseFloat(val, 64); err == nil {
			return time.Duration(secs * float64(time.Second))
		}
		if t, err := http.ParseTime(val); err == nil {
			delay := time.Until(t)
			if delay > 0 {
				return delay
			}
		}
	}
	for _, header := range []string{
		"x-ratelimit-reset-after",
		"x-ratelimit-reset-requests",
		"x-ratelimit-reset-tokens",
	} {
		if v := headers.Get(header); v != "" {
			if d := ParseDurationString(v); d > 0 {
				return d
			}
		}
	}
	return 0
}

// ParseDurationString handles "5", "5s", "500ms", "1m30s" formats.
func ParseDurationString(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	if secs, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(secs * float64(time.Second))
	}
	return 0
}

// IsRetryableStatus returns true for status codes that should be retried.
func IsRetryableStatus(code int) bool {
	switch code {
	case 429, 500, 502, 503, 504:
		return true
	}
	return false
}
