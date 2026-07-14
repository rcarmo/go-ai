// Package pimessages implements pi's native assistant-message SSE protocol.
package pimessages

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
	"github.com/rcarmo/go-ai/transports/sse"
)

func init() {
	goai.RegisterApi(&goai.ApiProvider{Api: goai.ApiPiMessages, Stream: stream, StreamSimple: stream})
}

type responseError struct {
	message string
	code    any
	details map[string]any
}

func (e *responseError) Error() string { return e.message }
func (e *responseError) Code() any     { return e.code }

func stream(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	ch := make(chan goai.Event, 32)
	go func() {
		defer close(ch)
		apiKey := resolveAPIKey(model, opts)
		if apiKey == "" {
			//lint:ignore ST1005 upstream pi-ai exact error string starts with a capital letter.
			ch <- createErrorEvent(model, fmt.Errorf("No API key provided for provider \"%s\"", model.Provider), false)
			return
		}
		base := strings.TrimRight(model.BaseURL, "/")
		u, err := url.Parse(base + "/messages")
		if err != nil {
			ch <- createErrorEvent(model, err, false)
			return
		}
		if opts != nil && boolMeta(opts, "debug") {
			q := u.Query()
			q.Set("debug", "1")
			u.RawQuery = q.Encode()
		}
		body := map[string]any{"model": model.ID, "context": convCtx, "options": buildOptions(opts)}
		payload, err := goai.InvokeOnPayload(opts, body, model)
		if err != nil {
			ch <- createErrorEvent(model, err, false)
			return
		}
		data, err := json.Marshal(payload)
		if err != nil {
			ch <- createErrorEvent(model, err, false)
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(data))
		if err != nil {
			ch <- createErrorEvent(model, err, false)
			return
		}
		req.Header.Set("authorization", "Bearer "+apiKey)
		req.Header.Set("accept", "text/event-stream")
		req.Header.Set("content-type", "application/json")
		if opts != nil {
			goai.ApplyHeaders(req.Header, opts.Headers)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			ch <- createErrorEvent(model, err, ctx.Err() != nil)
			return
		}
		defer resp.Body.Close()
		if opts != nil && opts.OnResponse != nil {
			opts.OnResponse(resp.StatusCode, headerRecord(resp.Header), model)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(resp.Body)
			ch <- createErrorEvent(model, newResponseError(model, u.String(), resp, string(b)), false)
			return
		}
		sawTerminal := false
		convert := newConverter(model)
		for ev := range sse.Parse(resp.Body) {
			if ev.Event == sse.EventError {
				ch <- createErrorEvent(model, fmt.Errorf("%s", ev.Data), false)
				return
			}
			if strings.TrimSpace(ev.Data) == "" || strings.TrimSpace(ev.Data) == "[DONE]" {
				continue
			}
			out, terminal, err := convert([]byte(ev.Data))
			if err != nil {
				ch <- createErrorEvent(model, err, false)
				return
			}
			if out != nil {
				ch <- out
			}
			if terminal {
				sawTerminal = true
				return
			}
		}
		if !sawTerminal {
			ch <- createErrorEvent(model, fmt.Errorf("%s stream ended without a terminal event", model.Provider), false)
		}
	}()
	return ch
}

