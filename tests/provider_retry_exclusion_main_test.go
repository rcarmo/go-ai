package goai_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Pinned provider-retry.test.ts:40: an excluded status returns the original
// provider error after one attempt, even if the server requests a retry.
func TestMainProviderRetrySourceNoRetryStatuses(t *testing.T) {
	for _, header := range []string{"", "true", "false"} {
		t.Run("header-"+header, func(t *testing.T) {
			attempts := 0
			failure := &goai.ProviderRequestError{Status: 504, Headers: http.Header{}, Message: "timed out"}
			if header != "" {
				failure.Headers.Set("x-should-retry", header)
			}
			_, err := goai.RetryProviderRequest(context.Background(), func() (string, error) { attempts++; return "", failure }, goai.ProviderRetryOptions{MaxRetries: 2, NoRetryStatuses: []int{504}})
			if err != failure || attempts != 1 {
				t.Fatalf("attempts=%d err=%v", attempts, err)
			}
		})
	}
	attempts := 0
	value, err := goai.RetryProviderRequest(context.Background(), func() (string, error) {
		attempts++
		if attempts == 1 {
			return "", &goai.ProviderRequestError{Status: 503, Headers: http.Header{"Retry-After-Ms": []string{"0"}}}
		}
		return "ok", nil
	}, goai.ProviderRetryOptions{MaxRetries: 1, NoRetryStatuses: []int{504}})
	if err != nil || value != "ok" || attempts != 2 {
		t.Fatalf("other status value=%q attempts=%d err=%v", value, attempts, err)
	}
	marker := errors.New("ordinary failure")
	attempts = 0
	_, err = goai.RetryProviderRequest(context.Background(), func() (string, error) { attempts++; return "", marker }, goai.ProviderRetryOptions{MaxRetries: 2, NoRetryStatuses: []int{504}})
	if err != marker || attempts != 1 {
		t.Fatalf("ordinary error attempts=%d err=%v", attempts, err)
	}
}
