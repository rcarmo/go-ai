package openaicodex

import goai "github.com/rcarmo/go-ai"

// Transport ownership and counters use the raw session key. Only wire cache
// keys/identity headers are clamped. Retention none has no transport session.
func codexTransportSessionID(opts *goai.StreamOptions) string {
	if opts == nil || opts.CacheRetention == goai.CacheRetentionNone {
		return ""
	}
	return opts.SessionID
}

func codexCacheSessionID(opts *goai.StreamOptions) string {
	return goai.ClampOpenAIPromptCacheKey(codexTransportSessionID(opts))
}
