package goai_test

import (
	"context"
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
