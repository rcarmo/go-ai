package goai_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestRetryProviderRequestRetriesRetryableProviderErrors(t *testing.T) {
	attempts := 0
	got, err := goai.RetryProviderRequest(context.Background(), func() (string, error) {
		attempts++
		if attempts == 1 {
			return "", &goai.ProviderRequestError{Status: 429, Headers: http.Header{"Retry-After-Ms": []string{"0"}}, Message: "throttled"}
		}
		return "ok", nil
	}, goai.ProviderRetryOptions{MaxRetries: 1})
	if err != nil || got != "ok" || attempts != 2 {
		t.Fatalf("got=%q attempts=%d err=%v", got, attempts, err)
	}
}

func TestRetryProviderRequestRetriesDNSLikeTransportFailures(t *testing.T) {
	attempts := 0
	got, err := goai.RetryProviderRequest(context.Background(), func() (string, error) {
		attempts++
		if attempts == 1 {
			return "", &goai.ProviderRequestError{Status: 0, Headers: http.Header{}, Message: "lookup api.example: no such host"}
		}
		return "ok", nil
	}, goai.ProviderRetryOptions{MaxRetries: 1})
	if err != nil || got != "ok" || attempts != 2 {
		t.Fatalf("got=%q attempts=%d err=%v", got, attempts, err)
	}
}

func TestRetryProviderRequestHonorsNonRetryableHeaderAndDelayCap(t *testing.T) {
	attempts := 0
	_, err := goai.RetryProviderRequest(context.Background(), func() (string, error) {
		attempts++
		return "", &goai.ProviderRequestError{Status: 429, Headers: http.Header{"X-Should-Retry": []string{"false"}}, Message: "no retry"}
	}, goai.ProviderRetryOptions{MaxRetries: 2})
	if err == nil || attempts != 1 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}

	_, err = goai.RetryProviderRequest(context.Background(), func() (string, error) {
		return "", &goai.ProviderRequestError{Status: 429, Headers: http.Header{"Retry-After": []string{"277403"}}, Message: "too long"}
	}, goai.ProviderRetryOptions{MaxRetries: 1, MaxRetryDelay: time.Second})
	if err == nil || !strings.Contains(err.Error(), "server requested 277403s retry delay") {
		t.Fatalf("expected delay cap error, got %v", err)
	}
}

func TestDoProviderRequestWithRetryFallsBackForNonFiniteRetryAfterHeader(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "Infinity")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("retry later"))
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := goai.DoProviderRequestWithRetry(context.Background(), server.Client(), req, goai.RetryConfig{MaxRetries: 1, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, JitterFraction: 0, MaxRetryDelayMs: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" || attempts.Load() != 2 {
		t.Fatalf("body=%q attempts=%d", string(body), attempts.Load())
	}
}

func TestRetryProviderRequestAbortsRetryDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var attempts atomic.Int32
	result := make(chan error, 1)
	go func() {
		_, err := goai.RetryProviderRequest(ctx, func() (string, error) {
			attempts.Add(1)
			return "", &goai.ProviderRequestError{Status: 429, Headers: http.Header{"Retry-After": []string{"60"}}, Message: "retry later"}
		}, goai.ProviderRetryOptions{MaxRetries: 2, DisableDelayCap: true})
		result <- err
	}()
	for attempts.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "aborted") || attempts.Load() != 1 {
			t.Fatalf("attempts=%d err=%v", attempts.Load(), err)
		}
	case <-time.After(time.Second):
		t.Fatal("retry delay did not abort")
	}
}
