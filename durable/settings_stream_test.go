package durable

import "testing"

func TestHostStreamRuntimePoliciesModesPreservedAndTopLevelWins(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		disabled := false
		all := "all"
		host := HarnessSettings{Stream: RequestSettings{Retry: RetryPolicy{Enabled: true, MaxRetries: 2, BaseDelayMs: 7, MaxDelayMs: 20}, Compaction: CompactionPolicy{Enabled: true, ReserveTokens: 200, KeepRecentTokens: 50, BackgroundTokens: 10}, ToolExecution: "sequential", SteeringMode: all, FollowUpMode: all}}
		h := taskTestHarnessOptions(t, b.store, Options{Settings: &host})
		_, err := h.Root(bg, AgentChange{})
		if err != nil {
			t.Fatal(err)
		}
		agent := h.resolvedSettings(RequestSettings{})
		if agent.Retry.MaxRetries != 2 || agent.Retry.BaseDelayMs != 7 || agent.Compaction.ReserveTokens != 200 || agent.ToolExecution != "sequential" || agent.SteeringMode != all || agent.FollowUpMode != all {
			t.Fatal("stream runtime fields lost", agent)
		}
		host.Retry = &RetrySettings{Enabled: &disabled}
		host.Compaction = &CompactionSettings{Enabled: &disabled}
		host.ToolExecution = "parallel"
		host.SteeringMode = "one-at-a-time"
		host.FollowUpMode = "one-at-a-time"
		if err := h.SetSettings(bg, host); err != nil {
			t.Fatal(err)
		}
		agent = h.resolvedSettings(RequestSettings{})
		if agent.Retry.Enabled || agent.Retry.BaseDelayMs != 7 || agent.Compaction.Enabled || agent.Compaction.ReserveTokens != 200 || agent.ToolExecution != "parallel" || agent.SteeringMode != "one-at-a-time" || agent.FollowUpMode != "one-at-a-time" {
			t.Fatal("top level precedence lost", agent)
		}
	})
}
