// Package faux provides a test-double LLM provider for unit testing.
//
// Faux providers return pre-configured responses with simulated streaming
// delays, making them ideal for testing tool-calling loops, context
// management, and event processing without hitting real APIs.
package faux

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// ResponseFactory generates a response dynamically based on context and state.
type ResponseFactory func(ctx *goai.Context, opts *goai.StreamOptions, state *State) *goai.Message

// ResponseStep is either a static *goai.Message or a ResponseFactory.
type ResponseStep interface{}

// State tracks call count and deferred lifecycle counters for the faux provider.
type State struct {
	CallCount          int64
	DeferredFetchCount int64
	CancelledDeferred  []goai.DeferredHandle
}

// Registration holds the faux provider's models and response queue.
type Registration struct {
	Api    goai.Api
	Models []*goai.Model
	State  *State

	mu              sync.Mutex
	responses       []ResponseStep
	deferred        map[string]*deferredEntry
	tokensPerSecond int
	deferredOptions FauxDeferredOptions
}

// ModelDef defines a faux model for registration.
type ModelDef struct {
	ID            string
	Name          string
	Reasoning     bool
	Input         []string
	Cost          goai.ModelCost
	ContextWindow int
	MaxTokens     int
}

// FauxDeferredOptions configures deterministic deferred-response behavior.
type FauxDeferredOptions struct {
	PendingFetches int
	PollAfterMs    int
}

// Options configures the faux provider registration.
type Options struct {
	Api             string
	Provider        string
	Models          []ModelDef
	TokensPerSecond int // simulated streaming speed (default: 1000)
	Deferred        FauxDeferredOptions
}

type deferredEntry struct {
	handle         goai.DeferredHandle
	step           ResponseStep
	context        *goai.Context
	options        *goai.StreamOptions
	model          *goai.Model
	pendingFetches int
	cancelled      bool
	final          *goai.Message
}

// Register creates and registers a new faux provider.
// Returns a Registration that can be used to set responses and inspect state.
func Register(opts *Options) *Registration {
	if opts == nil {
		opts = &Options{}
	}

	api := goai.Api("faux")
	if opts.Api != "" {
		api = goai.Api(opts.Api)
	}
	provider := goai.Provider("faux")
	if opts.Provider != "" {
		provider = goai.Provider(opts.Provider)
	}

	tps := 1000
	if opts.TokensPerSecond > 0 {
		tps = opts.TokensPerSecond
	}

	reg := &Registration{
		Api:             api,
		State:           &State{},
		deferred:        map[string]*deferredEntry{},
		tokensPerSecond: tps,
		deferredOptions: opts.Deferred,
	}

	// Create models
	if len(opts.Models) == 0 {
		opts.Models = []ModelDef{{
			ID: "faux-model", Name: "Faux Model",
			Input: []string{"text"}, ContextWindow: 128000, MaxTokens: 4096,
		}}
	}

	for _, md := range opts.Models {
		m := &goai.Model{
			ID:            md.ID,
			Name:          md.Name,
			Api:           api,
			Provider:      provider,
			Reasoning:     md.Reasoning,
			Input:         md.Input,
			Cost:          md.Cost,
			ContextWindow: md.ContextWindow,
			MaxTokens:     md.MaxTokens,
		}
		if m.Name == "" {
			m.Name = m.ID
		}
		if len(m.Input) == 0 {
			m.Input = []string{"text"}
		}
		if m.ContextWindow == 0 {
			m.ContextWindow = 128000
		}
		if m.MaxTokens == 0 {
			m.MaxTokens = 4096
		}
		reg.Models = append(reg.Models, m)
		goai.RegisterModel(m)
	}

	// Register the API provider
	goai.RegisterApi(&goai.ApiProvider{
		Api:            api,
		Stream:         reg.stream,
		StreamSimple:   reg.stream,
		FetchDeferred:  reg.fetchDeferred,
		CancelDeferred: reg.cancelDeferred,
	})

	return reg
}

