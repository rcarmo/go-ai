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

// State tracks call count for the faux provider.
type State struct {
	CallCount int64
}

// Registration holds the faux provider's models and response queue.
type Registration struct {
	Api    goai.Api
	Models []*goai.Model
	State  *State

	mu              sync.Mutex
	responses       []ResponseStep
	tokensPerSecond int
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

// Options configures the faux provider registration.
type Options struct {
	Api             string
	Provider        string
	Models          []ModelDef
	TokensPerSecond int // simulated streaming speed (default: 1000)
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
		tokensPerSecond: tps,
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
		Api:          api,
		Stream:       reg.stream,
		StreamSimple: reg.stream,
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
	return &goai.Message{
		Role: goai.RoleAssistant,
		Content: []goai.ContentBlock{
			{Type: "toolCall", ID: fmt.Sprintf("call_%s_%d", name, time.Now().UnixNano()), Name: name, Arguments: args},
		},
		StopReason: goai.StopReasonToolUse,
		Usage:      &goai.Usage{Input: 100, Output: 50, TotalTokens: 150},
		Timestamp:  time.Now().UnixMilli(),
	}
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

		var msg *goai.Message
		switch s := step.(type) {
		case *goai.Message:
			msg = s
		case ResponseFactory:
			msg = s(convCtx, opts, &State{CallCount: callNum})
		case nil:
			msg = TextMessage(fmt.Sprintf("Faux response #%d (no responses queued)", callNum))
		default:
			msg = ErrorMessage(fmt.Sprintf("unknown response step type: %T", step))
		}

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
