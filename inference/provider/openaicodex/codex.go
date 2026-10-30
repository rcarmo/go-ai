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
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/responsestream"
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
	send := goai.NewAssistantEventSender(ch)

	go func() {
		defer close(ch)
		goai.GetLogger().Debug("stream start", "api", "openai-codex-responses", "provider", model.Provider, "model", model.ID)

		apiKey := goai.ResolveAPIKey(model, opts)
		if apiKey == "" {
			//lint:ignore ST1005 upstream pi-ai exact error string starts with a capital letter.
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("No API key for provider: %s", model.Provider)})
			return
		}

		prepared, prepareErr := prepareCodexRequest(ctx, model, convCtx, opts)
		if prepareErr != nil {
			reason := goai.StopReasonError
			if ctx.Err() != nil {
				reason = goai.StopReasonAborted
			}
			send(&goai.ErrorEvent{Reason: reason, Err: prepareErr})
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
					err := streamViaWebSocket(ctx, model, convCtx, opts, apiKey, send, prepared)
					if err == nil {
						return
					}
					if ctx.Err() != nil {
						event := codexStreamErrorEvent(err)
						event.Reason = goai.StopReasonAborted
						event.Err = ctx.Err()
						if event.Error != nil {
							event.Error.StopReason = goai.StopReasonAborted
							event.Error.ErrorMessage = ctx.Err().Error()
						}
						send(event)
						return
					}
					connectionLimit := isWebSocketConnectionLimitReachedError(err)
					if connectionLimit && !retriedConnectionLimit {
						retriedConnectionLimit = true
						continue
					}
					if isCodexNonTransportError(err) && !connectionLimit {
						send(codexStreamErrorEvent(err))
						return
					}
					diagnostics = append(diagnostics, goai.CreateAssistantMessageDiagnostic("provider_transport_failure", err, map[string]any{
						"configuredTransport": string(transport),
						"fallbackTransport":   "sse",
						"eventsEmitted":       false,
						"phase":               "before_message_stream_start",
						"requestBytes":        len(prepared.bodyJSON),
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
		streamViaSSE(ctx, model, convCtx, opts, apiKey, send, diagnostics, prepared)
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
	partial *goai.Message
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
	var payloadErr *codexPayloadError
	var observerErr *codexObserverError
	return errors.As(err, &protoErr) || errors.As(err, &payloadErr) || errors.As(err, &observerErr)
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

func buildCodexSSEHeaders(modelHeaders, optHeaders map[string]string, suppressHeaders []string, accountID, token, sessionID string, identities ...*goai.ClientIdentity) http.Header {
	h := http.Header{}
	h.Set("originator", "pi")
	h.Set("User-Agent", goai.FormatUserAgent(goai.MergeClientIdentity(identities...)))
	goai.ApplyHeaders(h, modelHeaders)
	goai.ApplyHeaders(h, optHeaders)
	h.Set("Authorization", "Bearer "+token)
	h.Set("chatgpt-account-id", accountID)
	h.Set("OpenAI-Beta", "responses=experimental")
	h.Set("Accept", "text/event-stream")
	h.Set("Content-Type", "application/json")
	if sessionID != "" {
		h.Set("session-id", sessionID)
		h.Set("x-client-request-id", sessionID)
	}
	goai.SuppressHeaders(h, suppressHeaders)
	return h
}

func buildCodexWebSocketHeaders(modelHeaders, optHeaders map[string]string, suppressHeaders []string, accountID, token, requestID string, identities ...*goai.ClientIdentity) http.Header {
	h := http.Header{}
	h.Set("originator", "pi")
	h.Set("User-Agent", goai.FormatUserAgent(goai.MergeClientIdentity(identities...)))
	goai.ApplyHeaders(h, modelHeaders)
	goai.ApplyHeaders(h, optHeaders)
	h.Set("Authorization", "Bearer "+token)
	h.Set("chatgpt-account-id", accountID)
	h.Set("OpenAI-Beta", "responses_websockets=2026-02-06")
	if requestID != "" {
		h.Set("session-id", requestID)
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

const (
	codexWebSocketSessionCacheTTL    = 5 * time.Minute
	codexWebSocketSessionMaxAge      = 55 * time.Minute
	codexRequestCompressionZstdLevel = 3
)

type codexWebSocketSessionKey struct{ sessionID, accountID, wsURL string }

type codexWebSocketSessionEntry struct {
	busy         bool // guarded by codexWebSocketSessionsMu
	key          codexWebSocketSessionKey
	conn         *websocket.Conn
	continuation *codexWebSocketContinuation
	idleTimer    *time.Timer
	createdAt    time.Time
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
	codexWebSocketSessions            = map[codexWebSocketSessionKey]*codexWebSocketSessionEntry{}
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

// CloseOpenAICodexWebSocketSessions closes every account/endpoint partition of
// a logical Codex session, or every cached session when sessionID is empty.
func CloseOpenAICodexWebSocketSessions(sessionID string) {
	var entries []*codexWebSocketSessionEntry
	codexWebSocketSessionsMu.Lock()
	for key, entry := range codexWebSocketSessions {
		if sessionID == "" || key.sessionID == sessionID {
			delete(codexWebSocketSessions, key)
			if entry == nil {
				continue
			}
			if entry.idleTimer != nil {
				entry.idleTimer.Stop()
				entry.idleTimer = nil
			}
			entries = append(entries, entry)
		}
	}
	codexWebSocketSessionsMu.Unlock()
	for _, entry := range entries {
		if entry.conn != nil {
			_ = entry.conn.Close(websocket.StatusNormalClosure, "debug_close")
		}
	}
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

// Codex events can contain complete output snapshots, exceeding websocket's
// default 32 KiB. Match the SSE parser's 4 MiB line budget while retaining a
// bounded per-message allocation on both fresh and cached connections.
const codexWebSocketMessageLimit = 4 * 1024 * 1024

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
			conn.SetReadLimit(codexWebSocketMessageLimit)
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

func codexWebSocketSessionExpired(entry *codexWebSocketSessionEntry, now time.Time) bool {
	return entry != nil && !entry.createdAt.IsZero() && now.Sub(entry.createdAt) >= codexWebSocketSessionMaxAge
}

func acquireCodexWebSocketSession(ctx context.Context, wsURL string, headers http.Header, sessionID string, retryCfg goai.RetryConfig, model *goai.Model) (*websocket.Conn, *codexWebSocketSessionEntry, bool, error) {
	key := codexWebSocketSessionKey{sessionID: sessionID, accountID: headers.Get("ChatGPT-Account-Id"), wsURL: wsURL}
	var expired *websocket.Conn
	codexWebSocketSessionsMu.Lock()
	if cached := codexWebSocketSessions[key]; cached != nil {
		if cached.idleTimer != nil {
			cached.idleTimer.Stop()
			cached.idleTimer = nil
		}
		if cached.busy {
			codexWebSocketSessionsMu.Unlock()
			conn, err := dialCodexWebSocket(ctx, wsURL, headers, retryCfg, model)
			return conn, nil, false, err
		}
		if !codexWebSocketSessionExpired(cached, time.Now()) {
			cached.busy = true
			codexWebSocketSessionsMu.Unlock()
			return cached.conn, cached, true, nil
		}
		delete(codexWebSocketSessions, key)
		expired = cached.conn
	}
	codexWebSocketSessionsMu.Unlock()
	if expired != nil {
		_ = expired.Close(websocket.StatusNormalClosure, "connection_age_limit")
	}
	conn, err := dialCodexWebSocket(ctx, wsURL, headers, retryCfg, model)
	if err != nil {
		return nil, nil, false, err
	}
	entry := &codexWebSocketSessionEntry{key: key, conn: conn, busy: true, createdAt: time.Now()}
	codexWebSocketSessionsMu.Lock()
	if codexWebSocketSessions[key] != nil {
		codexWebSocketSessionsMu.Unlock()
		return conn, nil, false, nil
	}
	codexWebSocketSessions[key] = entry
	codexWebSocketSessionsMu.Unlock()
	return conn, entry, false, nil
}

func removeCodexWebSocketSession(sessionID string, entry *codexWebSocketSessionEntry) {
	if sessionID == "" || entry == nil || entry.key.sessionID != sessionID {
		return
	}
	key := entry.key
	var conn *websocket.Conn
	codexWebSocketSessionsMu.Lock()
	if codexWebSocketSessions[key] == entry {
		if entry != nil && entry.idleTimer != nil {
			entry.idleTimer.Stop()
			entry.idleTimer = nil
		}
		if entry != nil {
			conn = entry.conn
		}
		delete(codexWebSocketSessions, key)
	}
	codexWebSocketSessionsMu.Unlock()
	if conn != nil {
		conn.CloseNow()
	}
}

func scheduleCodexWebSocketIdleClose(sessionID string, entry *codexWebSocketSessionEntry) {
	if sessionID == "" || entry == nil || entry.key.sessionID != sessionID {
		return
	}
	key := entry.key
	codexWebSocketSessionsMu.Lock()
	defer codexWebSocketSessionsMu.Unlock()
	if codexWebSocketSessions[key] != entry {
		return
	}
	entry.busy = false
	if entry.idleTimer != nil {
		entry.idleTimer.Stop()
	}
	var timer *time.Timer
	timer = time.AfterFunc(codexWebSocketSessionCacheTTL, func() { expireCodexWebSocketSession(key, entry, timer) })
	entry.idleTimer = timer
}

func expireCodexWebSocketSession(key codexWebSocketSessionKey, entry *codexWebSocketSessionEntry, timer *time.Timer) {
	codexWebSocketSessionsMu.Lock()
	var conn *websocket.Conn
	if codexWebSocketSessions[key] == entry && !entry.busy && entry.idleTimer == timer {
		delete(codexWebSocketSessions, key)
		entry.idleTimer = nil
		conn = entry.conn
	}
	codexWebSocketSessionsMu.Unlock()
	if conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "idle_timeout")
	}
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

func streamViaWebSocket(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions, apiKey string, send func(goai.Event), prepared ...*codexPreparedRequest) error {
	request, prepareErr := codexRequestForTransport(ctx, model, convCtx, opts, prepared)
	if prepareErr != nil {
		return prepareErr
	}
	toolView := request.tools

	wsURL := resolveCodexWSURL(model.BaseURL)
	goai.GetLogger().Debug("WebSocket connect", "url", wsURL, "provider", model.Provider)

	accountID, err := extractCodexAccountID(apiKey)
	if err != nil {
		return fmt.Errorf("extract codex account id: %w", err)
	}
	requestID := request.requestID
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
	}(), accountID, apiKey, requestID, func() *goai.ClientIdentity {
		if opts != nil {
			return opts.Identity
		}
		return nil
	}())

	retryCfg := goai.RetryConfigFromOptions(opts)
	if opts != nil && opts.WebSocketConnectTimeoutMs != nil {
		retryCfg.ConnectTimeout = time.Duration(*opts.WebSocketConnectTimeoutMs) * time.Millisecond
	}
	useCachedContext := opts != nil && opts.CacheRetention != goai.CacheRetentionNone && opts.SessionID != ""
	var (
		conn   *websocket.Conn
		entry  *codexWebSocketSessionEntry
		reused bool
	)
	keepEntry := false
	if useCachedContext {
		var err error
		conn, entry, reused, err = acquireCodexWebSocketSession(ctx, wsURL, headers, opts.SessionID, retryCfg, model)
		if err != nil {
			return err
		}
		useCachedContext = entry != nil
	} else {
		var err error
		conn, err = dialCodexWebSocket(ctx, wsURL, headers, retryCfg, model)
		if err != nil {
			return err
		}
	}
	if entry != nil {
		entry.mu.Lock()
		defer func() {
			entry.mu.Unlock()
			if keepEntry {
				scheduleCodexWebSocketIdleClose(opts.SessionID, entry)
			} else {
				removeCodexWebSocketSession(opts.SessionID, entry)
			}
		}()
	} else {
		defer conn.CloseNow()
	}

	bodyJSON := request.bodyJSON
	fullEnvelope := request.envelope
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
		Role:       goai.RoleAssistant,
		Api:        model.Api,
		Provider:   model.Provider,
		Model:      model.ID,
		Usage:      &goai.Usage{},
		StopReason: goai.StopReasonPending,
	}

	started := false
	sawCompletion := false
	transportFailure := func(err error) {
		configured := goai.TransportAuto
		session := ""
		if opts != nil {
			session = opts.SessionID
			if opts.Transport != "" {
				configured = opts.Transport
			}
		}
		partial.StopReason = goai.StopReasonError
		partial.ErrorMessage = err.Error()
		partial.Timestamp = time.Now().UnixMilli()
		goai.AppendAssistantMessageDiagnostic(partial, goai.CreateAssistantMessageDiagnostic("provider_transport_failure", err, map[string]any{"configuredTransport": string(configured), "eventsEmitted": true, "phase": "after_message_stream_start", "requestBytes": len(bodyJSON)}))
		recordCodexWebSocketFailure(session, err)
	}

	type activeItem struct {
		itemType       string
		contentIdx     int
		argumentStream codexArgumentStream
		customInput    *responsestream.CustomInput
	}
	var current *activeItem
	var slots responsestream.Slots[activeItem]
	unfinished := map[int]bool{}
	var reasoning responsestream.ReasoningIndex
	createSlot := func(encodedItem json.RawMessage, outputIndex *int) (*activeItem, error) {
		var created *activeItem
		created = nil
		var item struct {
			Type   string `json:"type"`
			ID     string `json:"id"`
			CallID string `json:"call_id"`
			Name   string `json:"name"`
		}
		json.Unmarshal(encodedItem, &item)

		switch item.Type {
		case "reasoning":
			partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking"})
			idx := len(partial.Content) - 1
			reasoning.Track(item.ID, idx)
			created = &activeItem{itemType: "reasoning", contentIdx: idx}
			slots.Add(outputIndex, created)
			send(&goai.ThinkingStartEvent{ContentIndex: idx, Partial: partial})
		case "message":
			if responsestream.IsFinalAnswer(encodedItem) {
				partial.StopReason = goai.StopReasonStop
			}
			partial.Content = append(partial.Content, goai.ContentBlock{Type: "text"})
			idx := len(partial.Content) - 1
			created = &activeItem{itemType: "message", contentIdx: idx}
			slots.Add(outputIndex, created)
			send(&goai.TextStartEvent{ContentIndex: idx, Partial: partial})
		case "function_call", "custom_tool_call":
			partial.Content = append(partial.Content, goai.ContentBlock{
				Type: "toolCall",
				ID:   fmt.Sprintf("%s|%s", item.CallID, item.ID),
				Name: item.Name,
			})
			idx := len(partial.Content) - 1
			created = &activeItem{itemType: item.Type, contentIdx: idx}
			if item.Type == "custom_tool_call" {
				custom, err := responsestream.NewCustomInput(encodedItem, toolView.inputProperty(item.Name))
				if err != nil {
					return nil, err
				}
				created.customInput = custom
				partial.Content[created.contentIdx].SetStreamingArguments(custom.Arguments(), "")
			}
			slots.Add(outputIndex, created)
			send(&goai.ToolCallStartEvent{ContentIndex: idx, Partial: partial})
		}

		if created != nil && (created.itemType == "function_call" || created.itemType == "custom_tool_call") {
			unfinished[created.contentIdx] = true
		}
		return created, nil
	}

	argumentProtocolError := func(err error, payload []byte) error {
		if useCachedContext && entry != nil && opts != nil {
			removeCodexWebSocketSession(opts.SessionID, entry)
		}
		return &codexProtocolError{message: "Invalid Codex stream event: " + goai.FormatThrownValue(err), payload: string(payload), cause: err, partial: partial}
	}

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
					partial.StopReason = goai.StopReasonAborted
					partial.ErrorMessage = ctx.Err().Error()
					send(&goai.ErrorEvent{Reason: goai.StopReasonAborted, Error: partial, Err: ctx.Err()})
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
			transportFailure(err)
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: err})
			return nil
		}

		observed, validEvent, observeErr := decodeCodexEvent(data, "WebSocket", opts, model, partial)
		if observeErr != nil {
			if useCachedContext && entry != nil && opts != nil {
				entry.continuation = nil
				removeCodexWebSocketSession(opts.SessionID, entry)
			}
			if started && !isCodexNonTransportError(observeErr) {
				transportFailure(observeErr)
				send(codexStreamErrorEvent(observeErr))
				return nil
			}
			return observeErr
		}
		if !validEvent {
			continue
		}
		data = observed
		if ctx.Err() != nil {
			if useCachedContext && entry != nil && opts != nil {
				entry.continuation = nil
				removeCodexWebSocketSession(opts.SessionID, entry)
			}
			partial.StopReason = goai.StopReasonAborted
			partial.ErrorMessage = ctx.Err().Error()
			send(&goai.ErrorEvent{Reason: goai.StopReasonAborted, Error: partial, Err: ctx.Err()})
			return nil
		}

		var raw struct {
			OutputIndex *int             `json:"output_index"`
			Type        codexEventType   `json:"type"`
			Item        json.RawMessage  `json:"item,omitempty"`
			Response    json.RawMessage  `json:"response,omitempty"`
			Delta       string           `json:"delta,omitempty"`
			Code        string           `json:"code,omitempty"`
			Message     string           `json:"message,omitempty"`
			Error       *codexEventError `json:"error,omitempty"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			if !started {
				return &codexProtocolError{message: "Invalid Codex WebSocket JSON: " + goai.FormatThrownValue(err), payload: string(data), cause: err}
			}
			continue
		}
		if raw.Type != "error" && raw.Type != "response.failed" && !started {
			started = true
			send(&goai.StartEvent{Partial: partial})
		}

		// Same event processing as OpenAI Responses
		if raw.Type != "response.output_item.added" {
			current = slots.Get(raw.OutputIndex)
		}
		switch raw.Type {
		case "response.created":
			var resp struct {
				ID string `json:"id"`
			}
			json.Unmarshal(raw.Response, &resp)
			partial.ResponseID = resp.ID

		case "response.output_item.added":
			slot, slotErr := createSlot(raw.Item, raw.OutputIndex)
			current = slot
			if slotErr != nil {
				return argumentProtocolError(slotErr, data)
			}

		case "response.reasoning_summary_text.delta":
			if current != nil && current.itemType == "reasoning" {
				partial.Content[current.contentIdx].Thinking += raw.Delta
				send(&goai.ThinkingDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial})
			}

		case "response.output_text.delta":
			if current != nil && current.itemType == "message" {
				partial.Content[current.contentIdx].Text += raw.Delta
				send(&goai.TextDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial})
			}

		case "response.function_call_arguments.delta":
			if current != nil && current.itemType == "function_call" {
				delta, err := current.argumentStream.append(data, &partial.Content[current.contentIdx])
				if err != nil {
					return argumentProtocolError(err, data)
				}
				send(&goai.ToolCallDeltaEvent{ContentIndex: current.contentIdx, Delta: delta, Partial: partial})
			}

		case "response.custom_tool_call_input.delta", "response.custom_tool_call_input.done":
			if current != nil && current.itemType == "custom_tool_call" {
				var delta string
				var err error
				if raw.Type == "response.custom_tool_call_input.done" {
					delta, err = current.customInput.Close(data)
				} else {
					delta, err = current.customInput.Append(data)
				}
				if err != nil {
					return argumentProtocolError(err, data)
				}
				partial.Content[current.contentIdx].SetStreamingArguments(current.customInput.Arguments(), "")
				if delta != "" {
					send(&goai.ToolCallDeltaEvent{ContentIndex: current.contentIdx, Delta: delta, Partial: partial})
				}
			}

		case "response.function_call_arguments.done":
			if current != nil && current.itemType == "function_call" {
				delta, err := current.argumentStream.replace(data, &partial.Content[current.contentIdx])
				if err != nil {
					return argumentProtocolError(err, data)
				}
				if delta != "" {
					send(&goai.ToolCallDeltaEvent{ContentIndex: current.contentIdx, Delta: delta, Partial: partial})
				}
			}

		case "response.output_item.done":
			if current == nil {
				slot, slotErr := createSlot(raw.Item, raw.OutputIndex)
				current = slot
				if slotErr != nil {
					return argumentProtocolError(slotErr, data)
				}
			}
			if current == nil {
				continue
			}
			if !responsestream.MatchesItem(raw.Item, current.itemType) {
				continue
			}
			idx := current.contentIdx
			text, thinking, hasText := responsestream.FinalText(raw.Item)
			if hasText {
				partial.Content[idx].Text = text
			}
			if thinking != "" {
				partial.Content[idx].Thinking = thinking
			}

			switch current.itemType {
			case "reasoning":
				partial.Content[idx].ThinkingSignature = string(raw.Item)
				send(&goai.ThinkingEndEvent{ContentIndex: idx, Content: partial.Content[idx].Thinking, Partial: partial})
			case "message":
				partial.Content[idx].TextSignature = responsestream.MessageTextSignature(raw.Item)
				send(&goai.TextEndEvent{ContentIndex: idx, Content: partial.Content[idx].Text, Partial: partial})
			case "custom_tool_call":
				delta, err := current.customInput.Close(raw.Item)
				if err != nil {
					return argumentProtocolError(err, data)
				}
				partial.Content[idx].SetStreamingArguments(current.customInput.Arguments(), "")
				if delta != "" {
					send(&goai.ToolCallDeltaEvent{ContentIndex: idx, Delta: delta, Partial: partial})
				}
				call := goai.ToolCall{Type: "toolCall", ID: partial.Content[idx].ID, Name: partial.Content[idx].Name}
				call.SetStreamingArguments(current.customInput.Arguments(), "")
				send(&goai.ToolCallEndEvent{ContentIndex: idx, ToolCall: call, Partial: partial})
			case "function_call":
				call, err := current.argumentStream.finish(raw.Item, &partial.Content[idx])
				if err != nil {
					return argumentProtocolError(err, data)
				}
				send(&goai.ToolCallEndEvent{ContentIndex: idx, ToolCall: call, Partial: partial})
			}
			delete(unfinished, current.contentIdx)
			slots.Delete(raw.OutputIndex)
			current = nil

		case "response.completed", "response.incomplete", "response.done":
			sawCompletion = true
			if err := finalizeCodexResponse(raw.Response, partial, model, reasoning); err != nil {
				return argumentProtocolError(err, data)
			}
			break readLoop

		case "error":
			if useCachedContext && entry != nil {
				entry.continuation = nil
				removeCodexWebSocketSession(opts.SessionID, entry)
			}
			eventErr := extractCodexEventError(raw.Code, raw.Message, raw.Error)
			err := &codexAPIError{message: fmt.Sprintf("Codex error: %s", firstNonEmpty(eventErr.Message, eventErr.Code, string(data))), code: eventErr.Code, payload: string(data)}
			if !started {
				if useCachedContext && entry != nil && opts != nil {
					removeCodexWebSocketSession(opts.SessionID, entry)
				}
				return err
			}
			partial.StopReason = goai.StopReasonError
			partial.ErrorMessage = err.Error()
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: err})
			return nil

		case "response.failed":
			if useCachedContext && entry != nil {
				entry.continuation = nil
				removeCodexWebSocketSession(opts.SessionID, entry)
			}
			var failed struct {
				Response *struct {
					Status string `json:"status"`
					Error  *struct {
						Code    string `json:"code"`
						Message string `json:"message"`
					} `json:"error"`
				} `json:"response"`
			}
			_ = json.Unmarshal(data, &failed)
			code, message := "", ""
			if failed.Response != nil && failed.Response.Error != nil {
				code = failed.Response.Error.Code
				message = failed.Response.Error.Message
			}
			err := &codexAPIError{message: firstNonEmpty(message, "Codex response failed"), code: code, payload: string(data)}
			if failed.Response != nil {
				partial.RawStopReason = failed.Response.Status
			}
			if !started {
				return err
			}
			partial.StopReason = goai.StopReasonError
			partial.ErrorMessage = err.Error()
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: err})
			return nil
		}
	}

	if !sawCompletion {
		if useCachedContext && entry != nil && opts != nil {
			removeCodexWebSocketSession(opts.SessionID, entry)
			entry.continuation = nil
		}
		err := fmt.Errorf("WebSocket stream closed before response.completed")
		if started {
			transportFailure(err)
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: err})
			return nil
		}
		return err
	}

	if partial.StopReason == goai.StopReasonToolUse && len(unfinished) > 0 {
		return argumentProtocolError(fmt.Errorf("Tool call arguments did not complete before stream ended"), nil)
	}

	if err := responsestream.TerminalFailure(partial); err != nil {
		partial.Timestamp = time.Now().UnixMilli()
		if useCachedContext && entry != nil && opts != nil {
			entry.continuation = nil
			removeCodexWebSocketSession(opts.SessionID, entry)
		}
		send(&goai.ErrorEvent{Reason: partial.StopReason, Error: partial, Err: err})
		return nil
	}
	if useCachedContext && entry != nil && partial.ResponseID != "" {
		responseItems := buildCodexInputWithToolView(model, &goai.Context{Messages: []goai.Message{*partial}}, toolView)
		filtered := make([]interface{}, 0, len(responseItems))
		for _, item := range responseItems {
			if m, ok := item.(map[string]interface{}); ok && m["type"] == "function_call_output" {
				continue
			}
			filtered = append(filtered, item)
		}
		normalized, _ := normalizeJSONValue(filtered).([]interface{})
		entry.continuation = &codexWebSocketContinuation{lastRequestBody: fullEnvelope, lastResponseID: partial.ResponseID, lastResponseItems: normalized}
		keepEntry = true
	} else if useCachedContext && entry != nil {
		entry.continuation = nil
		keepEntry = true
	} else {
		conn.Close(websocket.StatusNormalClosure, "done")
	}

	partial.Timestamp = time.Now().UnixMilli()
	if partial.StopReason == goai.StopReasonPending {
		partial.StopReason = goai.StopReasonError
		partial.ErrorMessage = "Codex stream ended before a terminal response event"
		send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("codex stream ended before a terminal response event")})
		return nil
	}
	send(&goai.DoneEvent{Reason: partial.StopReason, Message: partial})
	return nil
}

// --- SSE fallback ---

func compressCodexRequestBodyZstd(bodyJSON []byte) ([]byte, bool) {
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(codexRequestCompressionZstdLevel)))
	if err != nil {
		return nil, false
	}
	if _, err := enc.Write(bodyJSON); err != nil {
		_ = enc.Close()
		return nil, false
	}
	if err := enc.Close(); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

func streamViaSSE(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions, apiKey string, send func(goai.Event), diagnostics []goai.AssistantMessageDiagnostic, prepared ...*codexPreparedRequest) {
	request, prepareErr := codexRequestForTransport(ctx, model, convCtx, opts, prepared)
	if prepareErr != nil {
		reason := goai.StopReasonError
		if ctx.Err() != nil {
			reason = goai.StopReasonAborted
		}
		send(&goai.ErrorEvent{Reason: reason, Err: prepareErr})
		return
	}
	toolView := request.tools
	bodyJSON := request.bodyJSON
	url := resolveCodexURL(model.BaseURL)
	goai.GetLogger().Debug("HTTP request", "url", url, "provider", model.Provider, "model", model.ID)
	reqBody := bodyJSON
	compressedBody, compressed := compressCodexRequestBodyZstd(bodyJSON)
	if compressed {
		reqBody = compressedBody
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: err})
		return
	}
	accountID, err := extractCodexAccountID(apiKey)
	if err != nil {
		send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("extract codex account id: %w", err)})
		return
	}
	sessionID := codexCacheSessionID(opts)
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
	if compressed {
		req.Header.Set("Content-Encoding", "zstd")
	}

	retryCfg := goai.RetryConfigFromOptions(opts)
	client := retryCfg.NewHTTPClient()
	resp, err := goai.DoProviderRequestWithRetry(ctx, client, req, retryCfg)
	if err != nil {
		if ctx.Err() != nil {
			send(&goai.ErrorEvent{Reason: goai.StopReasonAborted, Err: ctx.Err()})
		} else {
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: err})
		}
		return
	}
	defer resp.Body.Close()

	goai.InvokeOnResponse(opts, resp, model)

	if resp.StatusCode != 200 {
		goai.GetLogger().Warn("HTTP error response", "status", resp.StatusCode, "provider", model.Provider, "model", model.ID)
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))})
		return
	}

	// Reuse same SSE processing as OpenAI Responses
	processCodexSSEWithOptions(ctx, resp.Body, model, opts, send, diagnostics, toolView)
}

// processCodexSSE mirrors the OpenAI Responses-style event processing used by pi-ai.
func processCodexSSE(body io.Reader, model *goai.Model, send func(goai.Event), diagnostics []goai.AssistantMessageDiagnostic, views ...codexToolView) {
	toolView := codexToolView{}
	if len(views) > 0 {
		toolView = views[0]
	}
	processCodexSSEWithOptions(context.Background(), body, model, nil, send, diagnostics, toolView)
}
func processCodexSSEWithOptions(ctx context.Context, body io.Reader, model *goai.Model, opts *goai.StreamOptions, send func(goai.Event), diagnostics []goai.AssistantMessageDiagnostic, toolView codexToolView) {
	terminalResponseSeen := false
	partial := &goai.Message{
		Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, Usage: &goai.Usage{}, Diagnostics: diagnostics, StopReason: goai.StopReasonPending,
	}
	send(&goai.StartEvent{Partial: partial})

	type activeItem struct {
		itemType       string
		contentIdx     int
		argumentStream codexArgumentStream
		customInput    *responsestream.CustomInput
	}
	var current *activeItem
	var slots responsestream.Slots[activeItem]
	unfinished := map[int]bool{}
	var reasoning responsestream.ReasoningIndex
	createSlot := func(encodedItem json.RawMessage, outputIndex *int) (*activeItem, error) {
		var created *activeItem
		created = nil
		var item struct {
			Type   string `json:"type"`
			ID     string `json:"id"`
			CallID string `json:"call_id"`
			Name   string `json:"name"`
		}
		json.Unmarshal(encodedItem, &item)
		switch item.Type {
		case "reasoning":
			partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking"})
			created = &activeItem{itemType: "reasoning", contentIdx: len(partial.Content) - 1}
			reasoning.Track(item.ID, created.contentIdx)
			slots.Add(outputIndex, created)
			send(&goai.ThinkingStartEvent{ContentIndex: created.contentIdx, Partial: partial})
		case "message":
			if responsestream.IsFinalAnswer(encodedItem) {
				partial.StopReason = goai.StopReasonStop
			}
			partial.Content = append(partial.Content, goai.ContentBlock{Type: "text"})
			created = &activeItem{itemType: "message", contentIdx: len(partial.Content) - 1}
			slots.Add(outputIndex, created)
			send(&goai.TextStartEvent{ContentIndex: created.contentIdx, Partial: partial})
		case "function_call", "custom_tool_call":
			partial.Content = append(partial.Content, goai.ContentBlock{Type: "toolCall", ID: fmt.Sprintf("%s|%s", item.CallID, item.ID), Name: item.Name})
			created = &activeItem{itemType: item.Type, contentIdx: len(partial.Content) - 1}
			if item.Type == "custom_tool_call" {
				custom, err := responsestream.NewCustomInput(encodedItem, toolView.inputProperty(item.Name))
				if err != nil {
					return nil, err
				}
				created.customInput = custom
				partial.Content[created.contentIdx].SetStreamingArguments(custom.Arguments(), "")
			}
			slots.Add(outputIndex, created)
			send(&goai.ToolCallStartEvent{ContentIndex: created.contentIdx, Partial: partial})
		}
		if created != nil && (created.itemType == "function_call" || created.itemType == "custom_tool_call") {
			unfinished[created.contentIdx] = true
		}
		return created, nil
	}

	parseCtx, cancelParser := context.WithCancel(ctx)
	defer cancelParser()
	events := sse.ParseContext(parseCtx, body)
	abort := func() bool {
		if ctx.Err() == nil {
			return false
		}
		partial.StopReason = goai.StopReasonAborted
		partial.ErrorMessage = ctx.Err().Error()
		send(&goai.ErrorEvent{Reason: goai.StopReasonAborted, Error: partial, Err: ctx.Err()})
		return true
	}
eventsLoop:
	for evt := range events {
		if abort() {
			return
		}
		if evt.Event == sse.EventError {
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("SSE stream error: %s", evt.Data)})
			return
		}
		evt.Data = strings.TrimSpace(evt.Data)
		if evt.Data == "[DONE]" || evt.Data == "" {
			continue
		}
		observed, validEvent, observeErr := decodeCodexEvent([]byte(evt.Data), "SSE", opts, model, partial)
		if observeErr != nil {
			event := codexStreamErrorEvent(observeErr)
			if ctx.Err() != nil {
				event.Reason = goai.StopReasonAborted
				event.Err = ctx.Err()
				partial.StopReason = goai.StopReasonAborted
				partial.ErrorMessage = ctx.Err().Error()
			}
			send(event)
			return
		}
		if !validEvent {
			continue
		}

		if ctx.Err() != nil {
			partial.StopReason = goai.StopReasonAborted
			partial.ErrorMessage = ctx.Err().Error()
			send(&goai.ErrorEvent{Reason: goai.StopReasonAborted, Error: partial, Err: ctx.Err()})
			return
		}

		var raw struct {
			OutputIndex *int             `json:"output_index"`
			Type        codexEventType   `json:"type"`
			Item        json.RawMessage  `json:"item,omitempty"`
			Response    json.RawMessage  `json:"response,omitempty"`
			Delta       string           `json:"delta,omitempty"`
			Code        string           `json:"code,omitempty"`
			Message     string           `json:"message,omitempty"`
			Error       *codexEventError `json:"error,omitempty"`
		}
		evt.Data = string(observed)
		if json.Unmarshal(observed, &raw) != nil {
			continue
		}

		if raw.Type != "response.output_item.added" {
			current = slots.Get(raw.OutputIndex)
		}
		switch raw.Type {
		case "response.created":
			var resp struct {
				ID string `json:"id"`
			}
			json.Unmarshal(raw.Response, &resp)
			partial.ResponseID = resp.ID
		case "response.output_item.added":
			slot, slotErr := createSlot(raw.Item, raw.OutputIndex)
			current = slot
			if slotErr != nil {
				send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: slotErr})
				return
			}

		case "response.reasoning_summary_text.delta":
			if current != nil && current.itemType == "reasoning" {
				partial.Content[current.contentIdx].Thinking += raw.Delta
				send(&goai.ThinkingDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial})
			}
		case "response.output_text.delta":
			if current != nil && current.itemType == "message" {
				partial.Content[current.contentIdx].Text += raw.Delta
				send(&goai.TextDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial})
			}
		case "response.function_call_arguments.delta":
			if current != nil && current.itemType == "function_call" {
				delta, err := current.argumentStream.append([]byte(evt.Data), &partial.Content[current.contentIdx])
				if err != nil {
					send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: err})
					return
				}
				send(&goai.ToolCallDeltaEvent{ContentIndex: current.contentIdx, Delta: delta, Partial: partial})
			}

		case "response.custom_tool_call_input.delta", "response.custom_tool_call_input.done":
			if current != nil && current.itemType == "custom_tool_call" {
				var delta string
				var err error
				if raw.Type == "response.custom_tool_call_input.done" {
					delta, err = current.customInput.Close([]byte(evt.Data))
				} else {
					delta, err = current.customInput.Append([]byte(evt.Data))
				}
				if err != nil {
					send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: err})
					return
				}
				partial.Content[current.contentIdx].SetStreamingArguments(current.customInput.Arguments(), "")
				if delta != "" {
					send(&goai.ToolCallDeltaEvent{ContentIndex: current.contentIdx, Delta: delta, Partial: partial})
				}
			}

		case "response.function_call_arguments.done":
			if current != nil && current.itemType == "function_call" {
				delta, err := current.argumentStream.replace([]byte(evt.Data), &partial.Content[current.contentIdx])
				if err != nil {
					send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: err})
					return
				}
				if delta != "" {
					send(&goai.ToolCallDeltaEvent{ContentIndex: current.contentIdx, Delta: delta, Partial: partial})
				}
			}

		case "response.output_item.done":
			if current == nil {
				slot, slotErr := createSlot(raw.Item, raw.OutputIndex)
				current = slot
				if slotErr != nil {
					send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: slotErr})
					return
				}
			}
			if current == nil {
				continue
			}
			if !responsestream.MatchesItem(raw.Item, current.itemType) {
				continue
			}
			idx := current.contentIdx
			text, thinking, hasText := responsestream.FinalText(raw.Item)
			if hasText {
				partial.Content[idx].Text = text
			}
			if thinking != "" {
				partial.Content[idx].Thinking = thinking
			}

			switch current.itemType {
			case "reasoning":
				partial.Content[idx].ThinkingSignature = string(raw.Item)
				send(&goai.ThinkingEndEvent{ContentIndex: idx, Content: partial.Content[idx].Thinking, Partial: partial})
			case "message":
				partial.Content[idx].TextSignature = responsestream.MessageTextSignature(raw.Item)
				send(&goai.TextEndEvent{ContentIndex: idx, Content: partial.Content[idx].Text, Partial: partial})
			case "custom_tool_call":
				delta, err := current.customInput.Close(raw.Item)
				if err != nil {
					send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: err})
					return
				}
				partial.Content[idx].SetStreamingArguments(current.customInput.Arguments(), "")
				if delta != "" {
					send(&goai.ToolCallDeltaEvent{ContentIndex: idx, Delta: delta, Partial: partial})
				}
				call := goai.ToolCall{Type: "toolCall", ID: partial.Content[idx].ID, Name: partial.Content[idx].Name}
				call.SetStreamingArguments(current.customInput.Arguments(), "")
				send(&goai.ToolCallEndEvent{ContentIndex: idx, ToolCall: call, Partial: partial})
			case "function_call":
				call, err := current.argumentStream.finish(raw.Item, &partial.Content[idx])
				if err != nil {
					send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: err})
					return
				}
				send(&goai.ToolCallEndEvent{ContentIndex: idx, ToolCall: call, Partial: partial})
			}
			delete(unfinished, current.contentIdx)
			slots.Delete(raw.OutputIndex)
			current = nil
		case "error":
			eventErr := extractCodexEventError(raw.Code, raw.Message, raw.Error)
			err := fmt.Errorf("API error %s: %s", eventErr.Code, eventErr.Message)
			partial.StopReason = goai.StopReasonError
			partial.ErrorMessage = err.Error()
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: err})
			return
		case "response.failed":
			var failed struct {
				Response *struct {
					Status string `json:"status"`
					Error  *struct {
						Message string `json:"message"`
					} `json:"error"`
				} `json:"response"`
			}
			_ = json.Unmarshal([]byte(evt.Data), &failed)
			message := "Codex response failed"
			if failed.Response != nil && failed.Response.Error != nil && failed.Response.Error.Message != "" {
				message = failed.Response.Error.Message
			}
			if failed.Response != nil {
				partial.RawStopReason = failed.Response.Status
			}
			partial.StopReason = goai.StopReasonError
			partial.ErrorMessage = message
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("%s", message)})
			return
		case "response.completed", "response.incomplete", "response.done":
			terminalResponseSeen = true
			if err := finalizeCodexResponse(raw.Response, partial, model, reasoning); err != nil {
				send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: err})
				return
			}
			break eventsLoop
		}
	}

	partial.Timestamp = time.Now().UnixMilli()
	if abort() {
		return
	}

	if !terminalResponseSeen {
		partial.StopReason = goai.StopReasonError
		partial.ErrorMessage = "Codex stream ended before a terminal response event"
		send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("codex stream ended before a terminal response event")})
		return
	}
	for _, c := range partial.Content {
		if c.Type == "toolCall" && partial.StopReason == goai.StopReasonStop {
			partial.StopReason = goai.StopReasonToolUse
			break
		}
	}
	if partial.StopReason == goai.StopReasonToolUse && len(unfinished) > 0 {
		send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("Tool call arguments did not complete before stream ended")})
		return
	}

	if err := responsestream.TerminalFailure(partial); err != nil {
		send(&goai.ErrorEvent{Reason: partial.StopReason, Error: partial, Err: err})
		return
	}
	send(&goai.DoneEvent{Reason: partial.StopReason, Message: partial})
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
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  json.RawMessage        `json:"parameters,omitempty"`
	Strict      *bool                  `json:"strict,omitempty"`
	Format      map[string]interface{} `json:"format,omitempty"`
}

func buildCodexRequest(model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions, views ...codexToolView) codexRequest {
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
		req.MaxOutputTokens = goai.ClampStreamMaxTokensPtr(model, convCtx, opts)
		if opts.TextVerbosity != "" {
			req.Text = map[string]interface{}{"verbosity": opts.TextVerbosity}
		}
		if opts.SessionID != "" {
			req.PromptCacheKey = codexCacheSessionID(opts)
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
	if opts != nil && opts.ToolChoice != "" {
		req.ToolChoice = string(opts.ToolChoice)
	}

	// Build input in Responses-compatible format, but with system prompt carried in top-level instructions.
	view := codexToolView{}
	if len(views) > 0 {
		view = views[0]
	} else {
		view, _ = newCodexToolView(model, convCtx)
	}
	input := buildCodexInputWithToolView(model, convCtx, view)
	inputJSON, _ := json.Marshal(input)
	req.Input = inputJSON

	supportsGrammar := model.ResponsesCompat != nil && model.ResponsesCompat.SupportsOpenAIGrammarTools != nil && *model.ResponsesCompat.SupportsOpenAIGrammarTools
	supportsStrictMode := model.ResponsesCompat == nil || model.ResponsesCompat.SupportsStrictMode == nil || *model.ResponsesCompat.SupportsStrictMode
	for _, t := range convCtx.Tools {
		if supportsGrammar && t.ConstrainedSampling != nil && t.ConstrainedSampling.Type == "grammar" {
			definition := t.ConstrainedSampling.Variants["openai_lark"]
			syntax := "lark"
			if strings.TrimSpace(definition) == "" {
				definition = t.ConstrainedSampling.Variants["openai_regex"]
				syntax = "regex"
			}
			if strings.TrimSpace(definition) != "" {
				req.Tools = append(req.Tools, codexTool{Type: "custom", Name: t.Name, Format: map[string]interface{}{"type": "grammar", "syntax": syntax, "definition": definition}})
				continue
			}
		}
		parameters := t.Parameters
		strictValue := false
		if strict, err := goai.ResolveJSONSchemaStrictSampling(t, supportsStrictMode); err == nil && strict != nil {
			strictValue = *strict
			if *strict {
				if strictParameters, err := goai.JSONSchemaToolParameters(t, true); err == nil {
					parameters = strictParameters
				}
			}
		}
		var strictPtr *bool
		if supportsStrictMode {
			strictPtr = &strictValue
		}
		req.Tools = append(req.Tools, codexTool{
			Type: "function", Name: t.Name, Description: t.Description, Parameters: parameters, Strict: strictPtr,
		})
	}

	return req
}

func buildCodexInput(model *goai.Model, convCtx *goai.Context) []interface{} {
	view, _ := newCodexToolView(model, convCtx)
	return buildCodexInputWithToolView(model, convCtx, view)
}
func buildCodexInputWithToolView(model *goai.Model, convCtx *goai.Context, view codexToolView) []interface{} {
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
			input = append(input, buildCodexAssistantItemsWithToolView(msg, model, view)...)
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
	view, _ := newCodexToolView(model, &goai.Context{Messages: []goai.Message{msg}})
	return buildCodexAssistantItemsWithToolView(msg, model, view)
}
func buildCodexAssistantItemsWithToolView(msg goai.Message, model *goai.Model, view codexToolView) []interface{} {
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
				msgID, phase := responsestream.ParseTextSignature(block.TextSignature)
				if len(msgID) > 64 {
					msgID = fmt.Sprintf("msg_%x", crc32.ChecksumIEEE([]byte(msgID)))
				}
				if msgID != "" {
					item["id"] = msgID
				}
				if phase != "" {
					item["phase"] = phase
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
			if isDifferentModel && (strings.HasPrefix(itemID, "fc_") || strings.HasPrefix(itemID, "ctc_")) {
				itemID = ""
			}
			item := map[string]interface{}{"type": "function_call", "call_id": callID, "name": block.Name}
			if property, selected := view.properties[block.Name]; selected {
				input, _ := codexGrammarInput(block, property)
				item["type"] = "custom_tool_call"
				item["input"] = goai.SanitizeSurrogates(input)
			} else {
				var value any = block.Arguments
				if block.HasStreamingArguments() {
					value = goai.StreamingArgumentsJSON(block.Arguments)
				}
				encoded, _ := json.Marshal(value)
				item["arguments"] = string(encoded)
			}

			if itemID != "" {
				item["id"] = itemID
			}
			items = append(items, item)
		}
	}
	return items
}