func resolveAPIKey(model *goai.Model, opts *goai.StreamOptions) string {
	if k := goai.ResolveAPIKey(model, opts); k != "" {
		return k
	}
	env := goai.ProviderEnvFromOptions(opts)
	if v := goai.GetProviderEnvValue("RADIUS_API_KEY", env); v != "" {
		return v
	}
	return goai.GetProviderEnvValue("PI_MESSAGES_API_KEY", env)
}
func buildOptions(opts *goai.StreamOptions) map[string]any {
	m := map[string]any{}
	if opts == nil {
		return m
	}
	if opts.Temperature != nil {
		m["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		m["maxTokens"] = *opts.MaxTokens
	}
	if opts.Reasoning != nil {
		m["reasoning"] = *opts.Reasoning
	}
	if opts.CacheRetention != "" {
		m["cacheRetention"] = opts.CacheRetention
	} else if goai.GetProviderEnvValue("PI_CACHE_RETENTION", opts.Env) == "long" {
		m["cacheRetention"] = "long"
	}
	if opts.SessionID != "" {
		m["sessionId"] = opts.SessionID
	}
	if v, ok := opts.Metadata["toolChoice"]; ok {
		m["toolChoice"] = v
	}
	return m
}
func boolMeta(opts *goai.StreamOptions, k string) bool {
	v, ok := opts.Metadata[k]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}
func headerRecord(h http.Header) map[string]string {
	m := map[string]string{}
	for k, v := range h {
		if len(v) > 0 {
			m[strings.ToLower(k)] = v[0]
		}
	}
	return m
}
func emptyUsage() *goai.Usage { return &goai.Usage{Cost: goai.CostBreakdown{}} }

func newResponseError(model *goai.Model, url string, resp *http.Response, body string) error {
	var parsed map[string]any
	_ = json.Unmarshal([]byte(body), &parsed)
	var msg, code string
	if er, ok := parsed["error"].(map[string]any); ok {
		msg, _ = er["message"].(string)
		code, _ = er["code"].(string)
	}
	suffix := body
	if msg != "" {
		suffix = msg
	}
	text := fmt.Sprintf("%d %s: %s", resp.StatusCode, resp.Status, suffix)
	if code != "" {
		text += " (" + code + ")"
	}
	if len(body) > 8192 {
		body = body[:8192] + "…"
	}
	return &responseError{text, code, map[string]any{"version": 1, "provider": model.Provider, "model": model.ID, "url": url, "status": resp.StatusCode, "statusText": resp.Status, "body": body, "timestampMs": time.Now().UnixMilli()}}
}
func createErrorEvent(model *goai.Model, err error, aborted bool) goai.Event {
	reason := goai.StopReasonError
	if aborted {
		reason = goai.StopReasonAborted
	}
	msg := &goai.Message{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, Usage: emptyUsage(), StopReason: reason, ErrorMessage: err.Error(), Timestamp: time.Now().UnixMilli()}
	if re, ok := err.(*responseError); ok && !aborted {
		goai.AppendAssistantMessageDiagnostic(msg, goai.CreateAssistantMessageDiagnostic("pi_messages_response_failure", re, re.details))
	}
	return &goai.ErrorEvent{Reason: reason, Error: msg, Err: err}
}

func newConverter(model *goai.Model) func([]byte) (goai.Event, bool, error) {
	partial := &goai.Message{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, Usage: emptyUsage(), StopReason: goai.StopReasonStop, Timestamp: time.Now().UnixMilli()}
	toolJSON := map[int]string{}
	return func(data []byte) (goai.Event, bool, error) {
		var e map[string]any
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, false, err
		}
		typ, _ := e["type"].(string)
		idx := int(num(e["contentIndex"]))
		switch typ {
		case "done":
			finish(partial, e)
			appendRewrite(partial, e["rewrite"])
			return &goai.DoneEvent{Reason: partial.StopReason, Message: partial}, true, nil
		case "error":
			finish(partial, e)
			if s, _ := e["errorMessage"].(string); s != "" {
				partial.ErrorMessage = s
			}
			appendRewrite(partial, e["rewrite"])
			return &goai.ErrorEvent{Reason: partial.StopReason, Error: partial, Err: fmt.Errorf("%s", partial.ErrorMessage)}, true, nil
		case "text_start":
			ensure(&partial.Content, idx)
			partial.Content[idx] = goai.ContentBlock{Type: "text"}
			return &goai.TextStartEvent{ContentIndex: idx, Partial: partial}, false, nil
		case "text_delta":
			d, _ := e["delta"].(string)
			partial.Content[idx].Text += d
			return &goai.TextDeltaEvent{ContentIndex: idx, Delta: d, Partial: partial}, false, nil
		case "text_end":
			c, _ := e["content"].(string)
			sig, _ := e["contentSignature"].(string)
			partial.Content[idx].Text = c
			partial.Content[idx].TextSignature = sig
			return &goai.TextEndEvent{ContentIndex: idx, Content: c, Partial: partial}, false, nil
		case "thinking_start":
			ensure(&partial.Content, idx)
			partial.Content[idx] = goai.ContentBlock{Type: "thinking"}
			return &goai.ThinkingStartEvent{ContentIndex: idx, Partial: partial}, false, nil
		case "thinking_delta":
			d, _ := e["delta"].(string)
			partial.Content[idx].Thinking += d
			return &goai.ThinkingDeltaEvent{ContentIndex: idx, Delta: d, Partial: partial}, false, nil
		case "thinking_end":
			c, _ := e["content"].(string)
			sig, _ := e["contentSignature"].(string)
			red, _ := e["redacted"].(bool)
			partial.Content[idx].Thinking = c
			partial.Content[idx].ThinkingSignature = sig
			partial.Content[idx].Redacted = red
			return &goai.ThinkingEndEvent{ContentIndex: idx, Content: c, Partial: partial}, false, nil
		case "toolcall_start":
			ensure(&partial.Content, idx)
			id, _ := e["id"].(string)
			name, _ := e["toolName"].(string)
			partial.Content[idx] = goai.ContentBlock{Type: "toolCall", ID: id, Name: name, Arguments: map[string]any{}}
			toolJSON[idx] = ""
			return &goai.ToolCallStartEvent{ContentIndex: idx, Partial: partial}, false, nil
		case "toolcall_delta":
			d, _ := e["delta"].(string)
			toolJSON[idx] += d
			if args, ok := jsonparse.ParsePartialJSON(toolJSON[idx]); ok && args != nil {
				partial.Content[idx].Arguments = args
			}
			return &goai.ToolCallDeltaEvent{ContentIndex: idx, Delta: d, Partial: partial}, false, nil
		case "toolcall_end":
			if tc, ok := e["toolCall"].(map[string]any); ok {
				applyTool(&partial.Content[idx], tc)
			}
			delete(toolJSON, idx)
			return &goai.ToolCallEndEvent{ContentIndex: idx, ToolCall: goai.ToolCall{Type: "toolCall", ID: partial.Content[idx].ID, Name: partial.Content[idx].Name, Arguments: partial.Content[idx].Arguments}, Partial: partial}, false, nil
		case "start":
			return &goai.StartEvent{Partial: partial}, false, nil
		}
		return &goai.StartEvent{Partial: partial}, false, nil
	}
}
func ensure(c *[]goai.ContentBlock, idx int) {
	for len(*c) <= idx {
		*c = append(*c, goai.ContentBlock{})
	}
}
func num(v any) float64 { n, _ := v.(float64); return n }
func finish(m *goai.Message, e map[string]any) {
	if r, _ := e["reason"].(string); r != "" {
		m.StopReason = goai.StopReason(r)
	}
	if id, _ := e["responseId"].(string); id != "" {
		m.ResponseID = id
	}
	if u, ok := e["usage"].(map[string]any); ok {
		m.Usage = &goai.Usage{Input: int(num(u["input"])), Output: int(num(u["output"])), CacheRead: int(num(u["cacheRead"])), CacheWrite: int(num(u["cacheWrite"])), TotalTokens: int(num(u["totalTokens"])), Reasoning: int(num(u["reasoning"])), Cost: goai.CostBreakdown{}}
	}
}
func appendRewrite(m *goai.Message, v any) {
	if r, ok := v.(map[string]any); ok {
		goai.AppendAssistantMessageDiagnostic(m, goai.AssistantMessageDiagnostic{Type: "pi_messages_rewrite", Timestamp: time.Now().UnixMilli(), Details: r})
	}
}
func applyTool(c *goai.ContentBlock, tc map[string]any) {
	if id, _ := tc["id"].(string); id != "" {
		c.ID = id
	}
	if name, _ := tc["name"].(string); name != "" {
		c.Name = name
	}
	if args, ok := tc["arguments"].(map[string]any); ok {
		c.Arguments = args
	}
}
