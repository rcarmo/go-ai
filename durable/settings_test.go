package durable

import (
	"context"
	"testing"
)

func TestHarnessSettingsReferenceDefaultsPartialZeroFalseAndRuntimeGetter(t *testing.T) {
	defaults := (&Harness{}).resolvedSettings(RequestSettings{})
	if !defaults.Retry.Enabled || defaults.Retry.MaxRetries != 3 || defaults.Retry.BaseDelayMs != 2000 || defaults.Retry.MaxDelayMs != 60000 || !defaults.Compaction.Enabled || defaults.Compaction.ReserveTokens != 16384 || defaults.Compaction.KeepRecentTokens != 20000 || defaults.Compaction.BackgroundTokens != 32768 || defaults.ToolExecution != "parallel" || defaults.SteeringMode != "one-at-a-time" || defaults.FollowUpMode != "one-at-a-time" {
		t.Fatal(defaults)
	}
	backends(t, func(t *testing.T, b backend) {
		disabled, zero, delay := false, 0, int64(0)
		options := Options{Settings: &HarnessSettings{Retry: &RetrySettings{Enabled: &disabled, MaxRetries: &zero, BaseDelayMs: &delay}, Compaction: &CompactionSettings{BackgroundTokens: &zero}, SteeringMode: "all"}}
		var seen RequestSettings
		definition := taskDefinition(t, "task.settings", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			var err error
			seen, err = r.Settings(ctx)
			if err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarnessOptions(t, b.store, options, definition)
		disabled, zero, delay = true, 999, 999 // Host input must be detached on Open.
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		waitPublicTask(t, h, id)
		if seen.Retry.Enabled || seen.Retry.MaxRetries != 0 || seen.Retry.BaseDelayMs != 0 || seen.Retry.MaxDelayMs != 60000 || seen.Compaction.BackgroundTokens != 0 || !seen.Compaction.Enabled || seen.Compaction.KeepRecentTokens != 20000 || seen.SteeringMode != "all" {
			t.Fatal("partial/explicit zero settings", seen)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, doc := range state.Documents {
			if doc.Kind == "pi.agent" {
				var stored agentState
				if err := fromObject(doc.Value, &stored, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if stored.Settings.Retry != (RetryPolicy{}) {
					t.Fatal("host defaults persisted into agent", stored.Settings)
				}
			}
		}
	})
}
