package quickjs

import (
	"context"
	"encoding/json"
	"errors"
)

func boundedQuickJSCallback(ctx context.Context, exec func(context.Context, json.RawMessage) (json.RawMessage, error), args json.RawMessage) (out json.RawMessage, err error) {
	defer func() {
		if recover() != nil {
			out = nil
			err = errors.New("QuickJS host callback panic")
		}
	}()
	out, err = exec(ctx, args)
	if err != nil {
		if len(err.Error()) > int(maxBridgeStringBytes) {
			return nil, errors.New("QuickJS host error exceeds byte limit")
		}
		return nil, err
	}
	if len(out) > int(maxBridgeStringBytes) {
		return nil, errors.New("QuickJS host result exceeds byte limit")
	}
	if out != nil && !json.Valid(out) {
		return nil, errors.New("invalid QuickJS host result JSON")
	}
	return out, nil
}
