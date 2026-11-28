package oauth

import (
	"context"
	"fmt"
	"time"
)

const minDeviceCodePollInterval = time.Second
const defaultDeviceCodePollInterval = 5 * time.Second

// Legacy provider DTOs use zero for an omitted interval. Explicit positive
// intervals follow v1.5.0: minimum one second; omission defaults to five.
func normalizeDeviceCodePollInterval(intervalSecs int) time.Duration {
	if intervalSecs == 0 {
		return defaultDeviceCodePollInterval
	}
	return explicitDeviceCodePollInterval(intervalSecs)
}

func explicitDeviceCodePollInterval(intervalSecs int) time.Duration {
	if intervalSecs < 1 {
		return minDeviceCodePollInterval
	}
	const maxDuration = time.Duration(1<<63 - 1)
	if int64(intervalSecs) > int64(maxDuration/time.Second) {
		return maxDuration
	}
	return time.Duration(intervalSecs) * time.Second
}

func nextDeviceCodePollInterval(interval time.Duration, oauthError string) (time.Duration, bool) {
	return nextDeviceCodePollIntervalWithServerInterval(interval, oauthError, 0)
}

func nextDeviceCodePollIntervalWithServerInterval(interval time.Duration, oauthError string, serverIntervalSecs int) (time.Duration, bool) {
	switch oauthError {
	case "authorization_pending":
		return interval, true
	case "slow_down":
		if serverIntervalSecs > 0 {
			return normalizeDeviceCodePollInterval(serverIntervalSecs), true
		}
		return interval + 5*time.Second, true
	default:
		return interval, false
	}
}

func deviceCodeServerInterval(raw map[string]interface{}) int {
	if raw == nil {
		return 0
	}
	switch v := raw["interval"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

// OAuthDeviceCodePollResult is the result of one device authorization poll.
type OAuthDeviceCodePollResult[T any] struct {
	Status          string
	Value           T
	Message         string
	IntervalSeconds int
}

// OAuthDeviceCodePollOptions controls the public RFC 8628 polling helper.
type OAuthDeviceCodePollOptions[T any] struct {
	IntervalSeconds     *int
	ExpiresInSeconds    *int
	WaitBeforeFirstPoll bool
	Poll                func(context.Context) (OAuthDeviceCodePollResult[T], error)
	Signal              context.Context
}

func PollOAuthDeviceCodeFlow[T any](opts OAuthDeviceCodePollOptions[T]) (T, error) {
	var zero T
	ctx := opts.Signal
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Poll == nil {
		return zero, fmt.Errorf("OAuth device code poll function is missing")
	}
	deadline := time.Time{}
	if opts.ExpiresInSeconds != nil {
		deadline = time.Now().Add(time.Duration(*opts.ExpiresInSeconds) * time.Second)
	}
	interval := defaultDeviceCodePollInterval
	if opts.IntervalSeconds != nil {
		interval = explicitDeviceCodePollInterval(*opts.IntervalSeconds)
	}
	if opts.WaitBeforeFirstPoll {
		if !deadline.IsZero() {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return zero, oauthFlowError("Device flow timed out")
			}
			if interval > remaining {
				interval = remaining
			}
		}
		if err := waitOAuthDeviceCode(ctx, interval); err != nil {
			return zero, err
		}
	}
	slowDown := false
	for deadline.IsZero() || time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return zero, oauthFlowError("Login cancelled")
		}
		result, err := opts.Poll(ctx)
		if err != nil {
			return zero, err
		}
		switch result.Status {
		case "complete":
			return result.Value, nil
		case "failed":
			return zero, fmt.Errorf("%s", result.Message)
		case "slow_down":
			slowDown = true
			if result.IntervalSeconds > 0 {
				interval = explicitDeviceCodePollInterval(result.IntervalSeconds)
			} else {
				interval += 5 * time.Second
			}
		case "pending":
		default:
			return zero, fmt.Errorf("invalid OAuth device code poll status %q", result.Status)
		}
		wait := interval
		if !deadline.IsZero() {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				break
			}
			if wait > remaining {
				wait = remaining
			}
		}
		if err := waitOAuthDeviceCode(ctx, wait); err != nil {
			return zero, err
		}
	}
	if slowDown {
		return zero, oauthFlowError("Device flow timed out after one or more slow_down responses. This is often caused by clock drift in WSL or VM environments. Please sync or restart the VM clock and try again")
	}
	return zero, oauthFlowError("Device flow timed out")
}

func oauthFlowError(message string) error { return fmt.Errorf("%s", message) }

func waitOAuthDeviceCode(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		if err := ctx.Err(); err != nil {
			return oauthFlowError("Login cancelled")
		}
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return oauthFlowError("Login cancelled")
	case <-timer.C:
		return nil
	}
}
