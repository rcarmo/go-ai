package anthropic

import (
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	goai "github.com/rcarmo/go-ai"
)

const messageThreadsBeta = "message-threads-2026-08-12"
const maxThreadSessions = 10_000

type anthropicThreadEntry struct {
	session     string
	prefixCount int
	prefixHash  [sha256.Size]byte
	replyID     string
	off         bool
}

var anthropicThreads = struct {
	sync.Mutex
	bySession map[string]*list.Element
	order     list.List
}{bySession: map[string]*list.Element{}}

func init() {
	goai.RegisterSessionResourceCleanup(func(session string) error {
		anthropicThreads.Lock()
		defer anthropicThreads.Unlock()
		if session == "" {
			clear(anthropicThreads.bySession)
			anthropicThreads.order.Init()
		} else {
			deleteAnthropicThread(session)
		}
		return nil
	})
}

func deleteAnthropicThread(session string) {
	if entry := anthropicThreads.bySession[session]; entry != nil {
		anthropicThreads.order.Remove(entry)
		delete(anthropicThreads.bySession, session)
	}
}

func rememberAnthropicThread(entry anthropicThreadEntry) {
	anthropicThreads.Lock()
	defer anthropicThreads.Unlock()
	deleteAnthropicThread(entry.session)
	anthropicThreads.bySession[entry.session] = anthropicThreads.order.PushBack(entry)
	if anthropicThreads.order.Len() > maxThreadSessions {
		deleteAnthropicThread(anthropicThreads.order.Front().Value.(anthropicThreadEntry).session)
	}
}

func anthropicThreadSession(model *goai.Model, opts *goai.StreamOptions) string {
	if opts == nil || opts.SessionID == "" || goai.ResolveCacheRetention(opts.CacheRetention, goai.ProviderEnvFromOptions(opts)) == goai.CacheRetentionNone {
		return ""
	}
	supported := strings.Contains(model.BaseURL, "api.anthropic.com")
	if model.AnthropicCompat != nil && model.AnthropicCompat.SupportsMessageThreads != nil {
		supported = *model.AnthropicCompat.SupportsMessageThreads
	}
	if !supported {
		return ""
	}
	return opts.SessionID
}

type anthropicThreadRequest struct {
	session  string
	settings []byte
	messages []json.RawMessage
}

func prepareAnthropicThread(model *goai.Model, session, beta string, full []byte) (*anthropicThreadRequest, []byte) {
	var body map[string]json.RawMessage
	var messages []json.RawMessage
	if json.Unmarshal(full, &body) != nil || json.Unmarshal(body["messages"], &messages) != nil {
		return nil, full
	}
	settings := []any{model.Provider, model.BaseURL, beta}
	for _, key := range []string{"model", "system", "tools", "thinking", "output_config", "tool_choice", "betas", "fallbacks"} {
		settings = append(settings, body[key])
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return nil, full
	}
	request := &anthropicThreadRequest{session: session, settings: encoded, messages: messages}
	anthropicThreads.Lock()
	if state := anthropicThreads.bySession[session]; state != nil {
		entry := state.Value.(anthropicThreadEntry)
		if entry.off {
			anthropicThreads.Unlock()
			return nil, full
		}
		if len(messages) > entry.prefixCount && request.hash(messages[:entry.prefixCount]) == entry.prefixHash {
			// A reply can be continued only once, including concurrent callers.
			deleteAnthropicThread(session)
			anthropicThreads.Unlock()
			body["messages"], _ = json.Marshal(messages[entry.prefixCount:])
			delete(body, "tools")
			delete(body, "system")
			body["thread"], _ = json.Marshal(map[string]string{"type": "continue", "previous_message_id": entry.replyID})
			wire, err := json.Marshal(body)
			if err != nil {
				return nil, full
			}
			return request, wire
		}
	}
	anthropicThreads.Unlock()
	body["thread"] = json.RawMessage(`{"type":"create"}`)
	wire, err := json.Marshal(body)
	if err != nil {
		return nil, full
	}
	return request, wire
}

