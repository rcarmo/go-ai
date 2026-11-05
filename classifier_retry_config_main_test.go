package goai

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMainClassifierTimeoutPresence(t *testing.T) {
	zero, ten, negative := 0, 10, -1
	for _, tc := range []struct {
		name      string
		opts      *ClassifierOptions
		want      time.Duration
		wantError bool
	}{
		{"omitted", nil, 0, false},
		{"zero", &ClassifierOptions{Timeout: time.Second, TimeoutMsOverride: &zero}, time.Nanosecond, false},
		{"positive", &ClassifierOptions{TimeoutMsOverride: &ten}, 10 * time.Millisecond, false},
		{"legacy", &ClassifierOptions{TimeoutMs: 20}, 20 * time.Millisecond, false},
		{"negative", &ClassifierOptions{TimeoutMsOverride: &negative}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := classifierRequestTimeout(tc.opts)
			if got != tc.want || (err != nil) != tc.wantError {
				t.Fatalf("got (%v, %v), want (%v, error=%v)", got, err, tc.want, tc.wantError)
			}
		})
	}
}

func TestMainClassifierRetryDefaultsAndExplicitZero(t *testing.T) {
	zero, one := 0, 1
	for _, tc := range []struct {
		name           string
		opts           *ClassifierOptions
		retries, delay int
	}{
		{"nil", nil, 2, 60000},
		{"empty", &ClassifierOptions{}, 2, 60000},
		{"legacy-positive", &ClassifierOptions{MaxRetries: 1, MaxRetryDelayMs: 1200}, 1, 1200},
		{"zero-retries", &ClassifierOptions{MaxRetriesOverride: &zero}, 0, 60000},
		{"zero-cap", &ClassifierOptions{MaxRetryDelayMsOverride: &zero}, 2, 0},
		{"override-precedence", &ClassifierOptions{MaxRetries: 3, MaxRetriesOverride: &one}, 1, 60000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := classifierRetryConfig(tc.opts)
			if cfg.MaxRetries != tc.retries || cfg.MaxRetryDelayMs != tc.delay {
				t.Fatalf("got retries=%d cap=%d, want %d/%d", cfg.MaxRetries, cfg.MaxRetryDelayMs, tc.retries, tc.delay)
			}
			if cfg.MaxRetryDelayMs != 0 && time.Duration(cfg.MaxRetryDelayMs)*time.Millisecond <= 0 {
				t.Fatal("expected positive cap")
			}
		})
	}
}

// The public HTTP adapter must not silently disable a large positive cap
// when converting milliseconds to time.Duration.
func TestMainProviderRetryConfiguredCapConversion(t *testing.T) {
	maxDurationMs := int64((1<<63 - 1) / int64(time.Millisecond))
	if int64(int(maxDurationMs+1)) != maxDurationMs+1 {
		t.Skip("int too narrow to exercise duration overflow")
	}
	calls := 0
	client := &http.Client{Transport: retryCapTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After-Ms": []string{strconv.FormatInt(maxDurationMs+10000, 10)}}, Body: io.NopCloser(strings.NewReader("busy"))}, nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid", nil)
	cfg := NoRetryConfig()
	cfg.MaxRetries, cfg.MaxRetryDelayMs = 1, int(maxDurationMs+1)
	_, err := DoProviderRequestWithRetry(ctx, client, req, cfg)
	if err == nil || !strings.Contains(err.Error(), "server requested") || calls != 1 {
		t.Fatalf("got error=%v calls=%d; want cap rejection on first attempt", err, calls)
	}
}

type retryCapTransport func(*http.Request) (*http.Response, error)

func (f retryCapTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
