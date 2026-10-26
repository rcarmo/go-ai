package durable

import "context"

// requeueAtClose runs after scheduler and invocation hosts have joined. No new
// Harness admission can race it. Preserve checkpoints, abort marks and restart
// counts; a clean shutdown is not another interrupted host execution. These
// writes bypass publication because all observers are closing. A failed prefix
// remains running for normal crash recovery on the next Open.
func (s *Session) requeueAtClose() error {
	ctx := context.Background()
	if s.failure() != nil {
		return nil
	}
	if err := s.enter(ctx); err != nil {
		return err
	}
	defer s.leave()
	if s.failure() != nil {
		return nil
	}
	state, err := s.taskSnapshot(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids(state.Tasks) {
		task := state.Tasks[id]
		if !taskManaged(task) || task.Status != "running" {
			continue
		}
		task, err = copyTask(task, s.limits)
		if err != nil {
			return err
		}
		task.Status = "pending"
		if task.Execution != nil && task.Execution.Native != nil {
			task.Execution.Native.State.Status = "pending"
		}
		// One record per atomic admission also fits hosts with tight persisted batch
		// budgets. Do not clear restarts or retry a rejected/uncertain write.
		_, err = s.core.apply(ctx, Batch{Writes: []Write{{Op: "put-task", Task: &task}}}, true)
		if err = s.backendResult(ctx, err, true); err != nil {
			return err
		}
	}
	return nil
}
