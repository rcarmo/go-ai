package durable

// Native RequestSettings keeps the model map beside the comparable concrete
// CompactionPolicy. Host configuration uses CompactionSettings.ModelOverrides.
func compactionPolicy(settings RequestSettings, model ModelRef, local ...*CompactionSettings) CompactionPolicy {
	key := string(model.Provider) + "/" + model.ID
	policy := settings.Compaction
	var conversation *CompactionSettings
	if len(local) > 0 {
		conversation = local[0]
	}
	policy = applyCompactionOverrides(policy, conversation)
	if override, ok := settings.CompactionModelOverrides[key]; ok {
		policy = applyCompactionOverrides(policy, &override)
	}
	if conversation != nil {
		if conversation.Enabled != nil && !*conversation.Enabled {
			policy.Enabled = false
		}
		if override, ok := conversation.ModelOverrides[key]; ok {
			policy = applyCompactionOverrides(policy, &override)
		}
	}
	return policy
}

func validateConversationCompaction(settings *CompactionSettings) error {
	if settings == nil {
		return nil
	}
	policy := applyCompactionOverrides(CompactionPolicy{}, settings)
	if policy.ReserveTokens < 0 || policy.KeepRecentTokens < 0 || policy.BackgroundTokens < 0 {
		return reject("invalid conversation compaction settings")
	}
	return validateCompactionModels(settings.ModelOverrides)
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
		if policy.ReserveTokens < 0 || policy.KeepRecentTokens < 0 || policy.BackgroundTokens < 0 {
			return reject("invalid compaction model override")
		}
	}
	return nil
}
