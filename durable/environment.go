package durable

import "context"

// ExecutionEnvironment is the native local/remote environment identity. Coding
// tool packages may require richer capabilities on the returned implementation.
type ExecutionEnvironment interface{ Cwd() string }
type EnvTarget struct {
	Conversation ID
	Cwd          string
	Read         *HookAPI
}

func (a *ToolAPI) Environment(ctx context.Context) (ExecutionEnvironment, error) {
	if err := a.runtime.check(); err != nil {
		return nil, err
	}
	var cwd string
	err := a.h.session.readTasks(context.Background(), func(state Snapshot) error {
		if err := a.runtime.check(); err != nil {
			return err
		}
		if a.h.closing.Load() {
			return ErrClosed
		}
		if taskAborted(state.Tasks[a.runtime.TaskID()]) {
			return ErrSealed
		}
		doc, ok := agentDocument(state, a.task.Conversation)
		if !ok {
			return reject("agent unavailable")
		}
		var agent agentState
		if err := fromObject(doc.Value, &agent, a.h.session.limits); err != nil {
			return err
		}
		cwd = agent.Cwd
		return nil
	})
	if err != nil {
		return nil, err
	}
	if a.h.options.Env == nil {
		return nil, nil
	}
	target := EnvTarget{Conversation: a.task.Conversation, Cwd: cwd, Read: &HookAPI{a.runtime}}
	env, err := callEnvironment(ctx, a.h.options.Env, target)
	if err != nil {
		return nil, err
	}
	if err := a.runtime.check(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.h.closing.Load() {
		return nil, ErrClosed
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
