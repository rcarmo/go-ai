package durable

import "context"

// CreateOwnedConversation creates an invocation-owned conversation with inherited
// agent configuration. A nonempty key deduplicates creation across host replay.
// The key lives in an invocation memo, adopted with the conversation itself.
func (r *TaskRuntime) CreateOwnedConversation(ctx context.Context, key string, change *AgentChange) (*InvocationConversation, error) {
	var id ID
	err := r.Commit(ctx, func(tx *Tx, current TaskRecord) (*TaskState, error) {
		memoKey := "conversation:" + key
		if key != "" {
			if value, ok := current.Memos[memoKey]; ok {
				number, valid := exactNumber(value.Value)
				if !valid {
					return nil, reject("owned conversation memo")
				}
				id = ID(number.Num().Uint64())
				if conversation, exists := tx.state.Conversations[id]; !exists || conversation.Owner != r.TaskID() {
					return nil, reject("owned conversation identity")
				}
				return nil, nil
			}
		}
		parent, ok := agentDocument(tx.state, r.ConversationID())
		if !ok {
			return nil, reject("owner agent unavailable")
		}
		var state agentState
		if err := fromObject(parent.Value, &state, tx.limits); err != nil {
			return nil, err
		}
		if change != nil {
			next, err := r.harness.agent(*change)
			if err != nil {
				return nil, err
			}
			state = next
		}
		var err error
		id, err = tx.MintID()
		if err != nil {
			return nil, err
		}
		if err = tx.CreateConversation(Conversation{ID: id, Owner: r.TaskID()}); err != nil {
			return nil, err
		}
		docID, err := tx.MintID()
		if err != nil {
			return nil, err
		}
		value, err := dtoObject(state, tx.limits)
		if err != nil {
			return nil, err
		}
		if _, err = tx.CreateDocument(Document{ID: docID, Scope: "conversation", Owner: id, Kind: "pi.agent", Version: 1, History: "rewindable", Fork: "asOf", Value: value}); err != nil {
			return nil, err
		}
		if err = initializeBuiltins(tx, id); err != nil {
			return nil, err
		}
		if key != "" {
			raw, err := copyTask(tx.state.Tasks[r.TaskID()], tx.limits)
			if err != nil {
				return nil, err
			}
			if raw.Execution == nil {
				if raw.Kind != "pi.tool" && raw.Kind != "pi.generation" {
					return nil, reject("owner execution unavailable")
				}
				raw.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{AbortRequested: taskAborted(raw)}}
			}
			memo := &TaskValue{Present: true, Value: id}
			if raw.Execution.Native != nil {
				if raw.Execution.Native.Memos == nil {
					raw.Execution.Native.Memos = map[string]*TaskValue{}
				}
				raw.Execution.Native.Memos[memoKey] = memo
			} else {
				if raw.Execution.Builtin.Memos == nil {
					raw.Execution.Builtin.Memos = map[string]*TaskValue{}
				}
				raw.Execution.Builtin.Memos[memoKey] = memo
			}
			if err = tx.stage(Write{Op: "put-task", Task: &raw}); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	return r.Conversation(ctx, id)
}
func (api *ToolAPI) CreateOwnedConversation(ctx context.Context, key string, change *AgentChange) (*InvocationConversation, error) {
	return api.runtime.CreateOwnedConversation(ctx, key, change)
}
