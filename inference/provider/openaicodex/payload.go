package openaicodex

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
)

// Stream-owned state survives missing-continuation retries and SSE fallback.
// Per-attempt slot indices are rebuilt, but unresolved tool calls prevent completion.
type codexStreamOutput struct {
	partial    *goai.Message
	unfinished map[int]bool
}

func newCodexStreamOutput(model *goai.Model, opts *goai.StreamOptions) *codexStreamOutput {
	return &codexStreamOutput{
		partial: &goai.Message{
			Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, Usage: &goai.Usage{},
			StopReason: goai.StopReasonPending, ProviderThinkingLevel: codexConfigurationUpdateEffort(model, opts),
		},
		unfinished: map[int]bool{},
	}
}

type codexPreparedRequest struct {
	output    *codexStreamOutput
	bodyJSON  []byte
	requestID string
	envelope  map[string]interface{}
	tools     codexToolView
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
	if err := validateCodexRequestToolDeclarations(model, input); err != nil {
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
		//lint:ignore ST1005 Provider name is a proper noun; preserve the public diagnostic.
		return nil, &codexPayloadError{fmt.Errorf("Codex request payload must be an object")}
	}
	requestID := codexCacheSessionID(opts)
	if requestID == "" {
		requestID = goai.UUIDv7()
	}
	return &codexPreparedRequest{bodyJSON: encoded, envelope: envelope, tools: tools, requestID: requestID}, nil
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
