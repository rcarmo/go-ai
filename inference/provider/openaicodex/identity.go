package openaicodex

import goai "github.com/rcarmo/go-ai"

func codexCacheSessionID(opts *goai.StreamOptions) string {
	if opts == nil || opts.CacheRetention == goai.CacheRetentionNone {
		return ""
	}
	return goai.ClampOpenAIPromptCacheKey(opts.SessionID)
}
