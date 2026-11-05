package retry_test

import (
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/internal/retry"
)

func TestComputeBackoff(t *testing.T) {
	// Attempt 0: 1s * 2^0 = 1s (±jitter)
	d := retry.ComputeBackoff(0, time.Second, time.Minute, 2.0, 0)
	if d != time.Second {
		t.Fatalf("expected 1s without jitter, got %v", d)
	}

	// Attempt 2: 1s * 2^2 = 4s (no jitter)
	d = retry.ComputeBackoff(2, time.Second, time.Minute, 2.0, 0)
	if d != 4*time.Second {
		t.Fatalf("expected 4s, got %v", d)
	}

	// Max delay cap
	d = retry.ComputeBackoff(10, time.Second, 5*time.Second, 2.0, 0)
	if d != 5*time.Second {
		t.Fatalf("expected 5s cap, got %v", d)
	}

	// Jitter: result should be within ±25% of base
	base := float64(time.Second)
	for i := 0; i < 100; i++ {
		d = retry.ComputeBackoff(0, time.Second, time.Minute, 2.0, 0.25)
		if float64(d) < base*0.75 || float64(d) > base*1.25 {
			t.Fatalf("jitter out of range: %v", d)
		}
	}
}

func TestComputeBackoffConstant(t *testing.T) {
	// Multiplier 1.0 = constant delay
	for attempt := 0; attempt < 5; attempt++ {
		d := retry.ComputeBackoff(attempt, 2*time.Second, time.Minute, 1.0, 0)
		if d != 2*time.Second {
			t.Fatalf("attempt %d: expected 2s constant, got %v", attempt, d)
		}
	}
}

func TestIsRetryableStatus(t *testing.T) {
	retryable := []int{429, 500, 502, 503, 504}
	for _, code := range retryable {
		if !retry.IsRetryableStatus(code) {
			t.Fatalf("expected %d to be retryable", code)
		}
	}
	notRetryable := []int{200, 201, 301, 400, 401, 403, 404, 422}
	for _, code := range notRetryable {
		if retry.IsRetryableStatus(code) {
			t.Fatalf("expected %d to NOT be retryable", code)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	// Seconds
	h := http.Header{}
	h.Set("Retry-After", "5")
	d := retry.ParseRetryAfter(h)
	if d != 5*time.Second {
		t.Fatalf("expected 5s, got %v", d)
	}

	// Empty
	h2 := http.Header{}
	if retry.ParseRetryAfter(h2) != 0 {
		t.Fatal("expected 0 for empty headers")
	}

	// x-ratelimit-reset-after
	h3 := http.Header{}
	h3.Set("x-ratelimit-reset-after", "2.5")
	d3 := retry.ParseRetryAfter(h3)
	if d3 < 2*time.Second || d3 > 3*time.Second {
		t.Fatalf("expected ~2.5s, got %v", d3)
	}
}

func TestParseDurationString(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"5", 5 * time.Second},
		{"5s", 5 * time.Second},
		{"500ms", 500 * time.Millisecond},
		{"1m30s", 90 * time.Second},
		{"", 0},
		{"invalid", 0},
	}
	for _, tt := range tests {
		got := retry.ParseDurationString(tt.input)
		if got != tt.expected {
			t.Errorf("ParseDurationString(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestParseRetryAfterHugeAndNonFinite(t *testing.T) {
	for _, tc := range []struct {
		name   string
		header http.Header
		want   time.Duration
	}{
		{"huge-seconds", http.Header{"Retry-After": []string{"1e100"}}, time.Duration(math.MaxInt64)},
		{"huge-reset", http.Header{"X-Ratelimit-Reset-After": []string{"1e100"}}, time.Duration(math.MaxInt64)},
		{"infinity-fallback", http.Header{"Retry-After": []string{"Infinity"}, "X-Ratelimit-Reset-After": []string{"2"}}, 2 * time.Second},
		{"nan-fallback", http.Header{"Retry-After": []string{"NaN"}, "X-Ratelimit-Reset-After": []string{"2"}}, 2 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := retry.ParseRetryAfter(tc.header); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestComputeBackoffSaturatesBeforeDurationConversion(t *testing.T) {
	max := time.Duration(math.MaxInt64)
	for _, tc := range []struct {
		name         string
		attempt      int
		initial, cap time.Duration
		multiplier   float64
		want         time.Duration
	}{
		{"max-duration", 0, max, max, 1, max},
		{"large-exponent", 10000, time.Second, max, 2, max},
		{"normal-cap", 10000, time.Second, 8 * time.Second, 2, 8 * time.Second},
		{"decaying", 4, 8 * time.Second, max, 0.5, 500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := retry.ComputeBackoff(tc.attempt, tc.initial, tc.cap, tc.multiplier, 0); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
