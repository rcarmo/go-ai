package durable

import goai "github.com/rcarmo/go-ai"

// startExplicitContinue implements the entryless, idle-only admission. Selected
// queued users precede the continue in live.run.inputs and share its generation.
// The public submission type is the persisted discriminator: no new checkpoint
// field or fabricated empty user message is needed.
func (h *Harness) startExplicitContinue(tx *Tx, conversation, submission ID, requestID string, carrier *goai.TelemetryCarrier) error {
	live, err := builtin(tx, conversation, "pi.live")
	if err != nil {
		return err
	}
	previous, err := live.Get()
	if err != nil {
		return err
	}
	first, err := h.placeFinalBoundaryWrites(tx, conversation)
	if err != nil {
		return err
	}
	if err := tx.PutSubmission(Submission{ID: submission, Conversation: conversation, RequestID: requestID, Type: "continue", Status: "pending", Value: JSON{}, Trace: carrier}); err != nil {
		return err
	}
	taskID, err := tx.MintID()
	if err != nil {
		return err
	}
	cp := generationCheckpoint{Phase: "queued", Submission: submission}
	task := Task{ID: taskID, Conversation: conversation, Kind: "pi.generation", Status: "pending"}
	if err := h.applyQueuedInputs(tx, task, &cp, true); err != nil {
		return err
	}
	state, err := tx.current()
	if err != nil {
		return err
	}
	inputs := make([]any, 0, len(cp.Steered)+1)
	for _, id := range cp.Steered {
		inputs = append(inputs, id)
		if first == 0 {
			first = submissionEntryID(state.Submissions[id].Value["placedEntry"])
		}
	}
	inputs = append(inputs, submission)
	if inherited := submissionEntryID(generationObject(previous["runFailure"])["first"]); inherited != 0 {
		first = inherited
	}
	task.Checkpoint, err = dtoObject(cp, tx.limits)
	if err != nil {
		return err
	}
	if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
		return err
	}
	return live.Update(func(value JSON) error {
		if len(generationObject(value["run"])) != 0 {
			return reject("generation run ownership changed")
		}
		run := JSON{"task": taskID, "inputs": inputs}
		if first != 0 {
			run["first"] = first
		}
		value["run"] = run
		delete(value, "runFailure")
		delete(value, "failedRun")
		return nil
	})
}