// SetResponses replaces the response queue.
func (r *Registration) SetResponses(responses []ResponseStep) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.responses = responses
}

// AppendResponses adds responses to the queue.
func (r *Registration) AppendResponses(responses []ResponseStep) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.responses = append(r.responses, responses...)
}

// PendingResponseCount returns how many responses are queued.
func (r *Registration) PendingResponseCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.responses)
}

// GetModel returns the first model, or a specific one by ID.
func (r *Registration) GetModel(id ...string) *goai.Model {
	if len(id) == 0 || id[0] == "" {
		return r.Models[0]
	}
	for _, m := range r.Models {
		if m.ID == id[0] {
			return m
		}
	}
	return nil
}

// --- Helpers for building responses ---

// AssistantMessageOptions configures FauxAssistantMessage metadata.
type AssistantMessageOptions struct {
	StopReason   goai.StopReason
	ErrorMessage string
	ResponseID   string
	Timestamp    int64
}

// FauxText creates a text content block, mirroring upstream fauxText.
func FauxText(text string) goai.ContentBlock {
	return goai.ContentBlock{Type: "text", Text: text}
}

// FauxThinking creates a thinking content block, mirroring upstream fauxThinking.
func FauxThinking(thinking string) goai.ContentBlock {
	return goai.ContentBlock{Type: "thinking", Thinking: thinking}
}

// FauxToolCall creates a tool-call content block, mirroring upstream fauxToolCall.
func FauxToolCall(name string, args map[string]interface{}, id ...string) goai.ContentBlock {
	toolCallID := ""
	if len(id) > 0 {
		toolCallID = id[0]
	}
	if toolCallID == "" {
		toolCallID = fmt.Sprintf("call_%s_%d", name, time.Now().UnixNano())
	}
	return goai.ContentBlock{Type: "toolCall", ID: toolCallID, Name: name, Arguments: args}
}

// FauxAssistantMessage creates an assistant message from a string, one content
// block, or multiple content blocks, mirroring upstream fauxAssistantMessage.
func FauxAssistantMessage(content interface{}, options ...AssistantMessageOptions) *goai.Message {
	msg := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, Usage: &goai.Usage{}, Timestamp: time.Now().UnixMilli()}
	switch c := content.(type) {
	case string:
		msg.Content = []goai.ContentBlock{FauxText(c)}
	case goai.ContentBlock:
		msg.Content = []goai.ContentBlock{c}
	case []goai.ContentBlock:
		msg.Content = append([]goai.ContentBlock(nil), c...)
	case nil:
		msg.Content = nil
	default:
		msg.Content = []goai.ContentBlock{FauxText(fmt.Sprintf("%v", c))}
	}
	if len(options) > 0 {
		opt := options[0]
		if opt.StopReason != "" {
			msg.StopReason = opt.StopReason
		}
		msg.ErrorMessage = opt.ErrorMessage
		msg.ResponseID = opt.ResponseID
		if opt.Timestamp != 0 {
			msg.Timestamp = opt.Timestamp
		}
	}
	if msg.StopReason == goai.StopReasonToolUse {
		return msg
	}
	for _, block := range msg.Content {
		if block.Type == "toolCall" && len(options) == 0 {
			msg.StopReason = goai.StopReasonToolUse
			break
		}
	}
	return msg
}

// TextMessage creates a simple text assistant message.
func TextMessage(text string) *goai.Message {
	return &goai.Message{
		Role: goai.RoleAssistant,
		Content: []goai.ContentBlock{
			{Type: "text", Text: text},
		},
		StopReason: goai.StopReasonStop,
		Usage:      &goai.Usage{Input: 100, Output: len(text) / 4, TotalTokens: 100 + len(text)/4},
		Timestamp:  time.Now().UnixMilli(),
	}
}

