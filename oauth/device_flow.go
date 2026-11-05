package oauth

import "time"

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
