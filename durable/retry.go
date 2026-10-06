package durable

import (
	"math"
)

// RetryPolicy is a durable conversation override. Host defaults enable three
// retries with a 2-second base; transport retries are provider-controlled.
type RetryPolicy struct {
	Enabled     bool  `json:"enabled,omitempty"`
	MaxRetries  int   `json:"maxRetries,omitempty"`
	BaseDelayMs int64 `json:"baseDelayMs,omitempty"`
	MaxDelayMs  int64 `json:"maxDelayMs,omitempty"`
}

func validateRetryPolicy(policy RetryPolicy) error {
	if policy.MaxRetries < 0 || policy.MaxRetries > 100 || policy.BaseDelayMs < 0 || policy.MaxDelayMs < 0 || uint64(policy.BaseDelayMs) > MaxID || uint64(policy.MaxDelayMs) > MaxID {
		return reject("invalid durable retry policy")
	}
	return nil
}
func retryDelay(policy RetryPolicy, attempt int) int64 {
	// Callers supply resolved settings; explicit zero means no delay.
	base, cap := policy.BaseDelayMs, policy.MaxDelayMs
	for i := 1; i < attempt && base < cap; i++ {
		if base > cap/2 {
			base = cap
		} else {
			base *= 2
		}
	}
	if base > cap {
		return cap
	}
	return base
}
func retryDeadline(now, delay int64) int64 {
	if now > math.MaxInt64-delay {
		return math.MaxInt64
	}
	return now + delay
}
func retryableReceipt(message MessageReceipt) bool {
	return message.Retryable
}
