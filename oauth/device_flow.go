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
	switch oauthError {
	case "authorization_pending":
		return interval, true
	case "slow_down":
		return interval + 5*time.Second, true
	default:
		return interval, false
	}
}
