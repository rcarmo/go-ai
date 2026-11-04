package goai

import (
	"context"
	"time"
)

type AssistantRetryPolicy struct {
	Enabled     bool
	MaxRetries  int
	BaseDelayMs int
	// MaxAgentDelayMs caps each assistant retry. Nil uses 60,000 ms; zero
	// explicitly disables backoff. Negative values are treated as zero.
	MaxAgentDelayMs *int
}

const DefaultMaxAgentRetryDelayMs = 60_000

// assistantRetryDelay keeps exponential arithmetic within both the source's
// exact-integer range and Go's time.Duration representation.
func assistantRetryDelay(policy *AssistantRetryPolicy, attempt int) time.Duration {
	if policy == nil || policy.BaseDelayMs <= 0 {
		return 0
	}
	const maxSafe = int64(1<<53 - 1)
	const maxDurationMs = int64((1<<63 - 1) / int64(time.Millisecond))
	capMs := int64(DefaultMaxAgentRetryDelayMs)
	if policy.MaxAgentDelayMs != nil {
		capMs = int64(*policy.MaxAgentDelayMs)
	}
	if capMs <= 0 {
		return 0
	}
	if capMs > maxSafe {
		capMs = maxSafe
	}
	if capMs > maxDurationMs {
		capMs = maxDurationMs
	}
	delay := int64(policy.BaseDelayMs)
	if delay >= capMs {
		return time.Duration(capMs) * time.Millisecond
	}
	for n := 1; n < attempt; n++ {
		if delay > capMs/2 {
			return time.Duration(capMs) * time.Millisecond
		}
		delay *= 2
	}
	return time.Duration(delay) * time.Millisecond
}

type AssistantRetryCallbacks struct {
	OnRetryScheduled    func(attempt, maxAttempts int, delay time.Duration, errorMessage string)
	OnRetryAttemptStart func()
	OnRetryFinished     func(success bool, attempt int, finalError string)
}

// RetryAssistantCall runs produce with bounded retries on transient assistant errors.
// Aborted messages are terminal and reported as unsuccessful if a retry had been scheduled.
func RetryAssistantCall(ctx context.Context, produce func() *Message, policy *AssistantRetryPolicy, callbacks *AssistantRetryCallbacks) *Message {
	if ctx == nil {
		ctx = context.Background()
	}
	maxAttempts := 0
	if policy != nil && policy.Enabled {
		maxAttempts = policy.MaxRetries
	}
	attempt := 0
	lastAttempt := 0
	lastError := ""
	for {
		response := produce()
		if response == nil {
			response = &Message{Role: RoleAssistant, StopReason: StopReasonError, ErrorMessage: "nil assistant response"}
		}
		if response.StopReason == StopReasonAborted {
			if lastAttempt > 0 && callbacks != nil && callbacks.OnRetryFinished != nil {
				callbacks.OnRetryFinished(false, lastAttempt, "")
			}
			return response
		}
		if response.StopReason != StopReasonError {
			if lastAttempt > 0 && callbacks != nil && callbacks.OnRetryFinished != nil {
				callbacks.OnRetryFinished(true, lastAttempt, "")
			}
			return response
		}
		if attempt >= maxAttempts || !IsRetryableAssistantError(response) {
			if lastAttempt > 0 && callbacks != nil && callbacks.OnRetryFinished != nil {
				callbacks.OnRetryFinished(false, lastAttempt, response.ErrorMessage)
			}
			return response
		}
		attempt++
		lastAttempt = attempt
		lastError = response.ErrorMessage
		delay := assistantRetryDelay(policy, attempt)
		if callbacks != nil && callbacks.OnRetryScheduled != nil {
			callbacks.OnRetryScheduled(attempt, maxAttempts, delay, lastError)
		}
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				if callbacks != nil && callbacks.OnRetryFinished != nil {
					callbacks.OnRetryFinished(false, attempt, lastError)
				}
				aborted := *response
				aborted.StopReason, aborted.ErrorMessage = StopReasonAborted, ""
				return &aborted
			case <-timer.C:
			}
		} else if ctx.Err() != nil {
			if callbacks != nil && callbacks.OnRetryFinished != nil {
				callbacks.OnRetryFinished(false, attempt, lastError)
			}
			aborted := *response
			aborted.StopReason, aborted.ErrorMessage = StopReasonAborted, ""
			return &aborted
		}
		if callbacks != nil && callbacks.OnRetryAttemptStart != nil {
			callbacks.OnRetryAttemptStart()
		}
	}
}
