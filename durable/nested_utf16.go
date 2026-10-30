package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
)

// toolArgumentState is a schema-owned final argument snapshot. Its carrier
// metadata follows the same codec and budgets as the main tool checkpoint.
type toolArgumentState struct {
	StreamingArguments bool     `json:"streamingArguments,omitempty"`
	UTF16Arguments     []string `json:"utf16Arguments,omitempty"`
	Arguments          JSON     `json:"arguments"`
}

// StartToolWithArguments admits a keyed nested call using the SDK's final,
// UTF-16-aware argument domain. StartTool retains ordinary strict JSON rules.
func (r *TaskRuntime) StartToolWithArguments(ctx context.Context, key, name string, args goai.StreamingArgumentsJSON) (*NestedToolHandle, error) {
	return r.startToolScoped(ctx, key, name, JSON(args), false, true)
}

// CallToolWithArguments waits for a typed nested call under its durable key.
func (r *TaskRuntime) CallToolWithArguments(ctx context.Context, key, name string, args goai.StreamingArgumentsJSON) (ToolResult, error) {
	handle, err := r.StartToolWithArguments(ctx, key, name, args)
	if err != nil {
		return ToolResult{}, err
	}
	return handle.Wait(ctx)
}

func nestedArgumentCheckpoint(args JSON, native bool, l Limits) (JSON, error) {
	cp := JSON{"phase": "execute", "started": true}
	if !native {
		cp["arguments"] = args
		return cp, nil
	}
	object, err := dtoObject(toolArgumentState{StreamingArguments: true, Arguments: args}, l)
	if err != nil {
		return nil, err
	}
	cp["argumentState"] = object
	return cp, nil
}
func nestedCheckpointArguments(cp JSON, input nestedToolInput, l Limits) (JSON, error) {
	if raw, present := cp["argumentState"]; present {
		if _, duplicate := cp["arguments"]; duplicate || !input.StreamingArguments {
			return nil, reject("inconsistent nested argument ownership")
		}
		object, ok := raw.(map[string]any)
		if named, yes := raw.(JSON); yes {
			object = map[string]any(named)
			ok = true
		}
		if !ok {
			return nil, reject("invalid nested argument state")
		}
		var state toolArgumentState
		if err := fromObject(JSON(object), &state, l); err != nil {
			return nil, err
		}
		if !state.StreamingArguments || state.Arguments == nil {
			return nil, reject("nested argument ownership lost")
		}
		return state.Arguments, nil
	}
	if raw, present := cp["arguments"]; present {
		if input.StreamingArguments {
			return nil, reject("missing typed nested argument state")
		}
		object, ok := raw.(map[string]any)
		if !ok {
			return nil, reject("invalid nested arguments")
		}
		return JSON(object), nil
	}
	started, _ := cp["started"].(bool)
	if started && input.StreamingArguments {
		return nil, reject("missing nested argument intent")
	}
	return input.Arguments, nil
}
func (input nestedToolInput) argumentCall(args JSON) goai.ToolCall {
	call := goai.ToolCall{ID: input.Key, Name: input.Name, Arguments: map[string]any(args)}
	if input.StreamingArguments {
		call.SetStreamingArguments(map[string]any(args), "")
	}
	return call
}

// StartToolWithArguments admits a typed nested call while this API is active.
func (a *ToolAPI) StartToolWithArguments(ctx context.Context, key, name string, args goai.StreamingArgumentsJSON) (*NestedToolHandle, error) {
	a.mu.Lock()
	active := a.active
	a.mu.Unlock()
	if !active {
		return nil, ErrSealed
	}
	return a.runtime.StartToolWithArguments(ctx, key, name, args)
}

// CallToolWithArguments waits for a typed nested call under its durable key.
func (a *ToolAPI) CallToolWithArguments(ctx context.Context, key, name string, args goai.StreamingArgumentsJSON) (ToolResult, error) {
	handle, err := a.StartToolWithArguments(ctx, key, name, args)
	if err != nil {
		return ToolResult{}, err
	}
	return handle.Wait(ctx)
}
