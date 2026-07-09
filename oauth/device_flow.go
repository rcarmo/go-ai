package oauth

import "time"

const minDeviceCodePollInterval = 5 * time.Second

func normalizeDeviceCodePollInterval(intervalSecs int) time.Duration {
	interval := time.Duration(intervalSecs) * time.Second
	if interval < minDeviceCodePollInterval {
		return minDeviceCodePollInterval
	}
	return interval
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
