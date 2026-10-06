package durable

import "context"

func backgroundAnchorDefinition() (*TaskDefinition, error) {
	return DefineTask(TaskDefinitionOptions{Kind: "task.pi.subagent-anchor", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "done"}, nil }, Phases: map[string]TaskPhase{"done": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: nil}}}, nil
		})
	}}, Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
		})
	}})
}

// CreateBackgroundConversation creates a persistent subagent conversation below
// a background anchor. Ordinary parent abort/idle waits stop at that anchor;
// explicit Background=true abort crosses it. A key deduplicates creation.
func (c *ConversationHandle) CreateBackgroundConversation(ctx context.Context, key string, change *AgentChange) (*ConversationHandle, error) {
	h := c.h
	if key == "" {
		return nil, reject("background conversation requires a key")
	}
	var child ID
	_, err := h.CommitTasks(ctx, c.id, func(tx *Tx) error {
		address := DocumentAddress{Scope: "conversation", Owner: c.id, Kind: "app.subagents", Family: true, Key: key}
		for _, doc := range tx.state.Documents {
			if !doc.Retired && documentAddress(doc) == address {
				number, ok := exactNumber(doc.Value["conversation"])
				if !ok {
					return reject("subagent address")
				}
				child = ID(number.Num().Uint64())
				return nil
			}
		}
		snapshot, err := h.options.Registry.taskSnapshot(tx.limits)
		if err != nil {
			return err
		}
		anchor := snapshot.Task("task.pi.subagent-anchor")
		if anchor == nil {
			return reject("background anchor unavailable")
		}
		owner, err := tx.CreateTask(anchor, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
		if err != nil {
			return err
		}
		var agent agentState
		if stateDoc, ok := agentDocument(tx.state, c.id); ok {
			if err := fromObject(stateDoc.Value, &agent, tx.limits); err != nil {
				return err
			}
		}
		if change != nil {
			agent, err = h.agent(*change)
			if err != nil {
				return err
			}
		}
		child, err = tx.MintID()
		if err != nil {
			return err
		}
		if err = tx.CreateConversation(Conversation{ID: child, Owner: owner}); err != nil {
			return err
		}
		value, err := dtoObject(agent, tx.limits)
		if err != nil {
			return err
		}
		{
			candidate, err := tx.current()
			if err != nil {
				return err
			}
			existing, ok := agentDocument(candidate, child)
			if !ok {
				return reject("created agent unavailable")
			}
			handle, err := tx.Document(existing.ID)
			if err != nil {
				return err
			}
			if change != nil {
				if err := handle.Set(value); err != nil {
					return err
				}
			}
		}
		if err = initializeBuiltins(tx, child); err != nil {
			return err
		}
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		_, err = tx.CreateDocument(Document{ID: id, Scope: "conversation", Owner: c.id, Kind: "app.subagents", Version: 1, Family: true, Key: key, History: "latest", Fork: "initial", Value: JSON{"conversation": child, "anchor": owner}})
		return err
	})
	if err != nil {
		return nil, err
	}
	h.scheduler.enable()
	return h.Conversation(ctx, child)
}

// BackgroundAgent is detached host status. Reading status never resumes work.
type BackgroundAgent struct {
	Name         string
	Conversation ID
	Anchor       ID
	Busy         bool
}

func (c *ConversationHandle) BackgroundAgents(ctx context.Context) ([]BackgroundAgent, error) {
	agents := []BackgroundAgent{}
	err := c.h.session.readTasks(ctx, func(state Snapshot) error {
		if c.h.closing.Load() {
			return ErrClosed
		}
		for _, id := range ids(state.Documents) {
			doc := state.Documents[id]
			if doc.Scope != "conversation" || doc.Owner != c.id || doc.Kind != "app.subagents" || !doc.Family || doc.Retired {
				continue
			}
			child, ok := exactNumber(doc.Value["conversation"])
			if !ok {
				return reject("subagent identity")
			}
			anchor, ok := exactNumber(doc.Value["anchor"])
			if !ok {
				return reject("subagent anchor")
			}
			agent := BackgroundAgent{Name: doc.Key, Conversation: ID(child.Num().Uint64()), Anchor: ID(anchor.Num().Uint64())}
			for _, task := range state.Tasks {
				if task.Conversation == agent.Conversation && task.Kind == "pi.generation" && !terminalStatus(task.Status) {
					agent.Busy = true
					break
				}
			}
			agents = append(agents, agent)
		}
		return nil
	})
	return agents, err
}

// StopBackground aborts current child work, preserving its conversation and
// history for later SendBackground calls. Reporters suppress aborted answers.
func (c *ConversationHandle) StopBackground(ctx context.Context, key string) error {
	agents, err := c.BackgroundAgents(ctx)
	if err != nil {
		return err
	}
	for _, agent := range agents {
		if agent.Name == key {
			child, err := c.h.Conversation(ctx, agent.Conversation)
			if err != nil {
				return err
			}
			return child.Abort(ctx)
		}
	}
	return reject("unknown background subagent")
}
