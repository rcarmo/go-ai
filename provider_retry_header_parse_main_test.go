package goai

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Source-derived from Number.parseFloat in provider-retry.ts at v1.5.0.
func TestMainProviderRetryHeaderNumericPrefixes(t *testing.T) {
	for _, tc := range []struct {
		name, header, value string
		want                time.Duration
	}{
		{"milliseconds-suffix", "retry-after-ms", "2.5ms", 2500 * time.Microsecond},
		{"seconds-suffix", "retry-after", "1.25 seconds", 1250 * time.Millisecond},
		{"leading-whitespace", "retry-after-ms", "  \t+3e1junk", 30 * time.Millisecond},
		{"incomplete-exponent", "retry-after-ms", "2e+oops", 2 * time.Millisecond},
		{"trailing-dot", "retry-after-ms", "3.foo", 3 * time.Millisecond},
		{"negative-prefix", "retry-after-ms", "-1ms", -time.Millisecond},
		{"hex-prefix", "retry-after-ms", "0x10", 0},
		{"unicode-whitespace", "retry-after-ms", "\ufeff\u00a0.5ms", 500 * time.Microsecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header := http.Header{}
			header.Set(tc.header, tc.value)
			got, err := providerRetryDelay(&ProviderRequestError{Status: 429, Headers: header}, 0, ProviderRetryOptions{})
			if err != nil || got != tc.want {
				t.Fatalf("got (%v, %v), want %v", got, err, tc.want)
			}
		})
	}
}

// U+0085 is Go whitespace but is not ECMAScript WhiteSpace or LineTerminator.
func TestMainProviderRetryHeaderECMAScriptWhitespace(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        time.Duration
	}{
		{"next-line-falls-through", "\u00852ms", 4 * time.Millisecond},
		{"no-break-space-trimmed", "\u00a02ms", 2 * time.Millisecond},
		{"byte-order-mark-trimmed", "\ufeff2ms", 2 * time.Millisecond},
		{"line-separator-trimmed", "\u20282ms", 2 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			h.Set("retry-after-ms", tc.value)
			h.Set("retry-after", "0.004")
			got, err := providerRetryDelay(&ProviderRequestError{Status: 429, Headers: h}, 0, ProviderRetryOptions{})
			if err != nil || got != tc.want {
				t.Fatalf("got (%v, %v), want %v", got, err, tc.want)
			}
		})
	}
}

// Source-derived Date.parse offset cases. Compare absolute instants as well as
// the cap outcome so accepting a date in the wrong timezone cannot pass.
func TestMainProviderRetryHeaderGMTOffsetDate(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Time
	}{
		{"January 1, 2100 00:00:00 GMT+0100", time.Date(2099, 12, 31, 23, 0, 0, 0, time.UTC)},
		{"Fri, 01 Jan 2100 00:00:00 GMT+0100", time.Date(2099, 12, 31, 23, 0, 0, 0, time.UTC)},
		{"January 1, 2100 00:00:00 GMT-0230", time.Date(2100, 1, 1, 2, 30, 0, 0, time.UTC)},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got, err := parseProviderRetryDate(tc.value)
			if err != nil || !got.Equal(tc.want) {
				t.Fatalf("parse got %v, %v; want %v", got, err, tc.want)
			}
			header := http.Header{"Retry-After": []string{tc.value}}
			_, err = providerRetryDelay(&ProviderRequestError{Status: 429, Headers: header, Message: "busy"}, 0, ProviderRetryOptions{MaxRetryDelay: time.Second})
			if err == nil || !strings.Contains(err.Error(), "retry delay") {
				t.Fatalf("delay got %v; want server delay-cap error", err)
			}
		})
	}
}

// Source-derived from the Retry-After Date.parse fallback. The long month
// form is accepted by the pinned JavaScript runtime but not net/http.ParseTime.
func TestMainProviderRetryHeaderLongMonthDate(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "January 1, 3000 00:00:00 GMT")
	_, err := providerRetryDelay(&ProviderRequestError{Status: 429, Headers: h, Message: "busy"}, 0, ProviderRetryOptions{MaxRetryDelay: time.Second})
	if err == nil || !strings.Contains(err.Error(), "retry delay") {
		t.Fatalf("got %v; want a server delay-cap error", err)
	}
}

func TestMainProviderRetryHeaderNonFiniteFallsThrough(t *testing.T) {
	for _, value := range []string{"Infinity", "-Infinitysuffix", "1e9999"} {
		t.Run(value, func(t *testing.T) {
			h := http.Header{}
			h.Set("retry-after-ms", value)
			h.Set("retry-after", "2ms")
			got, err := providerRetryDelay(&ProviderRequestError{Status: 429, Headers: h}, 0, ProviderRetryOptions{})
			if err != nil || got != 2*time.Second {
				t.Fatalf("got (%v, %v), want 2s", got, err)
			}
		})
	}
}

func TestMainProviderRetryHeaderPrefixEnforcesCap(t *testing.T) {
	calls := 0
	_, err := RetryProviderRequest(context.Background(), func() (string, error) {
		calls++
		if calls > 1 {
			return "incorrect retry", nil
		}
		h := http.Header{}
		h.Set("retry-after", "277403 seconds")
		return "", &ProviderRequestError{Status: 429, Headers: h, Message: "busy"}
	}, ProviderRetryOptions{MaxRetries: 1, MaxRetryDelay: time.Second})
	if err == nil || !strings.Contains(err.Error(), "277403s retry delay") || calls != 1 {
		t.Fatalf("calls=%d, error=%v; want one call and server delay-cap error", calls, err)
	}
}