func (request *anthropicThreadRequest) hash(messages []json.RawMessage) [sha256.Size]byte {
	digest := sha256.New()
	digest.Write(request.settings)
	for _, raw := range messages {
		var message any
		if json.Unmarshal(raw, &message) == nil {
			normalizeThreadMessage(message)
			raw, _ = json.Marshal(message)
		}
		digest.Write(raw)
		digest.Write([]byte{'\n'})
	}
	return [sha256.Size]byte(digest.Sum(nil))
}

func normalizeThreadMessage(value any) {
	switch node := value.(type) {
	case map[string]any:
		delete(node, "cache_control")
		if text, ok := node["content"].(string); ok {
			node["content"] = []any{map[string]any{"type": "text", "text": text}}
		}
		for _, child := range node {
			normalizeThreadMessage(child)
		}
	case []any:
		for _, child := range node {
			normalizeThreadMessage(child)
		}
	}
}

func (request *anthropicThreadRequest) recordReply(model *goai.Model, opts *goai.StreamOptions, message *goai.Message) {
	if message == nil || message.ResponseID == "" || message.ResponseModel != "" {
		return
	}
	converted := buildRequest(model, &goai.Context{Messages: []goai.Message{*message}}, opts)
	messages := append([]json.RawMessage(nil), request.messages...)
	for _, reply := range converted.Messages {
		if reply.Role == "assistant" {
			wire, err := json.Marshal(reply)
			if err != nil {
				return
			}
			messages = append(messages, wire)
		}
	}
	rememberAnthropicThread(anthropicThreadEntry{session: request.session, prefixCount: len(messages), prefixHash: request.hash(messages), replyID: message.ResponseID})
}

// retryRejectedAnthropicThread retries only a thread rejection, before any
// stream events. The resend uses the already-hooked full request.
func retryRejectedAnthropicThread(ctx context.Context, response *http.Response, request *anthropicThreadRequest, full []byte, send func([]byte) (*http.Response, error)) (*http.Response, *anthropicThreadRequest, *goai.AssistantMessageDiagnostic, error) {
	if ctx.Err() != nil || response.StatusCode < 400 || response.StatusCode >= 500 {
		return response, request, nil, nil
	}
	original, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(original))
	var envelope struct {
		Error struct {
			Details struct {
				Code string `json:"error_code"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(original, &envelope)
	code := envelope.Error.Details.Code
	startNew := false
	switch code {
	case "thread_not_found", "thread_already_continued", "thread_continue_in_flight", "thread_fingerprint_mismatch":
		startNew = true
	}
	unsupported := code == "thread_unsupported_request" || code == "" && response.StatusCode == 400 && bytes.Contains(original, []byte("thread"))
	if !startNew && !unsupported {
		return response, request, nil, nil
	}
	if code == "" {
		code = strconv.Itoa(response.StatusCode)
	}
	diagnostic := &goai.AssistantMessageDiagnostic{Type: "anthropic_thread_fallback", Timestamp: time.Now().UnixMilli(),
		Error: goai.DiagnosticError{Message: fmt.Sprintf("HTTP %d: %s", response.StatusCode, original)}, Details: map[string]any{"reason": code}}
	response.Body.Close()
	if startNew {
		var body map[string]json.RawMessage
		if err := json.Unmarshal(full, &body); err != nil {
			return nil, nil, diagnostic, err
		}
		body["thread"] = json.RawMessage(`{"type":"create"}`)
		wire, err := json.Marshal(body)
		if err != nil {
			return nil, nil, diagnostic, err
		}
		retry, err := send(wire)
		return retry, request, diagnostic, err
	}
	retry, err := send(full)
	if err == nil && retry.StatusCode >= 200 && retry.StatusCode < 300 {
		rememberAnthropicThread(anthropicThreadEntry{session: request.session, off: true})
	}
	return retry, nil, diagnostic, err
}
