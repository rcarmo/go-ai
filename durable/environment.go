package durable

import (
	"context"
	"fmt"
)

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
// Agent returns a detached invocation-local selection for tool capabilities.
func (a *ToolAPI) Agent(ctx context.Context) (TaskAgent, error) {
	a.mu.Lock()
	active := a.active
	a.mu.Unlock()
	if !active {
		return TaskAgent{}, ErrSealed
	}
	return a.runtime.Agent(ctx)
}
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
	return resolveEnvironment(ctx, runtime.context, h, runtime, conversation, rejectAborted)
}

// Cleanup uses the Harness lifetime in place of the cancelled run lifetime.
func resolveEnvironment(ctx, invocation context.Context, h *Harness, runtime *TaskRuntime, conversation ID, rejectAborted bool) (ExecutionEnvironment, error) {
	if ctx == nil {
		return nil, reject("nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := invocation.Err(); err != nil {
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
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Preparation reports its failure once and renders without env. Tool
		// execution instead turns this same error into a tool_error result.
		return nil, err
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
	if err := invocation.Err(); err != nil {
		return nil, err
	}
	return env, nil
}
func callEnvironment(ctx context.Context, factory func(context.Context, EnvTarget) (ExecutionEnvironment, error), target EnvTarget) (env ExecutionEnvironment, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			env = nil
			if cause, ok := failure.(error); ok {
				err = cause
			} else {
				err = fmt.Errorf("%v", failure)
			}
		}
	}()
	return factory(ctx, target)
}
