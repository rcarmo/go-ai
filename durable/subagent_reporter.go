package durable

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// SendBackground starts a durable background reporter for a named child. Its
// input request ID deduplicates reporter admission; child delivery and parent
// report have stable IDs of their own. An empty Type steers a busy child.
func (c *ConversationHandle) SendBackground(ctx context.Context, key string, input Input) (ID, error) {
	if input.Entry != nil || len(input.Blocks) > 0 || input.Type != "" && input.Type != "steer" && input.Type != "follow-up" {
		return 0, reject("invalid background input")
	}
	if len(input.RequestID) > c.h.session.limits.MaxRequestIDBytes {
		return 0, reject("request ID limit")
	}
	snapshot, err := c.h.options.Registry.taskSnapshot(c.h.session.limits)
	if err != nil {
		return 0, err
	}
	definition := snapshot.Task("task.pi.subagent-reporter")
	if definition == nil {
		return 0, reject("background reporter unavailable")
	}
	var reporter ID
	_, err = c.h.CommitTasks(ctx, c.id, func(tx *Tx) error {
		doc, ok := backgroundAgentDocument(tx.state, c.id, key)
		if !ok {
			return reject("unknown background subagent")
		}
		if input.RequestID != "" {
			if reporters, ok := doc.Value["reporters"].(map[string]any); ok {
				if value, exists := reporters[input.RequestID]; exists {
					number, valid := exactNumber(value)
					if !valid {
						return reject("reporter identity")
					}
					reporter = ID(number.Num().Uint64())
					return nil
				}
			}
		}
		number, ok := exactNumber(doc.Value["conversation"])
		if !ok {
			return reject("subagent identity")
		}
		kind := input.Type
		if kind == "" {
			kind = "steer"
		}
		var err error
		reporter, err = tx.CreateTask(definition, JSON{"name": key, "conversation": ID(number.Num().Uint64()), "message": input.Content, "type": kind}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
		if err != nil {
			return err
		}
		if input.RequestID != "" {
			handle, err := tx.Document(doc.ID)
			if err != nil {
				return err
			}
			return handle.Update(func(value JSON) error {
				reporters, ok := value["reporters"].(map[string]any)
				if !ok {
					reporters = map[string]any{}
					value["reporters"] = reporters
				}
				reporters[input.RequestID] = reporter
				return nil
			})
		}
		return nil
	})
	if err == nil {
		c.h.scheduler.enable()
	}
	return reporter, err
}
func backgroundAgentDocument(state Snapshot, conversation ID, key string) (Document, bool) {
	for _, doc := range state.Documents {
		if doc.Scope == "conversation" && doc.Owner == conversation && doc.Kind == "app.subagents" && doc.Family && doc.Key == key && !doc.Retired {
			return doc, true
		}
	}
	return Document{}, false
}
func subagentReporterDefinition() (*TaskDefinition, error) {
	return DefineTask(TaskDefinitionOptions{Kind: "task.pi.subagent-reporter", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "deliver"}, nil }, Phases: map[string]TaskPhase{
		"deliver": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			input := task.Input.Value.(map[string]any)
			number, ok := exactNumber(input["conversation"])
			if !ok {
				return reject("reporter child identity")
			}
			name, _ := input["name"].(string)
			message, _ := input["message"].(string)
			kind, _ := input["type"].(string)
			child, err := r.Conversation(ctx, ID(number.Num().Uint64()))
			if err != nil {
				return err
			}
			sub, err := child.Submit(ctx, Input{Content: message, Type: kind, RequestID: fmt.Sprintf("subagent:%d", r.TaskID())})
			if err != nil {
				return err
			}
			settled, err := sub.Wait(ctx)
			if err != nil {
				var yield *InvocationWaitRequiresYield
				if !errors.As(err, &yield) {
					return err
				}
				// Capacity/dependency rejection asks the native task to yield,
				// not fail. Persist the dependency and release its executor permit.
				return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					if terminalStatus(tx.state.Submissions[sub.ID()].Status) {
						return &TaskState{Status: "running", Checkpoint: JSON{"phase": "deliver", "settled": true}}, nil
					}
					for _, id := range ids(tx.state.Tasks) {
						raw := tx.state.Tasks[id]
						if raw.Kind != "pi.generation" || terminalStatus(raw.Status) {
							continue
						}
						var cp generationCheckpoint
						if err := fromObject(raw.Checkpoint, &cp, tx.limits); err != nil {
							return nil, err
						}
						if generationIncludesSubmission(cp, sub.ID()) {
							return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "deliver"}, On: []ID{id}, Policy: "allSettled"}, nil
						}
					}
					return nil, reject("reporter submission task unavailable")
				})
			}
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				checkpoint := JSON{"phase": "report"}
				if settled.Submission.Status == "done" && settled.Message != nil {
					doc, ok := backgroundAgentDocument(tx.state, r.ConversationID(), name)
					if !ok {
						return nil, reject("reporter registry missing")
					}
					handle, err := tx.Document(doc.ID)
					if err != nil {
						return nil, err
					}
					err = handle.Update(func(value JSON) error {
						reported, _ := value["reported"].([]any)
						for _, id := range reported {
							number, ok := exactNumber(id)
							if ok && ID(number.Num().Uint64()) == settled.Task.ID {
								return nil
							}
						}
						value["reported"] = append(reported, settled.Task.ID)
						var text strings.Builder
						for _, block := range settled.Message.Content {
							if block.Type == "text" {
								text.WriteString(block.Text)
							}
						}
						checkpoint["report"] = fmt.Sprintf("[subagent %s answered, no reply needed] %s", name, text.String())
						return nil
					})
					if err != nil {
						return nil, err
					}
				} else if settled.Submission.Status != "aborted" {
					checkpoint["report"] = fmt.Sprintf("[subagent %s failed: %s]", name, settled.Submission.Status)
				}
				return &TaskState{Status: "running", Checkpoint: checkpoint}, nil
			})
		},
		"report": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			if text, ok := task.State.Checkpoint["report"].(string); ok {
				parent, err := r.Conversation(ctx, r.ConversationID())
				if err != nil {
					return err
				}
				if _, err = parent.Submit(ctx, Input{Content: text, Type: "follow-up", RequestID: fmt.Sprintf("subagent-report:%d", r.TaskID())}); err != nil {
					return err
				}
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: nil}}}, nil
			})
		},
	}, Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
		})
	}})
}
