package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
)

// ModelRef selects a process-local provider. Intent also persists a sanitized
// model behavior DTO; endpoints, headers, credentials and executable hooks stay
// process-local and are never included in the journal.
type ModelRef struct {
	Provider goai.Provider `json:"provider"`
	ID       string        `json:"id"`
}
type RequestSettings struct {
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"maxTokens,omitempty"`
}
type AgentChange struct {
	Name         string
	Model        ModelRef
	SystemPrompt string
	Settings     RequestSettings
}
type Options struct {
	Models func(goai.Provider, string) *goai.Model
	// RequestOptions resolves process-local credentials and hooks once per
	// dispatched attempt. M1b behavior options are limited to persisted Settings;
	// other behavior fields reject rather than silently changing after recovery.
	// Payload rewrite hooks reject; read-only hooks are process-local HOST code
	// and have no immutable/pure-function guarantee.
	RequestOptions func(context.Context, ModelRef) (*goai.StreamOptions, error)
}

func cloneModel(m *goai.Model, l Limits) (*goai.Model, error) {
	if m == nil {
		return nil, reject("model unavailable")
	}
	// Strip process-local transport/authentication before bounded validation.
	// Large private headers cannot consume the persisted model DTO's budget.
	// The dispatch path rehydrates endpoint/headers/APIKey separately.
	behavior := *m
	behavior.BaseURL = ""
	behavior.Headers = nil
	behavior.APIKey = ""
	p, e := encodeBounded(behavior, l, l.MaxRecordBytes)
	if e != nil {
		return nil, e
	}
	var copy goai.Model
	if e = decodeStrict(p, l, l.MaxRecordBytes, &copy); e != nil {
		return nil, e
	}
	// Caller must own the source snapshot during copying; registry values may
	// only be mutated after this method returns. This copy has no transport/auth.
	return &copy, nil
}
func cloneOptions(o *goai.StreamOptions, l Limits) (*goai.StreamOptions, error) {
	if o == nil {
		return &goai.StreamOptions{}, nil
	}
	// Only process-local authentication/observational hooks are accepted here.
	// Request behavior comes from the committed RequestSettings, not a fresh
	// recovery callback. M1c may extend this curated DTO under its own scope.
	behavior := *o
	behavior.APIKey = ""
	behavior.BearerToken = ""
	behavior.Headers = nil
	behavior.Env = nil
	behavior.TelemetryContext = nil
	behavior.OnPayload = nil
	behavior.OnPayloadWithTelemetry = nil
	behavior.OnResponse = nil
	behavior.OnResponseWithTelemetry = nil
	behavior.OnProviderStreamEvent = nil
	encoded, e := encodeBounded(behavior, l, l.MaxRecordBytes)
	if e != nil {
		return nil, e
	}
	var fields JSON
	if e = decodeStrict(encoded, l, l.MaxRecordBytes, &fields); e != nil {
		return nil, e
	}
	if len(fields) != 0 {
		return nil, reject("unsupported unpinned request behavior")
	}
	n := *o
	// Remove executable/credential fields only from the temporary copy used to
	// detach DTO fields. They are restored exclusively to process-local memory.
	data := *o
	data.APIKey = ""
	data.Headers = nil
	data.Env = nil
	data.BearerToken = ""
	data.TelemetryContext = nil
	data.OnPayload = nil
	data.OnPayloadWithTelemetry = nil
	data.OnResponse = nil
	data.OnResponseWithTelemetry = nil
	data.OnProviderStreamEvent = nil
	p, e := encodeBounded(data, l, l.MaxRecordBytes)
	if e != nil {
		return nil, e
	}
	var detached goai.StreamOptions
	if e = decodeStrict(p, l, l.MaxRecordBytes, &detached); e != nil {
		return nil, e
	}
	n = detached
	n.APIKey = o.APIKey
	n.BearerToken = o.BearerToken
	n.Headers = make(map[string]string, len(o.Headers))
	for k, v := range o.Headers {
		n.Headers[k] = v
	}
	n.Env = make(goai.ProviderEnv, len(o.Env))
	for k, v := range o.Env {
		n.Env[k] = v
	}
	n.RetryConfig = o.RetryConfig
	n.TelemetryContext = o.TelemetryContext
	// Payload-changing hooks are intentionally unsupported in M1b because they
	// can override the committed model/context/settings after intent. Read-only
	// response/raw-event observers remain explicit process-local host authority.
	if o.OnPayload != nil || o.OnPayloadWithTelemetry != nil {
		return nil, reject("payload rewrite hooks unsupported")
	}
	n.OnPayload = nil
	n.OnPayloadWithTelemetry = nil
	n.OnResponse = o.OnResponse
	n.OnResponseWithTelemetry = o.OnResponseWithTelemetry
	n.OnProviderStreamEvent = o.OnProviderStreamEvent
	return &n, nil
}
func dtoObject(v any, l Limits) (JSON, error) {
	p, e := encodeBounded(v, l, l.MaxDocumentBytes)
	if e != nil {
		return nil, e
	}
	var obj JSON
	if e = decodeStrict(p, l, l.MaxDocumentBytes, &obj); e != nil {
		return nil, e
	}
	return obj, nil
}
func fromObject(v JSON, target any, l Limits) error {
	p, e := encodeBounded(v, l, l.MaxDocumentBytes)
	if e != nil {
		return e
	}
	return decodeStrict(p, l, l.MaxDocumentBytes, target)
}
func cloneSettings(s RequestSettings, l Limits) (RequestSettings, error) {
	p, e := encodeBounded(s, l, l.MaxRecordBytes)
	if e != nil {
		return RequestSettings{}, e
	}
	var n RequestSettings
	e = decodeStrict(p, l, l.MaxRecordBytes, &n)
	if e == nil && ((n.MaxTokens != nil && *n.MaxTokens < 1) || (n.Temperature != nil && *n.Temperature < 0)) {
		return n, reject("invalid request settings")
	}
	return n, e
}

// Safe protocol receipt fields omit raw provider errors, opaque deferred handles,
// HTTP headers and metadata. Text/thinking are retained only at terminal commit.
type MessageReceipt struct {
	Role       goai.Role           `json:"role"`
	Content    []goai.ContentBlock `json:"content"`
	Api        goai.Api            `json:"api,omitempty"`
	Provider   goai.Provider       `json:"provider,omitempty"`
	Model      string              `json:"model,omitempty"`
	Usage      *goai.Usage         `json:"usage,omitempty"`
	StopReason goai.StopReason     `json:"stopReason,omitempty"`
	Timestamp  int64               `json:"timestamp,omitempty"`
	ErrorCode  string              `json:"errorCode,omitempty"`
}

type messageReceipt = MessageReceipt

func receiptMessage(r messageReceipt) goai.Message {
	return goai.Message{Role: r.Role, Content: r.Content, Api: r.Api, Provider: r.Provider, Model: r.Model, Usage: r.Usage, StopReason: r.StopReason, Timestamp: r.Timestamp, ErrorMessage: r.ErrorCode}
}
func userReceipt(text string) messageReceipt {
	return messageReceipt{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: text}}}
}
