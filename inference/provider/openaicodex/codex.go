// Package openaicodex implements the OpenAI Codex Responses API provider.
//
// Uses WebSocket transport for streaming, falling back to SSE/HTTP.
// Requires OAuth authentication (ChatGPT Plus/Pro subscription).
package openaicodex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
	retryutil "github.com/rcarmo/go-ai/internal/retry"
	"github.com/rcarmo/go-ai/transports/sse"
	"github.com/rcarmo/go-ai/transports/websocket"
)

func init() {
	goai.RegisterApi(&goai.ApiProvider{
		Api:          goai.ApiOpenAICodexResponses,
		Stream:       streamCodex,
		StreamSimple: streamCodexSimple,
	})
	goai.RegisterSessionResourceCleanup(func(sessionID string) error {
		CloseOpenAICodexWebSocketSessions(sessionID)
		return nil
	})
}

func streamCodexSimple(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	return streamCodex(ctx, model, convCtx, opts)
}

func streamCodex(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	ch := make(chan goai.Event, 32)

	go func() {
		defer close(ch)
		goai.GetLogger().Debug("stream start", "api", "openai-codex-responses", "provider", model.Provider, "model", model.ID)

		apiKey := goai.ResolveAPIKey(model, opts)
		if apiKey == "" {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("No API key for provider: %s", model.Provider)}
			return
		}

		// Match pi-ai: default to auto, preferring WebSocket with SSE fallback.
		transport := goai.TransportAuto
		if opts != nil && opts.Transport != "" {
			transport = opts.Transport
		}

		var diagnostics []goai.AssistantMessageDiagnostic
		if transport == goai.TransportWebSocket || transport == goai.TransportWebSocketCached || transport == goai.TransportAuto {
			if opts != nil && opts.SessionID != "" && isCodexWebSocketSSEFallbackActive(opts.SessionID) {
				recordCodexWebSocketSSEFallback(opts.SessionID)
			} else {
				retriedConnectionLimit := false
				for {
					err := streamViaWebSocket(ctx, model, convCtx, opts, apiKey, ch)
					if err == nil {
						return
					}
					if ctx.Err() != nil {
						ch <- &goai.ErrorEvent{Reason: goai.StopReasonAborted, Err: ctx.Err()}
						return
					}
					connectionLimit := isWebSocketConnectionLimitReachedError(err)
					if connectionLimit && !retriedConnectionLimit {
						retriedConnectionLimit = true
						continue
					}
					if isCodexNonTransportError(err) && !connectionLimit {
						ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
						return
					}
					diagnostics = append(diagnostics, goai.CreateAssistantMessageDiagnostic("provider_transport_failure", err, map[string]any{
						"configuredTransport": string(transport),
						"fallbackTransport":   "sse",
						"eventsEmitted":       false,
						"phase":               "before_message_stream_start",
					}))
					recordCodexWebSocketFailure(func() string {
						if opts != nil {
							return opts.SessionID
						}
						return ""
					}(), err)
					recordCodexWebSocketSSEFallback(func() string {
						if opts != nil {
							return opts.SessionID
						}
						return ""
					}())
					goai.GetLogger().Debug("WebSocket fallback to SSE", "error", err)
					break
				}
			}
		}

		// SSE fallback (same as OpenAI Responses but with Codex URL)
		streamViaSSE(ctx, model, convCtx, opts, apiKey, ch, diagnostics)
	}()

	return ch
}

func resolveCodexURL(baseURL string) string {
	if baseURL == "" {
		return "https://api.openai.com/v1/codex/responses"
	}
	normalized := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(normalized, "/codex") {
		return normalized + "/responses"
	}
	return normalized + "/codex/responses"
}

func resolveCodexWSURL(baseURL string) string {
	httpURL := resolveCodexURL(baseURL)
	httpURL = strings.Replace(httpURL, "https://", "wss://", 1)
	httpURL = strings.Replace(httpURL, "http://", "ws://", 1)
	return httpURL
}

type codexAPIError struct {
	message string
	code    string
	payload any
}

func (e *codexAPIError) Error() string {
	if e == nil {
		return "codex API error"
	}
	if e.message != "" {
		return e.message
	}
	if e.code != "" {
		return "Codex error: " + e.code
	}
	return "Codex API error"
}

func (e *codexAPIError) Code() any { return e.code }

type codexProtocolError struct {
	message string
	payload any
	cause   error
}

func (e *codexProtocolError) Error() string {
	if e == nil || e.message == "" {
		return "Codex protocol error"
	}
	return e.message
}

func (e *codexProtocolError) Unwrap() error { return e.cause }

func isCodexNonTransportError(err error) bool {
	var apiErr *codexAPIError
	if errors.As(err, &apiErr) {
		return true
	}
	var protoErr *codexProtocolError
	return errors.As(err, &protoErr)
}

func isWebSocketConnectionLimitReachedError(err error) bool {
	var apiErr *codexAPIError
	return errors.As(err, &apiErr) && apiErr.code == "websocket_connection_limit_reached"
}

type codexEventError struct {
	Code    string
	Message string
}

func extractCodexEventError(code, message string, nested *codexEventError) codexEventError {
	out := codexEventError{Code: code, Message: message}
	if nested != nil {
		if out.Code == "" {
			out.Code = nested.Code
		}
		if out.Message == "" {
			out.Message = nested.Message
		}
	}
	return out
}

func extractCodexAccountID(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid token")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payloadBytes, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return "", fmt.Errorf("decode token payload: %w", err)
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return "", fmt.Errorf("parse token payload: %w", err)
	}
	auth, _ := payload["https://api.openai.com/auth"].(map[string]any)
	accountID, _ := auth["chatgpt_account_id"].(string)
	if accountID == "" {
		return "", fmt.Errorf("no chatgpt_account_id in token")
	}
	return accountID, nil
}