// ThinkingMessage creates an assistant message with thinking + text.
func ThinkingMessage(thinking, text string) *goai.Message {
	return &goai.Message{
		Role: goai.RoleAssistant,
		Content: []goai.ContentBlock{
			{Type: "thinking", Thinking: thinking},
			{Type: "text", Text: text},
		},
		StopReason: goai.StopReasonStop,
		Usage:      &goai.Usage{Input: 100, Output: (len(thinking) + len(text)) / 4, TotalTokens: 100 + (len(thinking)+len(text))/4},
		Timestamp:  time.Now().UnixMilli(),
	}
}

// ToolCallMessage creates an assistant message with a tool call.
func ToolCallMessage(name string, args map[string]interface{}) *goai.Message {
	msg := FauxAssistantMessage(FauxToolCall(name, args), AssistantMessageOptions{StopReason: goai.StopReasonToolUse})
	msg.Usage = &goai.Usage{Input: 100, Output: 50, TotalTokens: 150}
	return msg
}

// ErrorMessage creates an error assistant message.
func ErrorMessage(errMsg string) *goai.Message {
	return &goai.Message{
		Role:         goai.RoleAssistant,
		Content:      []goai.ContentBlock{},
		StopReason:   goai.StopReasonError,
		ErrorMessage: errMsg,
		Usage:        &goai.Usage{},
		Timestamp:    time.Now().UnixMilli(),
	}
}

// --- Stream implementation ---

func (r *Registration) stream(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	ch := make(chan goai.Event, 32)

	go func() {
		defer close(ch)

		goai.GetLogger().Debug("faux stream", "model", model.ID, "pendingResponses", r.PendingResponseCount())
		goai.GetLogger().Debug("stream start", "api", string(model.Api), "provider", model.Provider, "model", model.ID)
		callNum := atomic.AddInt64(&r.State.CallCount, 1)

		// Get next response
		r.mu.Lock()
		var step ResponseStep
		if len(r.responses) > 0 {
			step = r.responses[0]
			r.responses = r.responses[1:]
		}
		r.mu.Unlock()

		if opts != nil && opts.Deferred != nil {
			handle := goai.DeferredHandle{Provider: string(model.Provider), ModelID: model.ID, Api: string(model.Api), ID: fmt.Sprintf("deferred_%d", time.Now().UnixNano())}
			if opts.Deferred.PollAfterMs > 0 {
				handle.PollAfterMs = opts.Deferred.PollAfterMs
			} else if r.deferredOptions.PollAfterMs > 0 {
				handle.PollAfterMs = r.deferredOptions.PollAfterMs
			}
			r.mu.Lock()
			r.deferred[handle.ID] = &deferredEntry{handle: handle, step: step, context: convCtx, options: opts, model: model, pendingFetches: r.deferredOptions.PendingFetches}
			r.mu.Unlock()
			msg := &goai.Message{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, Content: []goai.ContentBlock{}, Usage: &goai.Usage{}, StopReason: goai.StopReasonDeferred, Deferred: &handle, Timestamp: time.Now().UnixMilli()}
			ch <- &goai.StartEvent{Partial: msg}
			ch <- &goai.DoneEvent{Reason: goai.StopReasonDeferred, Message: msg}
			return
		}

		msg := r.resolveStep(step, convCtx, opts, callNum)

		// Fill in model info
		msg.Api = model.Api
		msg.Provider = model.Provider
		msg.Model = model.ID
		if msg.Timestamp == 0 {
			msg.Timestamp = time.Now().UnixMilli()
		}

		// Simulate streaming
		ch <- &goai.StartEvent{Partial: msg}

		for i, block := range msg.Content {
			switch block.Type {
			case "text":
				ch <- &goai.TextStartEvent{ContentIndex: i, Partial: msg}
				// Stream character by character with delay
				delay := r.charDelay(block.Text)
				for _, chunk := range chunkText(block.Text, 10) {
					if ctx.Err() != nil {
						msg.StopReason = goai.StopReasonAborted
						ch <- &goai.ErrorEvent{Reason: goai.StopReasonAborted, Error: msg, Err: ctx.Err()}
						return
					}
					ch <- &goai.TextDeltaEvent{ContentIndex: i, Delta: chunk, Partial: msg}
					if delay > 0 {
						time.Sleep(delay)
					}
				}
				ch <- &goai.TextEndEvent{ContentIndex: i, Content: block.Text, Partial: msg}

			case "thinking":
				ch <- &goai.ThinkingStartEvent{ContentIndex: i, Partial: msg}
				delay := r.charDelay(block.Thinking)
				for _, chunk := range chunkText(block.Thinking, 10) {
					ch <- &goai.ThinkingDeltaEvent{ContentIndex: i, Delta: chunk, Partial: msg}
					if delay > 0 {
						time.Sleep(delay)
					}
				}
				ch <- &goai.ThinkingEndEvent{ContentIndex: i, Content: block.Thinking, Partial: msg}

			case "toolCall":
				ch <- &goai.ToolCallStartEvent{ContentIndex: i, Partial: msg}
				ch <- &goai.ToolCallEndEvent{
					ContentIndex: i,
					ToolCall: goai.ToolCall{
						Type: "toolCall", ID: block.ID, Name: block.Name, Arguments: block.Arguments,
					},
					Partial: msg,
				}
			}
		}

		// Final event
		if msg.StopReason == goai.StopReasonError {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: msg, Err: fmt.Errorf("%s", msg.ErrorMessage)}
		} else {
			ch <- &goai.DoneEvent{Reason: msg.StopReason, Message: msg}
		}
	}()

	return ch
}

