package durable

import "context"

// HarnessSettings supplies process-local settings over the pinned runtime
// defaults. Pointer-valued policy fields preserve explicit zero/false overrides.
// Stream behaviour remains independent of credentials.
type RetrySettings struct {
	Enabled     *bool  `json:"enabled,omitempty"`
	MaxRetries  *int   `json:"maxRetries,omitempty"`
	BaseDelayMs *int64 `json:"baseDelayMs,omitempty"`
	MaxDelayMs  *int64 `json:"maxDelayMs,omitempty"`
}
type CompactionSettings struct {
	Enabled          *bool `json:"enabled,omitempty"`
	ReserveTokens    *int  `json:"reserveTokens,omitempty"`
	KeepRecentTokens *int  `json:"keepRecentTokens,omitempty"`
	BackgroundTokens *int  `json:"backgroundTokens,omitempty"`
	MaxTokens        *int  `json:"maxTokens,omitempty"`
}

// ProgressSettings preserves explicit zero intervals. Omitted fields inherit
// the reference 100ms defaults, independently for partials and tool output.
type ProgressSettings struct {
	PartialIntervalMs *int64 `json:"partialIntervalMs,omitempty"`
	OutputIntervalMs  *int64 `json:"outputIntervalMs,omitempty"`
}

type HarnessSettings struct {
	ContextRetentionMs *int64
	Extensions         *[]string
	Stream             RequestSettings
	Retry              *RetrySettings
	Compaction         *CompactionSettings
	ToolExecution      string
	SteeringMode       string
	FollowUpMode       string
	Progress           *ProgressSettings
}

func defaultHarnessSettings() RequestSettings {
	retention := int64(600000)
	return RequestSettings{ContextRetentionMs: &retention, Retry: RetryPolicy{Enabled: true, MaxRetries: 3, BaseDelayMs: 2000, MaxDelayMs: 60000}, Compaction: CompactionPolicy{Enabled: true, ReserveTokens: 16384, KeepRecentTokens: 20000, BackgroundTokens: 32768}, ToolExecution: "parallel", SteeringMode: "one-at-a-time", FollowUpMode: "one-at-a-time"}
}

func (h *Harness) resolvedSettings(stored RequestSettings) RequestSettings {
	settings := defaultHarnessSettings()
	host := h.hostSettings.Load()
	if host == nil {
		host = h.options.Settings
	}
	if host != nil {
		settings = host.Stream
		if host.ContextRetentionMs != nil {
			settings.ContextRetentionMs = copyTaskTime(host.ContextRetentionMs)
		}
		if settings.ContextRetentionMs == nil {
			retention := int64(600000)
			settings.ContextRetentionMs = &retention
		}
		defaults := defaultHarnessSettings()
		if settings.Retry == (RetryPolicy{}) {
			settings.Retry = defaults.Retry
		}
		if settings.Compaction == (CompactionPolicy{}) {
			settings.Compaction = defaults.Compaction
		}
		if settings.ToolExecution == "" {
			settings.ToolExecution = defaults.ToolExecution
		}
		if settings.SteeringMode == "" {
			settings.SteeringMode = defaults.SteeringMode
		}
		if settings.FollowUpMode == "" {
			settings.FollowUpMode = defaults.FollowUpMode
		}
		settings.Retry = applyRetryOverrides(settings.Retry, host.Stream.RetryOverrides)
		settings.Compaction = applyCompactionOverrides(settings.Compaction, host.Stream.CompactionOverrides)
		settings.Retry = applyRetryOverrides(settings.Retry, host.Retry)
		settings.Compaction = applyCompactionOverrides(settings.Compaction, host.Compaction)
		settings.Progress = mergeProgress(settings.Progress, host.Progress)
		if host.ToolExecution != "" {
			settings.ToolExecution = host.ToolExecution
		}
		if host.SteeringMode != "" {
			settings.SteeringMode = host.SteeringMode
		}
		if host.FollowUpMode != "" {
			settings.FollowUpMode = host.FollowUpMode
		}
	}
	// Retain the public per-conversation override surface; empty configuration
	// follows host settings instead of silently disabling runtime defaults.
	if stored.ContextRetentionMs != nil {
		settings.ContextRetentionMs = copyTaskTime(stored.ContextRetentionMs)
	}
	if stored.Temperature != nil {
		settings.Temperature = stored.Temperature
	}
	if stored.MaxTokens != nil {
		settings.MaxTokens = stored.MaxTokens
	}
	if stored.Deferred != nil {
		settings.Deferred = stored.Deferred
	}
	if stored.Transport != "" {
		settings.Transport = stored.Transport
	}
	if stored.TimeoutMs != nil {
		settings.TimeoutMs = stored.TimeoutMs
	}
	if stored.MaxRetries != nil {
		settings.MaxRetries = stored.MaxRetries
	}
	if stored.MaxRetryDelayMs != nil {
		settings.MaxRetryDelayMs = stored.MaxRetryDelayMs
	}
	if stored.Headers != nil {
		settings.Headers = stored.Headers
	}
	if stored.Metadata != nil {
		settings.Metadata = stored.Metadata
	}
	if stored.CacheRetention != "" {
		settings.CacheRetention = stored.CacheRetention
	}
	if stored.Retry != (RetryPolicy{}) {
		settings.Retry = stored.Retry
	}
	if stored.Compaction != (CompactionPolicy{}) {
		settings.Compaction = stored.Compaction
	}
	settings.Retry = applyRetryOverrides(settings.Retry, stored.RetryOverrides)
	settings.Compaction = applyCompactionOverrides(settings.Compaction, stored.CompactionOverrides)
	// Resolved intents contain concrete policies, not input presence metadata.
	settings.RetryOverrides, settings.CompactionOverrides = nil, nil
	settings.Progress = mergeProgress(settings.Progress, stored.Progress)
	partial, output := int64(100), int64(100)
	settings.Progress = mergeProgress(&ProgressSettings{PartialIntervalMs: &partial, OutputIntervalMs: &output}, settings.Progress)
	if stored.ToolExecution != "" {
		settings.ToolExecution = stored.ToolExecution
	}
	if stored.SteeringMode != "" {
		settings.SteeringMode = stored.SteeringMode
	}
	if stored.FollowUpMode != "" {
		settings.FollowUpMode = stored.FollowUpMode
	}
	return settings
}

