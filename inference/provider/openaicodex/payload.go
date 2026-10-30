package openaicodex

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
)

type codexPreparedRequest struct {
	bodyJSON []byte
	envelope map[string]interface{}
	tools    codexToolView
}
type codexPayloadError struct{ cause error }

func (e *codexPayloadError) Error() string { return e.cause.Error() }
func (e *codexPayloadError) Unwrap() error { return e.cause }

func prepareCodexRequest(ctx context.Context, model *goai.Model, input *goai.Context, opts *goai.StreamOptions) (*codexPreparedRequest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tools, err := newCodexToolView(model, input)
	if err != nil {
		return nil, &codexPayloadError{err}
	}
	body := buildCodexRequest(model, input, opts, tools)
	payload, err := goai.InvokeOnPayload(opts, body, model)
	if err != nil {
		return nil, &codexPayloadError{err}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, &codexPayloadError{err}
	}
	var envelope map[string]interface{}
	if err = json.Unmarshal(encoded, &envelope); err != nil {
		return nil, &codexPayloadError{err}
	}
	if envelope == nil {
		return nil, &codexPayloadError{fmt.Errorf("Codex request payload must be an object")}
	}
	return &codexPreparedRequest{bodyJSON: encoded, envelope: envelope, tools: tools}, nil
}
func codexRequestForTransport(ctx context.Context, model *goai.Model, input *goai.Context, opts *goai.StreamOptions, prepared []*codexPreparedRequest) (*codexPreparedRequest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(prepared) > 0 && prepared[0] != nil {
		return prepared[0], nil
	}
	return prepareCodexRequest(ctx, model, input, opts)
}
