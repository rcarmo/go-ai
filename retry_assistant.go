package goai

import "regexp"

var nonRetryableProviderLimitErrorPattern = regexp.MustCompile(`(?i)GoUsageLimitError|FreeUsageLimitError|Monthly usage limit reached|available balance|insufficient_quota|out of budget|quota exceeded|billing`)

var retryableProviderErrorPattern = regexp.MustCompile(`(?i)overloaded|rate.?limit|too many requests|429|500|502|503|504|524|service.?unavailable|server.?error|internal.?error|provider.?returned.?error|network.?error|connection.?error|connection.?refused|connection.?lost|other side closed|socket connection was closed|fetch failed|upstream.?connect|reset before headers|socket hang up|timed? out|timeout|terminated|websocket.?closed|websocket.?error|ended without|stream ended before_message_stop|stream ended before message_stop|http2 request did not get a response|ResourceExhausted|retry delay|you can retry your request|try your request again|please retry your request`)

func IsRetryableAssistantError(message *Message) bool {
	if message == nil || message.StopReason != StopReasonError || message.ErrorMessage == "" {
		return false
	}
	if nonRetryableProviderLimitErrorPattern.MatchString(message.ErrorMessage) {
		return false
	}
	return retryableProviderErrorPattern.MatchString(message.ErrorMessage)
}
