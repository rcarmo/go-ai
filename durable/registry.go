package durable

import (
	"context"
	"encoding/json"
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
	Temperature   *float64              `json:"temperature,omitempty"`
	MaxTokens     *int                  `json:"maxTokens,omitempty"`
	ToolExecution string                `json:"toolExecution,omitempty"`
	SteeringMode  string                `json:"steeringMode,omitempty"`
	FollowUpMode  string                `json:"followUpMode,omitempty"`
	Retry         RetryPolicy           `json:"retry,omitempty"`
	Compaction    CompactionPolicy      `json:"compaction,omitempty"`
	Deferred      *goai.DeferredOptions `json:"deferred,omitempty"`
}
type AgentChange struct {
	Name         string
	Cwd          string
	Model        ModelRef
	SystemPrompt string
	Settings     RequestSettings
	// Nil selects all installed extensions/tools; a pointer to an empty slice
	// selects none. Names are persisted; executable registrations stay local.
	Extensions *[]string
	Tools      *[]string
}
type Options struct {
	Now func() int64
	// Env resolves process-local capabilities for each tool use, off the Session
	// line. Only agent cwd is persisted; environments and credentials are not.
	Env func(context.Context, EnvTarget) (ExecutionEnvironment, error)
	// OnReport runs off-line after reservation rollback/adoption. It must return
	// promptly and must not synchronously join this Harness (Close/Wait APIs).
	// Panic is contained; no detached unbounded reporting goroutines are used.
	OnReport func(error)
	Registry *Registry
	// Sections render process-local prompt contributions before request intent.
	// Only rendered text and ordering are persisted, never callbacks.
	Sections []PromptSection
	Models   func(goai.Provider, string) *goai.Model
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
	// RetryConfig is json:"-" but changes remote effect/retry behavior. It is
	// not an authentication/transport DTO and must not evade intent pinning.
	if o.RetryConfig != nil {
		return nil, reject("unpinned retry policy unsupported")
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
	n.RetryConfig = nil
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
	if e == nil && (n.SteeringMode != "" && n.SteeringMode != "one" && n.SteeringMode != "all" || n.FollowUpMode != "" && n.FollowUpMode != "one" && n.FollowUpMode != "all") {
		return n, reject("invalid queue mode")
	}
	if e == nil && n.ToolExecution != "" && n.ToolExecution != "parallel" && n.ToolExecution != "sequential" {
		e = reject("invalid tool execution mode")
	}
	if e == nil {
		e = validateRetryPolicy(n.Retry)
	}
	if e == nil && (n.Compaction.TriggerTokens < 0 || n.Compaction.KeepRecentTokens < 0 || n.Compaction.MaxTokens < 0 || n.Compaction.ReserveTokens < 0 || n.Compaction.BackgroundTokens < 0) {
		e = reject("invalid compaction policy")
	}
	return n, e
}

// Safe protocol receipt fields omit raw provider errors, opaque deferred handles,
// HTTP headers and metadata. Text/thinking are retained only at terminal commit.
type MessageReceipt struct {
	Role            goai.Role            `json:"role"`
	Content         []goai.ContentBlock  `json:"content"`
	Api             goai.Api             `json:"api,omitempty"`
	Provider        goai.Provider        `json:"provider,omitempty"`
	Model           string               `json:"model,omitempty"`
	Usage           *goai.Usage          `json:"usage,omitempty"`
	StopReason      goai.StopReason      `json:"stopReason,omitempty"`
	Timestamp       int64                `json:"timestamp,omitempty"`
	ErrorCode       string               `json:"errorCode,omitempty"`
	Retryable       bool                 `json:"retryable,omitempty"`
	ContextOverflow bool                 `json:"contextOverflow,omitempty"`
	Deferred        *goai.DeferredHandle `json:"deferred,omitempty"`
	ToolCallID      string               `json:"toolCallId,omitempty"`
	ToolName        string               `json:"toolName,omitempty"`
	IsError         bool                 `json:"isError,omitempty"`
	Details         JSON                 `json:"details,omitempty"`
	// Non-object tool details use an explicit tagged adaptation; legacy Details
	// objects keep their existing wire shape. Neither admits executable values.
	DetailsValue any                  `json:"detailsValue,omitempty"`
	HasDetails   bool                 `json:"hasDetails,omitempty"`
	Sections     map[string]*string   `json:"sections,omitempty"`
	SectionOrder []string             `json:"sectionOrder,omitempty"`
	ToolsAdded   []ContributionTool   `json:"toolsAdded,omitempty"`
	ToolsRemoved []goai.ToolReference `json:"toolsRemoved,omitempty"`
	// EmptyArguments witnesses validated empty tool-call objects which the
	// legacy ContentBlock omitempty envelope cannot otherwise preserve.
	// Indices must be unique, in bounds and point only to empty toolCall args.
	EmptyArguments []int `json:"emptyArguments,omitempty"`
}

// ContributionTool owns decoded strict schema data instead of a RawMessage
// marshaler. Conversion to provider JSON happens only at request projection.
type ContributionTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  JSON   `json:"parameters"`
}