func (r *Registration) charDelay(text string) time.Duration {
	if r.tokensPerSecond <= 0 || len(text) == 0 {
		return 0
	}
	tokens := len(text) / 4
	if tokens == 0 {
		tokens = 1
	}
	totalDuration := time.Duration(float64(time.Second) * float64(tokens) / float64(r.tokensPerSecond))
	chunks := len(text) / 10
	if chunks == 0 {
		chunks = 1
	}
	return totalDuration / time.Duration(chunks)
}

func chunkText(text string, size int) []string {
	var chunks []string
	for i := 0; i < len(text); i += size {
		end := i + size
		if end > len(text) {
			end = len(text)
		}
		chunks = append(chunks, text[i:end])
	}
	return chunks
}

func (r *Registration) resolveStep(step ResponseStep, convCtx *goai.Context, opts *goai.StreamOptions, callNum int64) *goai.Message {
	switch s := step.(type) {
	case *goai.Message:
		return s
	case ResponseFactory:
		return s(convCtx, opts, &State{CallCount: callNum})
	case error:
		return ErrorMessage(s.Error())
	case nil:
		return TextMessage(fmt.Sprintf("Faux response #%d (no responses queued)", callNum))
	default:
		return ErrorMessage(fmt.Sprintf("unknown response step type: %T", step))
	}
}

