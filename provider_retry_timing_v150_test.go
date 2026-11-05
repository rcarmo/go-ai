package goai

import (
	"context"
	"net/http"
	"testing"
	"testing/synctest"
	"time"
)

// Translate the exact 999ms/1ms boundary of upstream provider-retry.test.ts
// through the public generic retry path, using Go's isolated virtual clock.
func TestV150ProviderRetryExactDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		done := false
		go func() {
			value, err := RetryProviderRequest(context.Background(), func() (string, error) {
				attempts++
				if attempts == 1 {
					return "", &ProviderRequestError{Status: 429, Headers: http.Header{"Retry-After-Ms": []string{"1000"}}}
				}
				return "ok", nil
			}, ProviderRetryOptions{MaxRetries: 1})
			if err != nil || value != "ok" {
				t.Errorf("value=%q err=%v", value, err)
			}
			done = true
		}()
		synctest.Wait()
		if attempts != 1 || done {
			t.Fatalf("initial attempts=%d done=%v", attempts, done)
		}
		time.Sleep(999 * time.Millisecond)
		synctest.Wait()
		if attempts != 1 || done {
			t.Fatalf("early attempts=%d done=%v", attempts, done)
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		if attempts != 2 || !done {
			t.Fatalf("boundary attempts=%d done=%v", attempts, done)
		}
	})
}

// Go does not expose a timer count. Ending the bubble at the same virtual
// instant, with every worker joined, witnesses prompt worker cleanup;
// advancing past the former deadline must not issue another request.
func TestV150ProviderRetryAbortTimerLifecycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		attempts := 0
		done := false
		start := time.Now()
		go func() {
			_, err := RetryProviderRequest(ctx, func() (string, error) {
				attempts++
				return "", &ProviderRequestError{Status: 429, Headers: http.Header{"Retry-After": []string{"277403"}}}
			}, ProviderRetryOptions{MaxRetries: 2, DisableDelayCap: true})
			if err == nil || err.Error() != "request aborted" {
				t.Errorf("err=%v", err)
			}
			done = true
		}()
		synctest.Wait()
		if attempts != 1 || done {
			t.Fatalf("before abort attempts=%d done=%v", attempts, done)
		}
		cancel()
		synctest.Wait()
		if !done || attempts != 1 || !time.Now().Equal(start) {
			t.Fatalf("abort attempts=%d done=%v elapsed=%s", attempts, done, time.Since(start))
		}
		time.Sleep(277404 * time.Second)
		synctest.Wait()
		if attempts != 1 {
			t.Fatalf("late retry attempts=%d", attempts)
		}
	})
}

func TestV150ProviderRetryExplicitDelayCapDisabled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		done := false
		go func() {
			_, err := RetryProviderRequest(context.Background(), func() (string, error) {
				attempts++
				if attempts == 1 {
					return "", &ProviderRequestError{Status: 429, Headers: http.Header{"Retry-After": []string{"2"}}}
				}
				return "ok", nil
			}, ProviderRetryOptions{MaxRetries: 1, DisableDelayCap: true})
			if err != nil {
				t.Error(err)
			}
			done = true
		}()
		synctest.Wait()
		time.Sleep(1999 * time.Millisecond)
		synctest.Wait()
		if attempts != 1 || done {
			t.Fatal("retried before 2 seconds")
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		if attempts != 2 || !done {
			t.Fatalf("attempts=%d done=%v", attempts, done)
		}
	})
}

func TestV150ProviderRetryCanceledTimerIsStopped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		timer := time.NewTimer(time.Hour)
		done := false
		go func() {
			if err := waitProviderRetryTimer(ctx, timer); err == nil {
				t.Error("abort accepted")
			}
			done = true
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()
		if !done {
			t.Fatal("timer waiter not joined")
		}
		// Before the deadline, Stop is false only if the production waiter already
		// stopped its timer. This detects a missing Stop, unlike worker counts.
		if timer.Stop() {
			t.Fatal("production waiter retained an active timer after cancellation")
		}
	})
}
