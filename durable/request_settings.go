package durable

import goai "github.com/rcarmo/go-ai"

// Apply a detached, committed behaviour snapshot. Authentication and observation
// hooks are separately rehydrated from RequestOptions at dispatch/recovery.
func applyRequestSettings(options *goai.StreamOptions, settings RequestSettings, limits Limits) error {
	value, err := cloneSettings(settings, limits)
	if err != nil {
		return err
	}
	options.Temperature, options.MaxTokens, options.Deferred = value.Temperature, value.MaxTokens, value.Deferred
	options.Transport, options.TimeoutMs = value.Transport, value.TimeoutMs
	options.MaxRetries, options.MaxRetryDelayMs = value.MaxRetries, value.MaxRetryDelayMs
	options.Metadata, options.CacheRetention = value.Metadata, value.CacheRetention
	if value.Headers != nil {
		headers := make(map[string]string, len(options.Headers)+len(value.Headers))
		for key, value := range value.Headers {
			headers[key] = value
		}
		// Fresh local authentication remains authoritative; never checkpoint it.
		for key, value := range options.Headers {
			headers[key] = value
		}
		options.Headers = headers
	}
	return nil
}
