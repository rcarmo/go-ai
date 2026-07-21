package goai

import (
	"context"
	"time"
)

type AssistantRetryPolicy struct {
	Enabled     bool
	MaxRetries  int
	BaseDelayMs int
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
		delay := time.Duration(0)
		if policy != nil && policy.BaseDelayMs > 0 {
			delay = time.Duration(policy.BaseDelayMs) * time.Millisecond * time.Duration(1<<(attempt-1))
		}
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
				return &Message{Role: RoleAssistant, StopReason: StopReasonAborted}
			case <-timer.C:
			}
		} else if ctx.Err() != nil {
			if callbacks != nil && callbacks.OnRetryFinished != nil {
				callbacks.OnRetryFinished(false, attempt, lastError)
			}
			return &Message{Role: RoleAssistant, StopReason: StopReasonAborted}
		}
		if callbacks != nil && callbacks.OnRetryAttemptStart != nil {
			callbacks.OnRetryAttemptStart()
		}
	}
}