func buildCodexSSEHeaders(modelHeaders, optHeaders map[string]string, suppressHeaders []string, accountID, token, sessionID string) http.Header {
	h := http.Header{}
	goai.ApplyHeaders(h, modelHeaders)
	goai.ApplyHeaders(h, optHeaders)
	h.Set("Authorization", "Bearer "+token)
	h.Set("chatgpt-account-id", accountID)
	h.Set("originator", "pi")
	h.Set("User-Agent", fmt.Sprintf("go-ai (%s %s)", runtime.GOOS, runtime.GOARCH))
	h.Set("OpenAI-Beta", "responses=experimental")
	h.Set("Accept", "text/event-stream")
	h.Set("Content-Type", "application/json")
	if sessionID != "" {
		h.Set("session_id", sessionID)
		h.Set("x-client-request-id", sessionID)
	}
	goai.SuppressHeaders(h, suppressHeaders)
	return h
}

func buildCodexWebSocketHeaders(modelHeaders, optHeaders map[string]string, suppressHeaders []string, accountID, token, requestID string) http.Header {
	h := http.Header{}
	goai.ApplyHeaders(h, modelHeaders)
	goai.ApplyHeaders(h, optHeaders)
	h.Set("Authorization", "Bearer "+token)
	h.Set("chatgpt-account-id", accountID)
	h.Set("originator", "pi")
	h.Set("User-Agent", fmt.Sprintf("go-ai (%s %s)", runtime.GOOS, runtime.GOARCH))
	h.Set("OpenAI-Beta", "responses_websockets=2026-02-06")
	if requestID != "" {
		h.Set("session_id", requestID)
		h.Set("x-client-request-id", requestID)
	}
	goai.SuppressHeaders(h, suppressHeaders)
	return h
}

// --- WebSocket transport ---

type codexWebSocketContinuation struct {
	lastRequestBody   map[string]interface{}
	lastResponseID    string
	lastResponseItems []interface{}
}

const codexWebSocketSessionCacheTTL = 5 * time.Minute

type codexWebSocketSessionEntry struct {
	conn         *websocket.Conn
	continuation *codexWebSocketContinuation
	idleTimer    *time.Timer
	mu           sync.Mutex
}

// OpenAICodexWebSocketDebugStats exposes lightweight instrumentation for
// websocket-cached transport behavior, mirroring pi-ai's debug helpers.
type OpenAICodexWebSocketDebugStats struct {
	Requests                int    `json:"requests"`
	ConnectionsCreated      int    `json:"connectionsCreated"`
	ConnectionsReused       int    `json:"connectionsReused"`
	CachedContextRequests   int    `json:"cachedContextRequests"`
	StoreTrueRequests       int    `json:"storeTrueRequests"`
	FullContextRequests     int    `json:"fullContextRequests"`
	DeltaRequests           int    `json:"deltaRequests"`
	LastInputItems          int    `json:"lastInputItems"`
	LastDeltaInputItems     *int   `json:"lastDeltaInputItems,omitempty"`
	LastPreviousResponseID  string `json:"lastPreviousResponseId,omitempty"`
	WebSocketFailures       int    `json:"websocketFailures"`
	SSEFallbacks            int    `json:"sseFallbacks"`
	LastWebSocketError      string `json:"lastWebSocketError,omitempty"`
	WebSocketFallbackActive bool   `json:"websocketFallbackActive,omitempty"`
}

var (
	codexWebSocketSessionsMu          sync.Mutex
	codexWebSocketSessions            = map[string]*codexWebSocketSessionEntry{}
	codexWebSocketStats               = map[string]*OpenAICodexWebSocketDebugStats{}
	codexWebSocketSSEFallbackSessions = map[string]bool{}
)

// GetOpenAICodexWebSocketDebugStats returns a copy of cached WebSocket stats for sessionID.
func GetOpenAICodexWebSocketDebugStats(sessionID string) *OpenAICodexWebSocketDebugStats {
	codexWebSocketSessionsMu.Lock()
	defer codexWebSocketSessionsMu.Unlock()
	stats := codexWebSocketStats[sessionID]
	if stats == nil {
		return nil
	}
	copy := *stats
	return &copy
}

// ResetOpenAICodexWebSocketDebugStats clears stats for one session, or all sessions when empty.
func ResetOpenAICodexWebSocketDebugStats(sessionID string) {
	codexWebSocketSessionsMu.Lock()
	defer codexWebSocketSessionsMu.Unlock()
	if sessionID != "" {
		delete(codexWebSocketStats, sessionID)
		delete(codexWebSocketSSEFallbackSessions, sessionID)
		return
	}
	codexWebSocketStats = map[string]*OpenAICodexWebSocketDebugStats{}
	codexWebSocketSSEFallbackSessions = map[string]bool{}
}

// CloseOpenAICodexWebSocketSessions closes one cached Codex WebSocket session, or all when empty.
func CloseOpenAICodexWebSocketSessions(sessionID string) {
	codexWebSocketSessionsMu.Lock()
	defer codexWebSocketSessionsMu.Unlock()
	closeEntry := func(entry *codexWebSocketSessionEntry) {
		if entry != nil {
			if entry.idleTimer != nil {
				entry.idleTimer.Stop()
			}
			if entry.conn != nil {
				_ = entry.conn.Close(websocket.StatusNormalClosure, "debug_close")
			}
		}
	}
	if sessionID != "" {
		closeEntry(codexWebSocketSessions[sessionID])
		delete(codexWebSocketSessions, sessionID)
		return
	}
	for _, entry := range codexWebSocketSessions {
		closeEntry(entry)
	}
	codexWebSocketSessions = map[string]*codexWebSocketSessionEntry{}
}

func isCodexWebSocketSSEFallbackActive(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	codexWebSocketSessionsMu.Lock()
	defer codexWebSocketSessionsMu.Unlock()
	return codexWebSocketSSEFallbackSessions[sessionID]
}