type messageReceipt = MessageReceipt

func receiptMessage(r messageReceipt) goai.Message {
	content := append([]goai.ContentBlock(nil), r.Content...)
	for _, index := range r.EmptyArguments {
		if index >= 0 && index < len(content) && content[index].Type == "toolCall" && len(content[index].Arguments) == 0 {
			content[index].Arguments = map[string]any{}
		}
	}
	m := goai.Message{Role: r.Role, Content: content, Api: r.Api, Provider: r.Provider, Model: r.Model, Usage: r.Usage, StopReason: r.StopReason, Timestamp: r.Timestamp, ErrorMessage: r.ErrorCode, ToolCallID: r.ToolCallID, ToolName: r.ToolName, IsError: r.IsError, Sections: r.Sections, ToolsRemoved: r.ToolsRemoved}
	if r.Details != nil {
		m.Details = r.Details
	}
	if r.HasDetails {
		m.Details = r.DetailsValue
	}
	for _, tool := range r.ToolsAdded {
		parameters, _ := json.Marshal(tool.Parameters)
		m.ToolsAdded = append(m.ToolsAdded, goai.Tool{Name: tool.Name, Description: tool.Description, Parameters: parameters})
	}
	return m
}
func userReceipt(text string) messageReceipt {
	return messageReceipt{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: text}}}
}

