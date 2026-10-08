package durable

func copyTaskTime(value *int64) *int64 {
	if value == nil {
		return nil
	}
	owned := *value
	return &owned
}

// Preserve first committed/staged timestamps across retry/recovery and held
// outcomes. Native done/failed/aborted records are the terminal-state variants.
// Completing keeps owned descendants live and does not stamp completion yet.
func stampTaskTimes(value, prior Task, now func() int64) Task {
	if prior.StartedAt != nil {
		value.StartedAt = prior.StartedAt
	}
	if prior.EndedAt != nil {
		value.EndedAt = prior.EndedAt
	}
	if value.Status == "running" && value.StartedAt == nil {
		stamp := now()
		value.StartedAt = &stamp
	}
	if terminalStatus(value.Status) && value.EndedAt == nil {
		stamp := now()
		value.EndedAt = &stamp
	}
	return value
}
