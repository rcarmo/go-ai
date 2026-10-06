package durable

import "context"

// Agent resolves current committed configuration with one current registry
// snapshot. Each read is detached and does not enable the scheduler.
func (c *ConversationHandle) Agent(ctx context.Context) (TaskAgent, error) { return c.agent(ctx, nil) }
func (c *InvocationConversation) Agent(ctx context.Context) (TaskAgent, error) {
	return (&ConversationHandle{h: c.runtime.harness, id: c.ID()}).agent(ctx, c.runtime)
}

func (c *ConversationHandle) agent(ctx context.Context, binding *TaskRuntime) (TaskAgent, error) {
	if binding != nil {
		if err := binding.check(); err != nil {
			return TaskAgent{}, err
		}
	}
	if ctx == nil {
		return TaskAgent{}, reject("nil context")
	}
	var agent agentState
	var snapshot TaskRegistrySnapshot
	err := c.h.session.readTasks(ctx, func(state Snapshot) error {
		if binding != nil {
			if err := binding.check(); err != nil {
				return err
			}
		}
		if c.h.closing.Load() {
			return ErrClosed
		}
		if _, exists := state.Conversations[c.id]; !exists {
			return reject("unknown conversation")
		}
		if doc, ok := agentDocument(state, c.id); ok {
			if err := fromObject(doc.Value, &agent, c.h.session.limits); err != nil {
				return err
			}
		}
		var err error
		snapshot, err = c.h.options.Registry.taskSnapshot(c.h.session.limits)
		return err
	})
	if err != nil {
		return TaskAgent{}, err
	}
	value, _, err := c.h.resolveTaskAgent(agent, snapshot)
	if err != nil {
		return TaskAgent{}, err
	}
	if binding != nil {
		if err := binding.check(); err != nil {
			return TaskAgent{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return TaskAgent{}, err
	}
	if c.h.closing.Load() {
		return TaskAgent{}, ErrClosed
	}
	return copyTaskAgent(value, c.h.session.limits)
}
