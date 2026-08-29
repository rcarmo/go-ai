package goai_test

import (
	"context"
	"net/http"
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
