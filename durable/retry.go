package durable

import (
	"math"
)

// RetryPolicy is persisted request behaviour. Defaults disable durable retries;
// transport-level retries remain independently controlled by the provider.
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
	base := policy.BaseDelayMs
	if base == 0 {
		base = 1000
	}
	cap := policy.MaxDelayMs
	if cap == 0 {
		cap = 60000
	}
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
