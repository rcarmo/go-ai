package goai_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type providerRetryTransportMain func(*http.Request) (*http.Response, error)

func (f providerRetryTransportMain) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type providerRetryBodyMain struct {
	io.Reader
	closed int
}

func (b *providerRetryBodyMain) Close() error { b.closed++; return nil }

// OnRetry is a native RetryConfig contract, not an upstream provider callback.
// It reports an admitted retry and its actual delay, including network errors.
func TestProviderHTTPRetryCallbackMain(t *testing.T) {
	for _, tc := range []struct {
		name       string
		transport  bool
		capError   bool
		cancel     bool
		wantCalls  int
		wantEvents int
		wantDelay  time.Duration
		wantStatus int
	}{
		{name: "http", wantCalls: 2, wantEvents: 1, wantDelay: 3 * time.Millisecond, wantStatus: 429},
		{name: "transport", transport: true, wantCalls: 2, wantEvents: 1, wantDelay: -1, wantStatus: 0},
		{name: "cap-rejects", capError: true, wantCalls: 1},
		{name: "callback-cancels", cancel: true, wantCalls: 1, wantEvents: 1, wantDelay: 3 * time.Millisecond, wantStatus: 429},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			calls, events := 0, 0
			client := &http.Client{Transport: providerRetryTransportMain(func(_ *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					if tc.transport {
						return nil, fmt.Errorf("transport failure")
					}
					delay := "3"
					if tc.capError {
						delay = "2000"
					}
					return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After-Ms": []string{delay}}, Body: io.NopCloser(strings.NewReader("busy"))}, nil
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
			})}
			cfg := goai.NoRetryConfig()
			cfg.MaxRetries, cfg.MaxRetryDelayMs = 1, 1000
			cfg.OnRetry = func(attempt int, delay time.Duration, status int) {
				events++
				if attempt != 0 || status != tc.wantStatus || (tc.wantDelay >= 0 && delay != tc.wantDelay) || (tc.wantDelay < 0 && (delay < 375*time.Millisecond || delay > 500*time.Millisecond)) {
					t.Errorf("callback got attempt=%d delay=%s status=%d", attempt, delay, status)
				}
				if tc.cancel {
					cancel()
				}
			}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid", nil)
			resp, err := goai.DoProviderRequestWithRetry(ctx, client, req, cfg)
			if resp != nil {
				resp.Body.Close()
			}
			if (tc.capError || tc.cancel) != (err != nil) {
				t.Fatalf("unexpected result: response=%v error=%v", resp, err)
			}
			if calls != tc.wantCalls || events != tc.wantEvents {
				t.Fatalf("calls=%d events=%d; want %d/%d", calls, events, tc.wantCalls, tc.wantEvents)
			}
		})
	}
}

// Transport and HTTP failures share one retry budget; the final response body
// remains caller-owned even if earlier attempts failed before a response.
func TestProviderHTTPRetryMixedAttemptOwnershipMain(t *testing.T) {
	for _, tc := range []struct {
		name     string
		statuses []int // 0 is a transport failure
	}{
		{"transport-then-http", []int{0, 429}},
		{"http-transport-http", []int{429, 0, 503}},
		{"transport-then-success", []int{0, 200}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			bodies := make([]*providerRetryBodyMain, 0, len(tc.statuses))
			client := &http.Client{Transport: providerRetryTransportMain(func(_ *http.Request) (*http.Response, error) {
				if calls >= len(tc.statuses) {
					return nil, fmt.Errorf("unexpected additional attempt")
				}
				status := tc.statuses[calls]
				calls++
				if status == 0 {
					return nil, fmt.Errorf("transport failure")
				}
				body := &providerRetryBodyMain{Reader: strings.NewReader("response payload")}
				bodies = append(bodies, body)
				return &http.Response{StatusCode: status, Header: http.Header{"Retry-After-Ms": []string{"0"}}, Body: body}, nil
			})}
			cfg := goai.NoRetryConfig()
			cfg.MaxRetries, cfg.MaxRetryDelayMs = len(tc.statuses)-1, 1000
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid", nil)
			response, err := goai.DoProviderRequestWithRetry(ctx, client, req, cfg)
			if err != nil || response == nil {
				t.Fatalf("expected final response; got response=%v error=%v", response, err)
			}
			defer response.Body.Close()
			if calls != len(tc.statuses) || response.StatusCode != tc.statuses[len(tc.statuses)-1] {
				t.Fatalf("calls=%d status=%d", calls, response.StatusCode)
			}
			for i, body := range bodies {
				wantClosed := 1
				if i == len(bodies)-1 {
					wantClosed = 0
				}
				if body.closed != wantClosed {
					t.Fatalf("body %d closed=%d want %d", i, body.closed, wantClosed)
				}
			}
			payload, err := io.ReadAll(response.Body)
			if err != nil || string(payload) != "response payload" {
				t.Fatalf("final body=%q error=%v", payload, err)
			}
		})
	}
}

func TestProviderHTTPRetryDirectiveAndOwnershipMain(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		directive  string
		custom     []int
		retry      bool
		headersNil bool
	}{
		{name: "force-error", status: 400, directive: "true", retry: true},
		{name: "veto", status: 429, directive: "false"},
		{name: "success-not-retried", status: 200, directive: "true"},
		{name: "native-allow", status: 400, custom: []int{400}, retry: true},
		{name: "native-deny-force", status: 400, custom: []int{429}, directive: "true"},
		{name: "native-allow-veto", status: 400, custom: []int{400}, directive: "false"},
		{name: "headers-less", status: 429, retry: true, headersNil: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			first := &providerRetryBodyMain{Reader: strings.NewReader("original payload")}
			headers := http.Header{"Retry-After": []string{"0"}}
			if tc.directive != "" {
				headers.Set("X-Should-Retry", tc.directive)
			}
			if tc.headersNil {
				headers = nil
			}
			client := &http.Client{Transport: providerRetryTransportMain(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: tc.status, Header: headers, Body: first}, nil
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("success"))}, nil
			})}
			cfg := goai.NoRetryConfig()
			cfg.MaxRetries = 1
			cfg.MaxRetryDelayMs = 60000
			cfg.RetryableStatuses = tc.custom
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "POST", "http://example.invalid", strings.NewReader("request"))
			response, err := goai.DoProviderRequestWithRetry(ctx, client, req, cfg)
			if err != nil || response == nil {
				t.Fatal("request failed", err)
			}
			defer response.Body.Close()
			wantCalls := 1
			wantPayload := "original payload"
			if tc.retry {
				wantCalls = 2
				wantPayload = "success"
			}
			if calls != wantCalls {
				t.Fatal("wrong attempt count", calls, wantCalls)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil || string(body) != wantPayload {
				t.Fatal("final payload lost", string(body), err)
			}
			closed := 0
			if tc.retry {
				closed = 1
			}
			if first.closed != closed {
				t.Fatal("wrong close ownership", first.closed, closed)
			}
			if !tc.headersNil && headers.Get("X-Should-Retry") != tc.directive {
				t.Fatal("server headers mutated", headers)
			}
		})
	}
}
