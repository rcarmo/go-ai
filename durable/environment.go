package durable

import "context"

// ExecutionEnvironment is the native local/remote environment identity.
// Read/write/edit tools require FileSystem; the local Bash adapter additionally
// requires its process capability. Factories and resources are not persisted.
type ExecutionEnvironment interface{ Cwd() string }
type EnvTarget struct {
	Conversation ID
	Cwd          string
	Read         *InvocationReader
}

// Environment returns the capability constructed once before tool execution.
func (a *ToolAPI) Environment(ctx context.Context) (ExecutionEnvironment, error) {
	if ctx == nil {
		return nil, reject("nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := a.runtime.check(); err != nil {
		return nil, err
	}
	if a.environmentResolved {
		return a.environment, a.environmentErr
	}
	return resolveInvocationEnvironment(ctx, a.h, a.runtime, a.task.Conversation, true)
}

// Environment resolves capabilities at every use with the current committed cwd,
// independently of the phase-local Agent snapshot. No factory runs on the line.
func (r *TaskRuntime) Environment(ctx context.Context) (ExecutionEnvironment, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	return resolveInvocationEnvironment(ctx, r.harness, r, r.conversation, false)
}

func resolveInvocationEnvironment(ctx context.Context, h *Harness, runtime *TaskRuntime, conversation ID, rejectAborted bool) (ExecutionEnvironment, error) {
	if ctx == nil {
		return nil, reject("nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := runtime.context.Err(); err != nil {
		return nil, err
	}
	var cwd string
	err := h.session.readTasks(ctx, func(state Snapshot) error {
		if err := runtime.check(); err != nil {
			return err
		}
		if h.closing.Load() {
			return ErrClosed
		}
		if rejectAborted && taskAborted(state.Tasks[runtime.TaskID()]) {
			return ErrSealed
		}
		if _, exists := state.Conversations[conversation]; !exists {
			return reject("unknown conversation")
		}
		var agent agentState
		if doc, ok := agentDocument(state, conversation); ok {
			if err := fromObject(doc.Value, &agent, h.session.limits); err != nil {
				return err
			}
		}
		cwd = agent.Cwd
		return nil
	})
	if err != nil {
		return nil, err
	}
	if h.options.Env == nil {
		return nil, runtime.check()
	}
	target := EnvTarget{Conversation: conversation, Cwd: cwd, Read: &InvocationReader{runtime}}
	env, err := callEnvironment(ctx, h.options.Env, target)
	if err != nil {
		// This is a host callback, not a tool's own error. Report locally but
		// never publish credentials/private payloads through durable outcomes.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		reportTaskError(h.options.OnReport, err)
		return nil, reject("environment callback failed")
	}
	if err := runtime.check(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if h.closing.Load() {
		return nil, ErrClosed
	}
	if err := runtime.context.Err(); err != nil {
		return nil, err
	}
	return env, nil
}
func callEnvironment(ctx context.Context, factory func(context.Context, EnvTarget) (ExecutionEnvironment, error), target EnvTarget) (env ExecutionEnvironment, err error) {
	defer func() {
		if recover() != nil {
			env = nil
			err = reject("environment callback panic")
		}
	}()
	return factory(ctx, target)
}
