package goai

import "regexp"

var nonRetryableProviderLimitErrorPattern = regexp.MustCompile(`(?i)GoUsageLimitError|FreeUsageLimitError|Monthly usage limit reached|available balance|insufficient_quota|out of budget|quota exceeded|billing|subscription_sharing_usage_limit_exceeded|subscription_sharing_workplace_individual_usage_limit_exceeded`)

// Match upstream's ASCII alphanumeric boundaries (#11057). RE2 has no
// lookaround, so consume the delimiters; only existence of a match is needed.
var retryableStatusPattern = regexp.MustCompile(`(?i)(^|[^a-z0-9])(429|500|502|503|504|520|524)($|[^a-z0-9])`)

var retryableProviderErrorPattern = regexp.MustCompile(`(?i)server_busy|servers are currently busy|pending stream has been canceled|overloaded|currently experiencing high demand|model is at capacity|rate.?limit|too many requests|service.?(?:is.?)?(?:temporarily.?)?unavailable|server.?error|internal.?error|provider.?returned.?(?:an.?)?error|exceeded request buffer limit while retrying upstream|upstream.?request.?failed|upstream_stream_read_error|stream was interrupted|http/?2 stream failed|network.?error|connection.?error|connection.?refused|connection.?lost|other side closed|socket connection was closed|fetch failed|request to \S+ failed, reason:|getaddrinfo|ENOTFOUND|EAI_AGAIN|upstream.?connect|reset before headers|socket hang up|timed? out|timeout|terminated|unable to forward upstream|stream disconnected before completion|websocket.?closed|websocket.?error|ended without|stream ended before_message_stop|stream ended before message_stop|stream ended before a terminal response event|http2 request did not get a response|ResourceExhausted|subscription_sharing_usage_unavailable|subscription_sharing_user_unavailable|retry delay|you can retry your request|try your request again|please retry your request`)

func IsRetryableAssistantError(message *Message) bool {
	if message == nil || message.StopReason != StopReasonError || message.ErrorMessage == "" {
		return false
	}
	if nonRetryableProviderLimitErrorPattern.MatchString(message.ErrorMessage) {
		return false
	}
	if retryableProviderErrorPattern.MatchString(message.ErrorMessage) {
		return true
	}
	return retryableStatusPattern.MatchString(message.ErrorMessage)
}