func recordCodexWebSocketSSEFallback(sessionID string) {
	if sessionID == "" {
		return
	}
	codexWebSocketSessionsMu.Lock()
	defer codexWebSocketSessionsMu.Unlock()
	stats := codexWebSocketStats[sessionID]
	if stats == nil {
		stats = &OpenAICodexWebSocketDebugStats{}
		codexWebSocketStats[sessionID] = stats
	}
	stats.SSEFallbacks++
	stats.WebSocketFallbackActive = codexWebSocketSSEFallbackSessions[sessionID]
}

func recordCodexWebSocketFailure(sessionID string, err error) {
	if sessionID == "" {
		return
	}
	codexWebSocketSessionsMu.Lock()
	defer codexWebSocketSessionsMu.Unlock()
	codexWebSocketSSEFallbackSessions[sessionID] = true
	stats := codexWebSocketStats[sessionID]
	if stats == nil {
		stats = &OpenAICodexWebSocketDebugStats{}
		codexWebSocketStats[sessionID] = stats
	}
	stats.WebSocketFailures++
	stats.LastWebSocketError = goai.FormatThrownValue(err)
	stats.WebSocketFallbackActive = true
}

func recordCodexWebSocketStats(sessionID string, reused bool, useCachedContext bool, requestEnvelope map[string]interface{}) {
	if sessionID == "" {
		return
	}
	codexWebSocketSessionsMu.Lock()
	defer codexWebSocketSessionsMu.Unlock()
	stats := codexWebSocketStats[sessionID]
	if stats == nil {
		stats = &OpenAICodexWebSocketDebugStats{}
		codexWebSocketStats[sessionID] = stats
	}
	stats.Requests++
	if reused {
		stats.ConnectionsReused++
	} else {
		stats.ConnectionsCreated++
	}
	if useCachedContext {
		stats.CachedContextRequests++
	}
	if store, _ := requestEnvelope["store"].(bool); store {
		stats.StoreTrueRequests++
	}
	stats.LastInputItems = countInputItems(requestEnvelope)
	if previousID, _ := requestEnvelope["previous_response_id"].(string); previousID != "" {
		stats.DeltaRequests++
		stats.LastPreviousResponseID = previousID
		items := countInputItems(requestEnvelope)
		stats.LastDeltaInputItems = &items
	} else {
		stats.FullContextRequests++
		stats.LastPreviousResponseID = ""
		stats.LastDeltaInputItems = nil
	}
}

func dialCodexWebSocket(ctx context.Context, wsURL string, headers http.Header, retryCfg goai.RetryConfig, model *goai.Model) (*websocket.Conn, error) {
	var (
		conn  *websocket.Conn
		wsErr error
	)
	for attempt := 0; ; attempt++ {
		dialCtx := ctx
		cancel := func() {}
		if retryCfg.ConnectTimeout > 0 {
			dialCtx, cancel = context.WithTimeout(ctx, retryCfg.ConnectTimeout)
		}
		conn, _, wsErr = websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
		cancel()
		if wsErr == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt >= retryCfg.MaxRetries {
			return nil, fmt.Errorf("WebSocket dial: %w", wsErr)
		}
		delay := retryutil.ComputeBackoff(attempt, retryCfg.InitialDelay, retryCfg.MaxDelay, retryCfg.BackoffMultiplier, retryCfg.JitterFraction)
		goai.GetLogger().Warn("websocket dial retry", "provider", model.Provider, "model", model.ID, "attempt", attempt+1, "maxRetries", retryCfg.MaxRetries, "delay", delay, "error", wsErr)
		if retryCfg.OnRetry != nil {
			retryCfg.OnRetry(attempt, delay, 0)
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}
}

func acquireCodexWebSocketSession(ctx context.Context, wsURL string, headers http.Header, sessionID string, retryCfg goai.RetryConfig, model *goai.Model) (*codexWebSocketSessionEntry, bool, error) {
	codexWebSocketSessionsMu.Lock()
	if entry := codexWebSocketSessions[sessionID]; entry != nil {
		if entry.idleTimer != nil {
			entry.idleTimer.Stop()
			entry.idleTimer = nil
		}
		codexWebSocketSessionsMu.Unlock()
		return entry, true, nil
	}
	codexWebSocketSessionsMu.Unlock()

	conn, err := dialCodexWebSocket(ctx, wsURL, headers, retryCfg, model)
	if err != nil {
		return nil, false, err
	}
	entry := &codexWebSocketSessionEntry{conn: conn}

	codexWebSocketSessionsMu.Lock()
	if existing := codexWebSocketSessions[sessionID]; existing != nil {
		if existing.idleTimer != nil {
			existing.idleTimer.Stop()
			existing.idleTimer = nil
		}
		codexWebSocketSessionsMu.Unlock()
		conn.CloseNow()
		return existing, true, nil
	}
	codexWebSocketSessions[sessionID] = entry
	codexWebSocketSessionsMu.Unlock()
	return entry, false, nil
}

func removeCodexWebSocketSession(sessionID string, entry *codexWebSocketSessionEntry) {
	if sessionID == "" {
		return
	}
	var conn *websocket.Conn
	codexWebSocketSessionsMu.Lock()
	if codexWebSocketSessions[sessionID] == entry {
		if entry != nil && entry.idleTimer != nil {
			entry.idleTimer.Stop()
			entry.idleTimer = nil
		}
		if entry != nil {
			conn = entry.conn
		}
		delete(codexWebSocketSessions, sessionID)
	}
	codexWebSocketSessionsMu.Unlock()
	if conn != nil {
		conn.CloseNow()
	}
}

func scheduleCodexWebSocketIdleClose(sessionID string, entry *codexWebSocketSessionEntry) {
	if sessionID == "" || entry == nil {
		return
	}
	codexWebSocketSessionsMu.Lock()
	defer codexWebSocketSessionsMu.Unlock()
	if codexWebSocketSessions[sessionID] != entry {
		return
	}
	if entry.idleTimer != nil {
		entry.idleTimer.Stop()
	}
	entry.idleTimer = time.AfterFunc(codexWebSocketSessionCacheTTL, func() {
		codexWebSocketSessionsMu.Lock()
		defer codexWebSocketSessionsMu.Unlock()
		if codexWebSocketSessions[sessionID] == entry {
			delete(codexWebSocketSessions, sessionID)
			_ = entry.conn.Close(websocket.StatusNormalClosure, "idle_timeout")
		}
	})
}

func requestBodyWithoutCodexInput(body map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range body {
		if k == "input" || k == "previous_response_id" || k == "type" {
			continue
		}
		out[k] = v
	}
	return out
}

func requestBodiesMatchExceptInput(a, b map[string]interface{}) bool {
	return reflect.DeepEqual(requestBodyWithoutCodexInput(a), requestBodyWithoutCodexInput(b))
}

func inputItems(body map[string]interface{}) []interface{} {
	items, _ := body["input"].([]interface{})
	return items
}

func getCachedWebSocketInputDelta(body map[string]interface{}, continuation *codexWebSocketContinuation) []interface{} {
	if continuation == nil || !requestBodiesMatchExceptInput(body, continuation.lastRequestBody) {
		return nil
	}
	current := inputItems(body)
	baseline := append([]interface{}{}, inputItems(continuation.lastRequestBody)...)
	baseline = append(baseline, continuation.lastResponseItems...)
	if len(current) < len(baseline) {
		return nil
	}
	if !reflect.DeepEqual(current[:len(baseline)], baseline) {
		return nil
	}
	return current[len(baseline):]
}

func buildCachedWebSocketRequestBody(entry *codexWebSocketSessionEntry, body map[string]interface{}) map[string]interface{} {
	if entry == nil || entry.continuation == nil {
		return body
	}
	delta := getCachedWebSocketInputDelta(body, entry.continuation)
	if delta == nil || entry.continuation.lastResponseID == "" {
		entry.continuation = nil
		return body
	}
	out := map[string]interface{}{}
	for k, v := range body {
		out[k] = v
	}
	out["previous_response_id"] = entry.continuation.lastResponseID
	out["input"] = delta
	return out
}

func countInputItems(body map[string]interface{}) int { return len(inputItems(body)) }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeJSONValue(v interface{}) interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}