// contributionReceipt projects only the supported persisted contribution
// fields. Opaque/provider-control data rejects before staging rather than being
// silently dropped or broadening terminal receipt redaction.
func contributionReceipt(m goai.Message, l Limits) (messageReceipt, error) {
	if m.Deferred != nil || m.ResponseID != "" || m.ResponseModel != "" || m.ProviderThinkingLevel != "" || m.ThinkingLevel != "" || len(m.Diagnostics) != 0 || m.RawStopReason != "" || m.ErrorMessage != "" || m.EndTurn != nil || m.NestedCalls != nil || len(m.AddedToolNames) != 0 {
		return messageReceipt{}, reject("unsupported opaque message contribution fields")
	}
	if m.Role != goai.RoleUser && m.Role != goai.RoleAssistant && m.Role != goai.RoleToolResult && m.Role != goai.RoleSystem {
		return messageReceipt{}, reject("invalid contribution role")
	}
	if m.Role != goai.RoleSystem && (len(m.Sections) != 0 || len(m.ToolsAdded) != 0 || len(m.ToolsRemoved) != 0) {
		return messageReceipt{}, reject("system contribution fields on non-system role")
	}
	if m.Role != goai.RoleToolResult && m.Details != nil {
		return messageReceipt{}, reject("details require tool-result role")
	}
	r := messageReceipt{Role: m.Role, Api: m.Api, Provider: m.Provider, Model: m.Model, Usage: m.Usage, StopReason: m.StopReason, Timestamp: m.Timestamp, ToolCallID: m.ToolCallID, ToolName: m.ToolName, IsError: m.IsError, Content: make([]goai.ContentBlock, 0, len(m.Content))}
	for _, c := range m.Content {
		if c.TextSignature != "" || c.TextSignaturePresent || c.ThinkingSignature != "" || c.ThinkingSignaturePresent || c.Redacted || c.RedactedPresent || c.ThoughtSignature != "" || c.ThoughtSignaturePresent || c.Namespace != "" || c.NamespacePresent {
			return messageReceipt{}, reject("unsupported opaque contribution signature/control")
		}
		switch c.Type {
		case "text":
			if c.Arguments != nil || c.ID != "" || c.Name != "" || c.Data != "" || c.MimeType != "" || c.Thinking != "" {
				return messageReceipt{}, reject("text contribution shape")
			}
			r.Content = append(r.Content, goai.ContentBlock{Type: "text", Text: c.Text})
		case "thinking":
			if m.Role != goai.RoleAssistant || c.Arguments != nil || c.ID != "" || c.Name != "" || c.Data != "" || c.Text != "" {
				return messageReceipt{}, reject("thinking contribution shape")
			}
			r.Content = append(r.Content, goai.ContentBlock{Type: "thinking", Thinking: c.Thinking})
		case "image":
			if m.Role != goai.RoleUser && m.Role != goai.RoleToolResult {
				return messageReceipt{}, reject("image contribution role")
			}
			if c.Arguments != nil || c.Text != "" || c.ID != "" || c.Thinking != "" || c.Name != "" {
				return messageReceipt{}, reject("image contribution shape")
			}
			r.Content = append(r.Content, goai.ContentBlock{Type: "image", Data: c.Data, MimeType: c.MimeType})
		case "toolCall":
			if m.Role != goai.RoleAssistant || c.ID == "" || c.Name == "" || c.Arguments == nil || c.Text != "" || c.Data != "" || c.Thinking != "" {
				return messageReceipt{}, reject("tool-call contribution shape")
			}
			args, e := copyObject(JSON(c.Arguments), l)
			if e != nil {
				return messageReceipt{}, e
			}
			if len(args) == 0 {
				r.EmptyArguments = append(r.EmptyArguments, len(r.Content))
			}
			r.Content = append(r.Content, goai.ContentBlock{Type: "toolCall", ID: c.ID, Name: c.Name, Arguments: args})
		default:
			return messageReceipt{}, reject("unsupported contribution content")
		}
	}
	if !validUsage(r.Usage) {
		return messageReceipt{}, reject("invalid contribution usage")
	}
	if m.Sections != nil {
		r.Sections = map[string]*string{}
		for key, value := range m.Sections {
			if value == nil {
				r.Sections[key] = nil
			} else {
				copy := *value
				r.Sections[key] = &copy
			}
		}
	}
	for _, tool := range m.ToolsAdded {
		if !validKind(tool.Name) || tool.ConstrainedSampling != nil {
			return messageReceipt{}, reject("unsupported tool contribution control")
		}
		var parameters JSON
		if e := decodeStrict(tool.Parameters, l, l.MaxRecordBytes, &parameters); e != nil {
			return messageReceipt{}, e
		}
		r.ToolsAdded = append(r.ToolsAdded, ContributionTool{Name: tool.Name, Description: tool.Description, Parameters: parameters})
	}
	r.ToolsRemoved = append([]goai.ToolReference(nil), m.ToolsRemoved...)
	for _, tool := range r.ToolsRemoved {
		if !validKind(tool.Name) {
			return messageReceipt{}, reject("invalid removed tool contribution")
		}
	}
	if m.Details != nil {
		value, e := ownJSONValue(m.Details, l)
		if e != nil {
			return messageReceipt{}, e
		}
		if object, ok := value.(map[string]any); ok {
			r.Details = JSON(object)
		} else {
			r.DetailsValue = value
			r.HasDetails = true
		}
	}
	object, e := dtoObject(r, l)
	if e != nil {
		return messageReceipt{}, e
	}
	var owned messageReceipt
	if e = fromObject(object, &owned, l); e != nil {
		return messageReceipt{}, e
	}
	if e = restoreReceiptArguments(&owned, l); e != nil {
		return messageReceipt{}, e
	}
	return owned, nil
}

// Restore only a consistent serialized empty-object variant. Caller protocol
// inputs are validated separately before any witness is created.
func restoreReceiptArguments(r *MessageReceipt, l Limits) error {
	seen := map[int]bool{}
	for _, index := range r.EmptyArguments {
		if index < 0 || index >= len(r.Content) || seen[index] || r.Role != goai.RoleAssistant {
			return reject("invalid empty argument witness")
		}
		block := &r.Content[index]
		if block.Type != "toolCall" || block.ID == "" || block.Name == "" || len(block.Arguments) != 0 {
			return reject("inconsistent empty argument witness")
		}
		seen[index] = true
		block.Arguments = map[string]any{}
	}
	for _, block := range r.Content {
		if block.Type == "toolCall" {
			if block.Arguments == nil {
				return reject("tool-call arguments absent without witness")
			}
			if _, e := copyObject(JSON(block.Arguments), l); e != nil {
				return e
			}
		}
	}
	return nil
}
