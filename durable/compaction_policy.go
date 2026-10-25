package durable

// Native RequestSettings keeps the model map beside the comparable concrete
// CompactionPolicy. Host configuration uses CompactionSettings.ModelOverrides.
func compactionPolicy(settings RequestSettings, model ModelRef) CompactionPolicy {
	policy := settings.Compaction
	if override, ok := settings.CompactionModelOverrides[string(model.Provider)+"/"+model.ID]; ok {
		policy = applyCompactionOverrides(policy, &override)
	}
	return policy
}
func mergeCompactionModels(base, overrides map[string]CompactionSettings) map[string]CompactionSettings {
	if len(overrides) == 0 {
		return base
	}
	out := make(map[string]CompactionSettings, len(base)+len(overrides))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range overrides {
		out[key] = value
	}
	return out
}
func validateCompactionModels(models map[string]CompactionSettings) error {
	for _, override := range models {
		if override.ModelOverrides != nil {
			return reject("nested compaction model overrides")
		}
		policy := applyCompactionOverrides(CompactionPolicy{}, &override)
		if policy.ReserveTokens < 0 || policy.KeepRecentTokens < 0 || policy.BackgroundTokens < 0 || policy.MaxTokens < 0 {
			return reject("invalid compaction model override")
		}
	}
	return nil
}