func applyRetryOverrides(policy RetryPolicy, overrides *RetrySettings) RetryPolicy {
	if overrides == nil {
		return policy
	}
	if overrides.Enabled != nil {
		policy.Enabled = *overrides.Enabled
	}
	if overrides.MaxRetries != nil {
		policy.MaxRetries = *overrides.MaxRetries
	}
	if overrides.BaseDelayMs != nil {
		policy.BaseDelayMs = *overrides.BaseDelayMs
	}
	if overrides.MaxDelayMs != nil {
		policy.MaxDelayMs = *overrides.MaxDelayMs
	}
	return policy
}

func applyCompactionOverrides(policy CompactionPolicy, overrides *CompactionSettings) CompactionPolicy {
	if overrides == nil {
		return policy
	}
	if overrides.Enabled != nil {
		policy.Enabled = *overrides.Enabled
	}
	if overrides.ReserveTokens != nil {
		policy.ReserveTokens = *overrides.ReserveTokens
	}
	if overrides.KeepRecentTokens != nil {
		policy.KeepRecentTokens = *overrides.KeepRecentTokens
	}
	if overrides.BackgroundTokens != nil {
		policy.BackgroundTokens = *overrides.BackgroundTokens
	}
	if overrides.MaxTokens != nil {
		policy.MaxTokens = *overrides.MaxTokens
	}
	return policy
}

// SetSettings atomically replaces host settings for subsequent resolutions.
// Existing request intents retain committed behaviour. Inputs are detached.
func (h *Harness) SetSettings(ctx context.Context, settings HarnessSettings) error {
	if ctx == nil {
		return reject("nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if h.closing.Load() {
		return ErrClosed
	}
	resolved := (&Harness{options: Options{Settings: &settings}}).resolvedSettings(RequestSettings{})
	value, err := cloneSettings(resolved, h.session.limits)
	if err != nil {
		return err
	}
	retryEnabled, compactionEnabled := value.Retry.Enabled, value.Compaction.Enabled
	copy := &HarnessSettings{ContextRetentionMs: copyTaskTime(value.ContextRetentionMs), Stream: value, Retry: &RetrySettings{Enabled: &retryEnabled, MaxRetries: &value.Retry.MaxRetries, BaseDelayMs: &value.Retry.BaseDelayMs, MaxDelayMs: &value.Retry.MaxDelayMs}, Compaction: &CompactionSettings{Enabled: &compactionEnabled, ReserveTokens: &value.Compaction.ReserveTokens, KeepRecentTokens: &value.Compaction.KeepRecentTokens, BackgroundTokens: &value.Compaction.BackgroundTokens, MaxTokens: &value.Compaction.MaxTokens}, ToolExecution: value.ToolExecution, SteeringMode: value.SteeringMode, FollowUpMode: value.FollowUpMode, Progress: mergeProgress(nil, value.Progress)}
	if settings.Extensions != nil {
		names, err := agentSelectionNames(*settings.Extensions)
		if err != nil {
			return err
		}
		copy.Extensions = &names
	}
	if h.closing.Load() {
		return ErrClosed
	}
	h.hostSettings.Store(copy)
	h.session.taskBookkeeping(func() { h.session.expireContextRanges() })
	return nil
}

// Settings reads resolved host defaults, like the reference runtime getter.
func (r *TaskRuntime) Settings(ctx context.Context) (RequestSettings, error) {
	if err := r.check(); err != nil {
		return RequestSettings{}, err
	}
	if ctx == nil {
		return RequestSettings{}, reject("nil context")
	}
	if err := ctx.Err(); err != nil {
		return RequestSettings{}, err
	}
	return cloneSettings(r.harness.resolvedSettings(RequestSettings{}), r.harness.session.limits)
}

func mergeProgress(base, override *ProgressSettings) *ProgressSettings {
	result := &ProgressSettings{}
	for _, value := range []*ProgressSettings{base, override} {
		if value == nil {
			continue
		}
		if value.PartialIntervalMs != nil {
			n := *value.PartialIntervalMs
			result.PartialIntervalMs = &n
		}
		if value.OutputIntervalMs != nil {
			n := *value.OutputIntervalMs
			result.OutputIntervalMs = &n
		}
	}
	return result
}