func (r *Registration) fetchDeferred(ctx context.Context, model *goai.Model, handle goai.DeferredHandle, opts *goai.StreamOptions) <-chan goai.Event {
	ch := make(chan goai.Event, 16)
	go func() {
		defer close(ch)
		atomic.AddInt64(&r.State.DeferredFetchCount, 1)
		r.mu.Lock()
		entry := r.deferred[handle.ID]
		if entry != nil && (entry.handle.Provider != handle.Provider || entry.handle.ModelID != handle.ModelID || entry.handle.Api != handle.Api) {
			entry = nil
		}
		if entry == nil {
			r.mu.Unlock()
			msg := ErrorMessage("unknown faux deferred response: " + handle.ID)
			msg.Api, msg.Provider, msg.Model = model.Api, model.Provider, model.ID
			ch <- &goai.StartEvent{Partial: msg}
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: msg, Err: fmt.Errorf("%s", msg.ErrorMessage)}
			return
		}
		if entry.cancelled {
			r.mu.Unlock()
			msg := ErrorMessage("faux deferred response was cancelled: " + handle.ID)
			msg.Api, msg.Provider, msg.Model = model.Api, model.Provider, model.ID
			ch <- &goai.StartEvent{Partial: msg}
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: msg, Err: fmt.Errorf("%s", msg.ErrorMessage)}
			return
		}
		if entry.pendingFetches > 0 {
			entry.pendingFetches--
			pending := entry.handle
			r.mu.Unlock()
			msg := &goai.Message{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, Content: []goai.ContentBlock{}, Usage: &goai.Usage{}, StopReason: goai.StopReasonDeferred, Deferred: &pending, Timestamp: time.Now().UnixMilli()}
			ch <- &goai.StartEvent{Partial: msg}
			ch <- &goai.DoneEvent{Reason: goai.StopReasonDeferred, Message: msg}
			return
		}
		if entry.final == nil {
			entry.final = r.resolveStep(entry.step, entry.context, entry.options, atomic.LoadInt64(&r.State.CallCount))
		}
		msg := entry.final
		r.mu.Unlock()
		r.emitMessage(ctx, model, msg, ch)
	}()
	return ch
}

func (r *Registration) cancelDeferred(ctx context.Context, model *goai.Model, handle goai.DeferredHandle, opts *goai.StreamOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.State.CancelledDeferred = append(r.State.CancelledDeferred, handle)
	if entry := r.deferred[handle.ID]; entry != nil {
		entry.cancelled = true
	}
	return nil
}

func (r *Registration) emitMessage(ctx context.Context, model *goai.Model, msg *goai.Message, ch chan<- goai.Event) {
	if msg == nil {
		msg = ErrorMessage("nil faux response")
	}
	msg.Api = model.Api
	msg.Provider = model.Provider
	msg.Model = model.ID
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().UnixMilli()
	}
	if msg.Usage == nil {
		msg.Usage = &goai.Usage{Input: 100, Output: 1, TotalTokens: 101}
	}
	ch <- &goai.StartEvent{Partial: msg}
	for i, block := range msg.Content {
		switch block.Type {
		case "text":
			ch <- &goai.TextStartEvent{ContentIndex: i, Partial: msg}
			if block.Text != "" {
				ch <- &goai.TextDeltaEvent{ContentIndex: i, Delta: block.Text, Partial: msg}
			}
			ch <- &goai.TextEndEvent{ContentIndex: i, Content: block.Text, Partial: msg}
		case "thinking":
			ch <- &goai.ThinkingStartEvent{ContentIndex: i, Partial: msg}
			if block.Thinking != "" {
				ch <- &goai.ThinkingDeltaEvent{ContentIndex: i, Delta: block.Thinking, Partial: msg}
			}
			ch <- &goai.ThinkingEndEvent{ContentIndex: i, Content: block.Thinking, Partial: msg}
		case "toolCall":
			ch <- &goai.ToolCallStartEvent{ContentIndex: i, Partial: msg}
			ch <- &goai.ToolCallEndEvent{ContentIndex: i, ToolCall: goai.ToolCall{Type: "toolCall", ID: block.ID, Name: block.Name, Arguments: block.Arguments}, Partial: msg}
		}
	}
	if err := ctx.Err(); err != nil {
		msg.StopReason = goai.StopReasonAborted
		ch <- &goai.ErrorEvent{Reason: goai.StopReasonAborted, Error: msg, Err: err}
		return
	}
	if msg.StopReason == goai.StopReasonError {
		ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: msg, Err: fmt.Errorf("%s", msg.ErrorMessage)}
		return
	}
	ch <- &goai.DoneEvent{Reason: msg.StopReason, Message: msg}
}