func streamViaWebSocket(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions, apiKey string, ch chan<- goai.Event) error {
	wsURL := resolveCodexWSURL(model.BaseURL)
	goai.GetLogger().Debug("WebSocket connect", "url", wsURL, "provider", model.Provider)

	accountID, err := extractCodexAccountID(apiKey)
	if err != nil {
		return fmt.Errorf("extract codex account id: %w", err)
	}
	requestID := ""
	if opts != nil {
		requestID = opts.SessionID
	}
	headers := buildCodexWebSocketHeaders(model.Headers, func() map[string]string {
		if opts != nil {
			return opts.Headers
		}
		return nil
	}(), func() []string {
		if opts != nil {
			return opts.SuppressHeaders
		}
		return nil
	}(), accountID, apiKey, requestID)

	retryCfg := goai.RetryConfigFromOptions(opts)
	if opts != nil && opts.WebSocketConnectTimeoutMs != nil {
		retryCfg.ConnectTimeout = time.Duration(*opts.WebSocketConnectTimeoutMs) * time.Millisecond
	}
	useCachedContext := opts != nil && (opts.Transport == goai.TransportWebSocketCached || opts.Transport == goai.TransportAuto || opts.Transport == "") && opts.SessionID != ""
	var (
		conn   *websocket.Conn
		entry  *codexWebSocketSessionEntry
		reused bool
	)
	if useCachedContext {
		var err error
		entry, reused, err = acquireCodexWebSocketSession(ctx, wsURL, headers, opts.SessionID, retryCfg, model)
		if err != nil {
			return err
		}
		entry.mu.Lock()
		defer entry.mu.Unlock()
		conn = entry.conn
	} else {
		var err error
		conn, err = dialCodexWebSocket(ctx, wsURL, headers, retryCfg, model)
		if err != nil {
			return err
		}
		defer conn.CloseNow()
	}

	// Build request body
	body := buildCodexRequest(model, convCtx, opts)
	payload, err := goai.InvokeOnPayload(opts, body, model)
	if err != nil {
		return err
	}
	bodyJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Send request over WebSocket
	var fullEnvelope map[string]interface{}
	if err := json.Unmarshal(bodyJSON, &fullEnvelope); err != nil {
		return fmt.Errorf("decode websocket payload: %w", err)
	}
	requestEnvelope := fullEnvelope
	if useCachedContext && entry != nil {
		requestEnvelope = buildCachedWebSocketRequestBody(entry, fullEnvelope)
	}
	if opts != nil && opts.SessionID != "" {
		recordCodexWebSocketStats(opts.SessionID, reused, useCachedContext, requestEnvelope)
	}
	sendEnvelope := map[string]interface{}{}
	for k, v := range requestEnvelope {
		sendEnvelope[k] = v
	}
	sendEnvelope["type"] = "response.create"
	framedJSON, err := json.Marshal(sendEnvelope)
	if err != nil {
		return fmt.Errorf("encode websocket payload: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, framedJSON); err != nil {
		if useCachedContext && entry != nil && opts != nil {
			removeCodexWebSocketSession(opts.SessionID, entry)
		}
		return fmt.Errorf("WebSocket write: %w", err)
	}

	// Process responses
	partial := &goai.Message{
		Role:     goai.RoleAssistant,
		Api:      model.Api,
		Provider: model.Provider,
		Model:    model.ID,
		Usage:    &goai.Usage{},
	}

	started := false

	type activeItem struct {
		itemType    string
		contentIdx  int
		partialJSON string
	}
	var current *activeItem

readLoop:
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if useCachedContext && entry != nil && opts != nil {
				removeCodexWebSocketSession(opts.SessionID, entry)
				entry.continuation = nil
			}
			if ctx.Err() != nil {
				goai.GetLogger().Debug("request aborted", "provider", model.Provider, "model", model.ID)
				if started {
					ch <- &goai.ErrorEvent{Reason: goai.StopReasonAborted, Err: ctx.Err()}
					return nil
				}
				return ctx.Err()
			}
			// Normal close
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				break
			}
			if !started {
				return fmt.Errorf("WebSocket read: %w", err)
			}
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return nil
		}

		var raw struct {
			Type     string           `json:"type"`
			Item     json.RawMessage  `json:"item,omitempty"`
			Response json.RawMessage  `json:"response,omitempty"`
			Delta    string           `json:"delta,omitempty"`
			Code     string           `json:"code,omitempty"`
			Message  string           `json:"message,omitempty"`
			Error    *codexEventError `json:"error,omitempty"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			if !started {
				return &codexProtocolError{message: "Invalid Codex WebSocket JSON: " + goai.FormatThrownValue(err), payload: string(data), cause: err}
			}
			continue
		}
		if raw.Type != "error" && raw.Type != "response.failed" && !started {
			started = true
			ch <- &goai.StartEvent{Partial: partial}
		}

		// Same event processing as OpenAI Responses
		switch raw.Type {
		case "response.created":
			var resp struct {
				ID string `json:"id"`
			}
			json.Unmarshal(raw.Response, &resp)
			partial.ResponseID = resp.ID

		case "response.output_item.added":
			var item struct {
				Type   string `json:"type"`
				ID     string `json:"id"`
				CallID string `json:"call_id"`
				Name   string `json:"name"`
			}
			json.Unmarshal(raw.Item, &item)

			switch item.Type {
			case "reasoning":
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking"})
				idx := len(partial.Content) - 1
				current = &activeItem{itemType: "reasoning", contentIdx: idx}
				ch <- &goai.ThinkingStartEvent{ContentIndex: idx, Partial: partial}
			case "message":
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "text"})
				idx := len(partial.Content) - 1
				current = &activeItem{itemType: "message", contentIdx: idx}
				ch <- &goai.TextStartEvent{ContentIndex: idx, Partial: partial}
			case "function_call":
				partial.Content = append(partial.Content, goai.ContentBlock{
					Type: "toolCall",
					ID:   fmt.Sprintf("%s|%s", item.CallID, item.ID),
					Name: item.Name,
				})
				idx := len(partial.Content) - 1
				current = &activeItem{itemType: "function_call", contentIdx: idx}
				ch <- &goai.ToolCallStartEvent{ContentIndex: idx, Partial: partial}
			}

		case "response.reasoning_summary_text.delta":
			if current != nil && current.itemType == "reasoning" {
				partial.Content[current.contentIdx].Thinking += raw.Delta
				ch <- &goai.ThinkingDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial}
			}

		case "response.output_text.delta":
			if current != nil && current.itemType == "message" {
				partial.Content[current.contentIdx].Text += raw.Delta
				ch <- &goai.TextDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial}
			}

		case "response.function_call_arguments.delta":
			if current != nil && current.itemType == "function_call" {
				current.partialJSON += raw.Delta
				args, _ := jsonparse.ParsePartialJSON(current.partialJSON)
				if args != nil {
					partial.Content[current.contentIdx].Arguments = args
				}
				ch <- &goai.ToolCallDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial}
			}

		case "response.function_call_arguments.done":
			if current != nil && current.itemType == "function_call" {
				var done struct {
					Arguments string `json:"arguments"`
				}
				if json.Unmarshal(data, &done) == nil && done.Arguments != "" {
					previous := current.partialJSON
					current.partialJSON = done.Arguments
					args, _ := jsonparse.ParsePartialJSON(current.partialJSON)
					if args != nil {
						partial.Content[current.contentIdx].Arguments = args
					}
					if strings.HasPrefix(done.Arguments, previous) {
						delta := strings.TrimPrefix(done.Arguments, previous)
						if delta != "" {
							ch <- &goai.ToolCallDeltaEvent{ContentIndex: current.contentIdx, Delta: delta, Partial: partial}
						}
					}
				}
			}

		case "response.output_item.done":
			if current == nil {
				continue
			}
			idx := current.contentIdx
			switch current.itemType {
			case "reasoning":
				partial.Content[idx].ThinkingSignature = string(raw.Item)
				ch <- &goai.ThinkingEndEvent{ContentIndex: idx, Content: partial.Content[idx].Thinking, Partial: partial}
			case "message":
				ch <- &goai.TextEndEvent{ContentIndex: idx, Content: partial.Content[idx].Text, Partial: partial}
			case "function_call":
				args, _ := jsonparse.ParsePartialJSON(current.partialJSON)
				if args == nil {
					var item struct {
						Arguments string `json:"arguments"`
					}
					if json.Unmarshal(raw.Item, &item) == nil && item.Arguments != "" {
						args, _ = jsonparse.ParsePartialJSON(item.Arguments)
					}
				}
				if args == nil {
					args = map[string]interface{}{}
				}
				partial.Content[idx].Arguments = args
				ch <- &goai.ToolCallEndEvent{
					ContentIndex: idx,
					ToolCall:     goai.ToolCall{Type: "toolCall", ID: partial.Content[idx].ID, Name: partial.Content[idx].Name, Arguments: args},
					Partial:      partial,
				}
			}
			current = nil

		case "response.completed", "response.incomplete", "response.done":
			var resp struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Usage  *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
					TotalTokens  int `json:"total_tokens"`
					InputDetails *struct {
						CachedTokens int `json:"cached_tokens"`
					} `json:"input_tokens_details"`
				} `json:"usage"`
			}
			json.Unmarshal(raw.Response, &resp)
			if resp.ID != "" {
				partial.ResponseID = resp.ID
			}
			if resp.Usage != nil {
				cached := 0
				if resp.Usage.InputDetails != nil {
					cached = resp.Usage.InputDetails.CachedTokens
				}
				partial.Usage = &goai.Usage{
					Input: resp.Usage.InputTokens - cached, Output: resp.Usage.OutputTokens,
					CacheRead: cached, TotalTokens: resp.Usage.TotalTokens,
				}
				partial.Usage.Cost = goai.CalculateCost(model, partial.Usage)
			}
			partial.StopReason = mapCodexStatus(resp.Status)
			for _, c := range partial.Content {
				if c.Type == "toolCall" && partial.StopReason == goai.StopReasonStop {
					partial.StopReason = goai.StopReasonToolUse
					break
				}
			}
			break readLoop

		case "error":
			if useCachedContext && entry != nil {
				entry.continuation = nil
			}
			eventErr := extractCodexEventError(raw.Code, raw.Message, raw.Error)
			err := &codexAPIError{message: fmt.Sprintf("Codex error: %s", firstNonEmpty(eventErr.Message, eventErr.Code, string(data))), code: eventErr.Code, payload: string(data)}
			if !started {
				if useCachedContext && entry != nil && opts != nil {
					removeCodexWebSocketSession(opts.SessionID, entry)
				}
				return err
			}
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return nil

		case "response.failed":
			if useCachedContext && entry != nil {
				entry.continuation = nil
			}
			err := &codexAPIError{message: "Codex response failed", payload: string(data)}
			if !started {
				return err
			}
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return nil
		}
	}

	if useCachedContext && entry != nil && partial.ResponseID != "" {
		responseItems := buildCodexInput(model, &goai.Context{Messages: []goai.Message{*partial}})
		filtered := make([]interface{}, 0, len(responseItems))
		for _, item := range responseItems {
			if m, ok := item.(map[string]interface{}); ok && m["type"] == "function_call_output" {
				continue
			}
			filtered = append(filtered, item)
		}
		normalized, _ := normalizeJSONValue(filtered).([]interface{})
		entry.continuation = &codexWebSocketContinuation{lastRequestBody: fullEnvelope, lastResponseID: partial.ResponseID, lastResponseItems: normalized}
		scheduleCodexWebSocketIdleClose(opts.SessionID, entry)
	} else if useCachedContext && entry != nil {
		entry.continuation = nil
		scheduleCodexWebSocketIdleClose(opts.SessionID, entry)
	} else {
		conn.Close(websocket.StatusNormalClosure, "done")
	}

	partial.Timestamp = time.Now().UnixMilli()
	if partial.StopReason == "" {
		partial.StopReason = goai.StopReasonStop
	}
	ch <- &goai.DoneEvent{Reason: partial.StopReason, Message: partial}
	return nil
}

// --- SSE fallback ---

func streamViaSSE(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions, apiKey string, ch chan<- goai.Event, diagnostics []goai.AssistantMessageDiagnostic) {
	body := buildCodexRequest(model, convCtx, opts)
	payload, err := goai.InvokeOnPayload(opts, body, model)
	if err != nil {
		ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
		return
	}
	bodyJSON, _ := json.Marshal(payload)

	url := resolveCodexURL(model.BaseURL)
	goai.GetLogger().Debug("HTTP request", "url", url, "provider", model.Provider, "model", model.ID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
		return
	}
	accountID, err := extractCodexAccountID(apiKey)
	if err != nil {
		ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("extract codex account id: %w", err)}
		return
	}
	sessionID := ""
	if opts != nil {
		sessionID = opts.SessionID
	}
	req.Header = buildCodexSSEHeaders(model.Headers, func() map[string]string {
		if opts != nil {
			return opts.Headers
		}
		return nil
	}(), func() []string {
		if opts != nil {
			return opts.SuppressHeaders
		}
		return nil
	}(), accountID, apiKey, sessionID)

	retryCfg := goai.RetryConfigFromOptions(opts)
	client := retryCfg.NewHTTPClient()
	resp, err := goai.DoWithRetry(ctx, client, req, retryCfg)
	if err != nil {
		if ctx.Err() != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonAborted, Err: ctx.Err()}
		} else {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
		}
		return
	}
	defer resp.Body.Close()

	goai.InvokeOnResponse(opts, resp, model)

	if resp.StatusCode != 200 {
		goai.GetLogger().Warn("HTTP error response", "status", resp.StatusCode, "provider", model.Provider, "model", model.ID)
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))}
		return
	}

	// Reuse same SSE processing as OpenAI Responses
	processCodexSSE(resp.Body, model, ch, diagnostics)
}

// processCodexSSE mirrors the OpenAI Responses-style event processing used by pi-ai.
func processCodexSSE(body io.Reader, model *goai.Model, ch chan<- goai.Event, diagnostics []goai.AssistantMessageDiagnostic) {
	partial := &goai.Message{
		Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, Usage: &goai.Usage{}, Diagnostics: diagnostics,
	}
	ch <- &goai.StartEvent{Partial: partial}

	type activeItem struct {
		itemType    string
		contentIdx  int
		partialJSON string
	}
	var current *activeItem

	events := sse.Parse(body)
	for evt := range events {
		if evt.Event == sse.EventError {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("SSE stream error: %s", evt.Data)}
			return
		}
		if evt.Data == "[DONE]" {
			break
		}
		var raw struct {
			Type     string           `json:"type"`
			Item     json.RawMessage  `json:"item,omitempty"`
			Response json.RawMessage  `json:"response,omitempty"`
			Delta    string           `json:"delta,omitempty"`
			Code     string           `json:"code,omitempty"`
			Message  string           `json:"message,omitempty"`
			Error    *codexEventError `json:"error,omitempty"`
		}
		if json.Unmarshal([]byte(evt.Data), &raw) != nil {
			continue
		}

		switch raw.Type {
		case "response.created":
			var resp struct {
				ID string `json:"id"`
			}
			json.Unmarshal(raw.Response, &resp)
			partial.ResponseID = resp.ID
		case "response.output_item.added":
			var item struct {
				Type   string `json:"type"`
				ID     string `json:"id"`
				CallID string `json:"call_id"`
				Name   string `json:"name"`
			}
			json.Unmarshal(raw.Item, &item)
			switch item.Type {
			case "reasoning":
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking"})
				current = &activeItem{itemType: "reasoning", contentIdx: len(partial.Content) - 1}
				ch <- &goai.ThinkingStartEvent{ContentIndex: current.contentIdx, Partial: partial}
			case "message":
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "text"})
				current = &activeItem{itemType: "message", contentIdx: len(partial.Content) - 1}
				ch <- &goai.TextStartEvent{ContentIndex: current.contentIdx, Partial: partial}
			case "function_call":
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "toolCall", ID: fmt.Sprintf("%s|%s", item.CallID, item.ID), Name: item.Name})
				current = &activeItem{itemType: "function_call", contentIdx: len(partial.Content) - 1}
				ch <- &goai.ToolCallStartEvent{ContentIndex: current.contentIdx, Partial: partial}
			}
		case "response.reasoning_summary_text.delta":
			if current != nil && current.itemType == "reasoning" {
				partial.Content[current.contentIdx].Thinking += raw.Delta
				ch <- &goai.ThinkingDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial}
			}
		case "response.output_text.delta":
			if current != nil && current.itemType == "message" {
				partial.Content[current.contentIdx].Text += raw.Delta
				ch <- &goai.TextDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial}
			}
		case "response.function_call_arguments.delta":
			if current != nil && current.itemType == "function_call" {
				current.partialJSON += raw.Delta
				args, _ := jsonparse.ParsePartialJSON(current.partialJSON)
				if args != nil {
					partial.Content[current.contentIdx].Arguments = args
				}
				ch <- &goai.ToolCallDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial}
			}
		case "response.function_call_arguments.done":
			if current != nil && current.itemType == "function_call" {
				var done struct {
					Arguments string `json:"arguments"`
				}
				if json.Unmarshal([]byte(evt.Data), &done) == nil && done.Arguments != "" {
					previous := current.partialJSON
					current.partialJSON = done.Arguments
					args, _ := jsonparse.ParsePartialJSON(current.partialJSON)
					if args != nil {
						partial.Content[current.contentIdx].Arguments = args
					}
					if strings.HasPrefix(done.Arguments, previous) {
						delta := strings.TrimPrefix(done.Arguments, previous)
						if delta != "" {
							ch <- &goai.ToolCallDeltaEvent{ContentIndex: current.contentIdx, Delta: delta, Partial: partial}
						}
					}
				}
			}
		case "response.output_item.done":
			if current == nil {
				continue
			}
			idx := current.contentIdx
			switch current.itemType {
			case "reasoning":
				partial.Content[idx].ThinkingSignature = string(raw.Item)
				ch <- &goai.ThinkingEndEvent{ContentIndex: idx, Content: partial.Content[idx].Thinking, Partial: partial}
			case "message":
				ch <- &goai.TextEndEvent{ContentIndex: idx, Content: partial.Content[idx].Text, Partial: partial}
			case "function_call":
				args, _ := jsonparse.ParsePartialJSON(current.partialJSON)
				if args == nil {
					var item struct {
						Arguments string `json:"arguments"`
					}
					if json.Unmarshal(raw.Item, &item) == nil && item.Arguments != "" {
						args, _ = jsonparse.ParsePartialJSON(item.Arguments)
					}
				}
				if args == nil {
					args = map[string]interface{}{}
				}
				partial.Content[idx].Arguments = args
				ch <- &goai.ToolCallEndEvent{ContentIndex: idx, ToolCall: goai.ToolCall{Type: "toolCall", ID: partial.Content[idx].ID, Name: partial.Content[idx].Name, Arguments: args}, Partial: partial}
			}
			current = nil
		case "error":
			eventErr := extractCodexEventError(raw.Code, raw.Message, raw.Error)
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("API error %s: %s", eventErr.Code, eventErr.Message)}
			return
		case "response.failed":
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("response failed")}
			return
		case "response.completed", "response.incomplete", "response.done":
			var resp struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Usage  *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
					TotalTokens  int `json:"total_tokens"`
					InputDetails *struct {
						CachedTokens int `json:"cached_tokens"`
					} `json:"input_tokens_details"`
				} `json:"usage"`
			}
			json.Unmarshal(raw.Response, &resp)
			partial.ResponseID = resp.ID
			if resp.Usage != nil {
				partial.Usage = &goai.Usage{Input: resp.Usage.InputTokens, Output: resp.Usage.OutputTokens, TotalTokens: resp.Usage.TotalTokens}
				if resp.Usage.InputDetails != nil {
					partial.Usage.CacheRead = resp.Usage.InputDetails.CachedTokens
				}
				partial.Usage.Cost = goai.CalculateCost(model, partial.Usage)
			}
			partial.StopReason = mapCodexStatus(resp.Status)
		}
	}

	partial.Timestamp = time.Now().UnixMilli()
	if partial.StopReason == "" {
		partial.StopReason = goai.StopReasonStop
	}
	ch <- &goai.DoneEvent{Reason: partial.StopReason, Message: partial}
}

// --- Request building ---

type codexRequest struct {
	Model              string          `json:"model"`
	Store              bool            `json:"store"`
	Stream             bool            `json:"stream"`
	Instructions       string          `json:"instructions,omitempty"`
	Input              json.RawMessage `json:"input"`
	Tools              []codexTool     `json:"tools,omitempty"`
	MaxOutputTokens    *int            `json:"max_output_tokens,omitempty"`
	Temperature        *float64        `json:"temperature,omitempty"`
	Reasoning          interface{}     `json:"reasoning,omitempty"`
	Text               interface{}     `json:"text,omitempty"`
	Include            []string        `json:"include,omitempty"`
	PromptCacheKey     string          `json:"prompt_cache_key,omitempty"`
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
	ToolChoice         string          `json:"tool_choice,omitempty"`
	ParallelToolCalls  *bool           `json:"parallel_tool_calls,omitempty"`
	ServiceTier        string          `json:"service_tier,omitempty"`
}

type codexTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

func buildCodexRequest(model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) codexRequest {
	parallelToolCalls := true
	req := codexRequest{
		Model:             model.ID,
		Store:             false,
		Stream:            true,
		Instructions:      convCtx.SystemPrompt,
		Text:              map[string]interface{}{"verbosity": "low"},
		Include:           []string{"reasoning.encrypted_content"},
		ToolChoice:        "auto",
		ParallelToolCalls: &parallelToolCalls,
	}

	if opts != nil {
		req.Temperature = opts.Temperature
		req.MaxOutputTokens = opts.MaxTokens
		if opts.TextVerbosity != "" {
			req.Text = map[string]interface{}{"verbosity": opts.TextVerbosity}
		}
		if opts.SessionID != "" {
			req.PromptCacheKey = goai.ClampOpenAIPromptCacheKey(opts.SessionID)
		}
		if opts.Reasoning != nil {
			if effort, ok := goai.MapThinkingLevel(model, goai.ModelThinkingLevel(*opts.Reasoning)); ok {
				summary := opts.ReasoningSummary
				if summary == "" {
					summary = "auto"
				}
				req.Reasoning = map[string]interface{}{
					"effort":  effort,
					"summary": summary,
				}
			}
		}
	}
	if opts != nil && opts.ServiceTier != "" {
		req.ServiceTier = opts.ServiceTier
	}

	// Build input in Responses-compatible format, but with system prompt carried in top-level instructions.
	input := buildCodexInput(model, convCtx)
	inputJSON, _ := json.Marshal(input)
	req.Input = inputJSON

	for _, t := range convCtx.Tools {
		req.Tools = append(req.Tools, codexTool{
			Type: "function", Name: t.Name, Description: t.Description, Parameters: t.Parameters,
		})
	}

	return req
}

func buildCodexInput(model *goai.Model, convCtx *goai.Context) []interface{} {
	var input []interface{}
	transformed := goai.TransformMessages(convCtx.Messages, model)
	for _, msg := range transformed {
		switch msg.Role {
		case goai.RoleUser:
			var content []map[string]interface{}
			for _, b := range msg.Content {
				if b.Type == "text" {
					content = append(content, map[string]interface{}{"type": "input_text", "text": goai.SanitizeSurrogates(b.Text)})
				}
			}
			if len(content) > 0 {
				input = append(input, map[string]interface{}{"role": "user", "content": content})
			}
		case goai.RoleAssistant:
			input = append(input, buildCodexAssistantItems(msg, model)...)
		case goai.RoleToolResult:
			text := ""
			for _, b := range msg.Content {
				if b.Type == "text" {
					text += b.Text
				}
			}
			callID := msg.ToolCallID
			if idx := strings.Index(callID, "|"); idx >= 0 {
				callID = callID[:idx]
			}
			input = append(input, map[string]interface{}{
				"type": "function_call_output", "call_id": callID, "output": goai.SanitizeSurrogates(text),
			})
		}
	}
	return input
}

func buildCodexAssistantItems(msg goai.Message, model *goai.Model) []interface{} {
	isDifferentModel := msg.Model != "" && msg.Model != model.ID && msg.Provider == model.Provider && msg.Api == model.Api
	var items []interface{}
	for _, block := range msg.Content {
		switch block.Type {
		case "thinking":
			if block.ThinkingSignature != "" {
				var item interface{}
				if json.Unmarshal([]byte(block.ThinkingSignature), &item) == nil {
					items = append(items, item)
				}
			}
		case "text":
			item := map[string]interface{}{
				"type":    "message",
				"role":    "assistant",
				"content": []map[string]interface{}{{"type": "output_text", "text": goai.SanitizeSurrogates(block.Text)}},
				"status":  "completed",
			}
			if block.TextSignature != "" {
				var sig struct {
					ID    string `json:"id"`
					Phase string `json:"phase"`
				}
				if json.Unmarshal([]byte(block.TextSignature), &sig) == nil && sig.ID != "" {
					msgID := sig.ID
					if len(msgID) > 64 {
						msgID = fmt.Sprintf("msg_%x", crc32.ChecksumIEEE([]byte(msgID)))
					}
					item["id"] = msgID
					if sig.Phase != "" {
						item["phase"] = sig.Phase
					}
				}
			}
			items = append(items, item)
		case "toolCall":
			callID := block.ID
			itemID := ""
			if idx := strings.Index(callID, "|"); idx >= 0 {
				itemID = callID[idx+1:]
				callID = callID[:idx]
			}
			if isDifferentModel && strings.HasPrefix(itemID, "fc_") {
				itemID = ""
			}
			item := map[string]interface{}{
				"type":      "function_call",
				"call_id":   callID,
				"name":      block.Name,
				"arguments": func() string { j, _ := json.Marshal(block.Arguments); return string(j) }(),
			}
			if itemID != "" {
				item["id"] = itemID
			}
			items = append(items, item)
		}
	}
	return items
}

func mapCodexStatus(status string) goai.StopReason {
	switch status {
	case "completed":
		return goai.StopReasonStop
	case "incomplete":
		return goai.StopReasonLength
	case "failed", "cancelled":
		return goai.StopReasonError
	default:
		return goai.StopReasonStop
	}
}
