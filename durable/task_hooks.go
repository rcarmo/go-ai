package durable

import (
	"context"
	"errors"
	"sync/atomic"
)

type taskHookFailure struct{ err error }

func taskHookCause(err, target error) (same bool) {
	defer func() {
		if recover() != nil {
			same = false
		}
	}()
	return errors.Is(err, target)
}

// TaskHook is a process-local custom handler. The caller decides its argument
// protocol; JSON arguments/results cross the same detached strict value boundary.
type TaskHook func(context.Context, any) (any, error)
type selectedTaskHook struct {
	TaskHookRegistration
	extension string
}

type TaskHookRegistration struct {
	Task     string
	Handlers map[string]TaskHook
}

// EachHook visits selected handlers in extension/registration order. Both
// invoke and each handler are invocation-fenced. A callback error/panic is
// reported and later handlers continue unless invocation cancellation occurred.
func (r *TaskRuntime) EachHook(ctx context.Context, name string, invoke func(TaskHook) error) error {
	if err := r.check(); err != nil {
		return err
	}
	if ctx == nil || invoke == nil || !validKind(name) {
		return reject("invalid task hook invocation")
	}
	resolution, err := r.phaseAgent(ctx)
	if err != nil {
		return err
	}
	for _, registration := range resolution.hooks {
		if registration.Task != r.kind {
			continue
		}
		handler := registration.Handlers[name]
		if handler == nil {
			continue
		}
		if err := r.check(); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.context.Err(); err != nil {
			return err
		}
		var failure atomic.Pointer[taskHookFailure]
		gated := func(callContext context.Context, input any) (any, error) {
			if err := r.check(); err != nil {
				return nil, err
			}
			if callContext == nil {
				return nil, reject("nil context")
			}
			if err := r.context.Err(); err != nil {
				return nil, err
			}
			if err := callContext.Err(); err != nil {
				return nil, err
			}
			owned, err := ownJSONValue(input, r.harness.session.limits)
			if err != nil {
				return nil, err
			}
			value, err := callTaskHookHandler(handler, callContext, owned)
			if err != nil {
				failure.Store(&taskHookFailure{err: err})
				return nil, err
			}
			if err := r.check(); err != nil {
				return nil, err
			}
			if err := callContext.Err(); err != nil {
				return nil, err
			}
			if err := r.context.Err(); err != nil {
				return nil, err
			}
			ownedResult, err := ownJSONValue(value, r.harness.session.limits)
			if err != nil {
				failure.Store(&taskHookFailure{err: err})
			}
			return ownedResult, err
		}
		if err := callTaskHookInvoke(invoke, gated); err != nil {
			if err := r.context.Err(); err != nil {
				return err
			}
			if handlerFailure := failure.Load(); handlerFailure != nil && taskHookCause(err, handlerFailure.err) {
				r.reportExtensionError(err, registration.extension)
			} else {
				r.reportError(err)
			}
		}
	}
	if err := r.check(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.context.Err()
}
func callTaskHookHandler(handler TaskHook, ctx context.Context, input any) (value any, err error) {
	defer func() {
		if recover() != nil {
			value = nil
			err = reject("task hook handler panic")
		}
	}()
	return handler(ctx, input)
}

func callTaskHookInvoke(invoke func(TaskHook) error, handler TaskHook) (err error) {
	defer func() {
		if recover() != nil {
			err = reject("task hook callback panic")
		}
	}()
	return invoke(handler)
}
