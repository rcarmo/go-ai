package goai

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const defaultProviderMaxRetryDelay = 60 * time.Second

type ProviderRequestError struct {
	Status  int
	Headers http.Header
	Message string
}

func (e *ProviderRequestError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("provider error: %d", e.Status)
}

type ProviderRetryOptions struct {
	MaxRetries      int
	MaxRetryDelay   time.Duration
	DisableDelayCap bool
}

func RetryProviderRequest[T any](ctx context.Context, request func() (T, error), options ProviderRetryOptions) (T, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	remaining := options.MaxRetries
	maxRetries := options.MaxRetries
	for {
		value, err := request()
		if err == nil {
			return value, nil
		}
		if ctx.Err() != nil {
			var zero T
			return zero, providerAbortError()
		}
		perr, ok := err.(*ProviderRequestError)
		if remaining <= 0 || !ok || !isRetryableProviderRequestError(perr) {
			return value, err
		}
		retryIndex := maxRetries - remaining
		remaining--
		delay, err := providerRetryDelay(perr, retryIndex, options)
		if err != nil {
			var zero T
			return zero, err
		}
		if err := sleepProviderRetry(ctx, delay); err != nil {
			var zero T
			return zero, err
		}
	}
}

func isRetryableProviderRequestError(err *ProviderRequestError) bool {
	if err.Headers != nil {
		switch err.Headers.Get("x-should-retry") {
		case "true":
			return true
		case "false":
			return false
		}
	}
	if err.Status == 0 {
		return true
	}
	return err.Status == 408 || err.Status == 409 || err.Status == 429 || err.Status >= 500
}

func providerRetryDelay(err *ProviderRequestError, retryIndex int, options ProviderRetryOptions) (time.Duration, error) {
	if err.Headers != nil {
		if v := err.Headers.Get("retry-after-ms"); v != "" {
			ms, parseErr := strconv.ParseFloat(v, 64)
			if parseErr == nil {
				return validateProviderRetryDelay(time.Duration(ms*float64(time.Millisecond)), options, err.Error())
			}
		}
		if v := err.Headers.Get("retry-after"); v != "" {
			seconds, parseErr := strconv.ParseFloat(v, 64)
			if parseErr == nil {
				return validateProviderRetryDelay(time.Duration(seconds*float64(time.Second)), options, err.Error())
			}
			if when, dateErr := http.ParseTime(v); dateErr == nil {
				return validateProviderRetryDelay(time.Until(when), options, err.Error())
			}
		}
	}
	multiplier := 1
	for i := 0; i < retryIndex; i++ {
		multiplier *= 2
	}
	delay := time.Duration(float64(time.Second) * 0.5 * float64(multiplier))
	if delay > 8*time.Second {
		delay = 8 * time.Second
	}
	return delay, nil
}

func validateProviderRetryDelay(delay time.Duration, options ProviderRetryOptions, message string) (time.Duration, error) {
	if options.DisableDelayCap {
		return delay, nil
	}
	maxDelay := options.MaxRetryDelay
	if maxDelay == 0 {
		maxDelay = defaultProviderMaxRetryDelay
	}
	if maxDelay > 0 && delay > maxDelay {
		return 0, fmt.Errorf("server requested %ds retry delay (max: %ds). %s", int((delay+time.Second-1)/time.Second), int((maxDelay+time.Second-1)/time.Second), message)
	}
	return delay, nil
}

func sleepProviderRetry(ctx context.Context, delay time.Duration) error {
	if ctx.Err() != nil {
		return providerAbortError()
	}
	timer := time.NewTimer(max(delay, 0))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return providerAbortError()
	case <-timer.C:
		return nil
	}
}

func providerAbortError() error {
	err := fmt.Errorf("request aborted")
	return err
}
