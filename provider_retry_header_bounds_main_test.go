package goai

import (
	"context"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Source-derived from the server-delay validation in provider-retry.ts at v1.5.0.
func TestMainProviderRetryServerDelayBounds(t *testing.T) {
	for _, tc := range []struct {
		name, header, value string
		options             ProviderRetryOptions
		want                time.Duration
		wantError           bool
	}{
		{"millisecond-over-cap", "retry-after-ms", "1e100", ProviderRetryOptions{}, 0, true},
		{"second-over-cap", "retry-after", "1e100", ProviderRetryOptions{}, 0, true},
		{"large-ms-cap-disabled", "retry-after-ms", "1e100", ProviderRetryOptions{DisableDelayCap: true}, time.Duration(math.MaxInt64), false},
		{"large-seconds-cap-disabled", "retry-after", "1e100", ProviderRetryOptions{DisableDelayCap: true}, time.Duration(math.MaxInt64), false},
		{"large-negative-ms", "retry-after-ms", "-1e100", ProviderRetryOptions{}, time.Duration(math.MinInt64), false},
		{"fractional-ms", "retry-after-ms", "1.5", ProviderRetryOptions{}, 1500 * time.Microsecond, false},
		{"at-cap", "retry-after", "1", ProviderRetryOptions{MaxRetryDelay: time.Second}, time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := &ProviderRequestError{Status: 429, Headers: http.Header{}}
			err.Headers.Set(tc.header, tc.value)
			got, delayErr := providerRetryDelay(err, 0, tc.options)
			if (delayErr != nil) != tc.wantError || (!tc.wantError && got != tc.want) {
				t.Fatalf("got (%v, %v), want (%v, error=%v)", got, delayErr, tc.want, tc.wantError)
			}
		})
	}
}

func TestMainProviderRetryHugeServerDelayStopsBeforeRetry(t *testing.T) {
	calls := 0
	_, err := RetryProviderRequest(context.Background(), func() (string, error) {
		calls++
		if calls > 1 {
			return "incorrect retry", nil
		}
		header := http.Header{}
		header.Set("retry-after", "1e100")
		return "", &ProviderRequestError{Status: 429, Headers: header, Message: "busy"}
	}, ProviderRetryOptions{MaxRetries: 1})
	if err == nil || !strings.Contains(err.Error(), "retry delay") || calls != 1 {
		t.Fatalf("calls=%d, error=%v; want one call and delay-cap error", calls, err)
	}
}
