package openaicodex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/klauspost/compress/zstd"
	goai "github.com/rcarmo/go-ai"
)

const payloadJWT = "eyJhbGciOiJub25lIn0.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdF8xMjMifX0."

func TestCodexPayloadFailureBeforeConnectionMain(t *testing.T) {
	for _, transport := range []goai.Transport{goai.TransportAuto, goai.TransportSSE, goai.TransportWebSocketCached} {
		for _, mode := range []string{"hook", "marshal", "array", "null", "pre_cancel", "hook_cancel"} {
			t.Run(string(transport)+"/"+mode, func(t *testing.T) {
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					http.Error(w, "unexpected request", 400)
				}))
				defer server.Close()
				model := codexGrammarModel()
				model.BaseURL = server.URL
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if mode == "pre_cancel" {
					cancel()
				}
				sentinel := errors.New("hook failed")
				hooks := 0
				opts := &goai.StreamOptions{APIKey: payloadJWT, Transport: transport, SessionID: t.Name(), OnPayload: func(payload any, _ *goai.Model) (any, error) {
					hooks++
					switch mode {
					case "hook":
						return nil, sentinel
					case "marshal":
						return map[string]any{"bad": math.NaN()}, nil
					case "array":
						return []any{}, nil
					case "null":
						return json.RawMessage("null"), nil
					case "hook_cancel":
						cancel()
					}
					return payload, nil
				}}
				defer CloseOpenAICodexWebSocketSessions(opts.SessionID)
				defer ResetOpenAICodexWebSocketDebugStats(opts.SessionID)
				failures, done := 0, 0
				for event := range streamCodex(ctx, model, &goai.Context{}, opts) {
					switch e := event.(type) {
					case *goai.DoneEvent:
						done++
					case *goai.ErrorEvent:
						failures++
						want := goai.StopReasonError
						if strings.Contains(mode, "cancel") {
							want = goai.StopReasonAborted
						}
						if e.Reason != want || e.Err == nil {
							t.Error("wrong payload failure", e)
						}
						if mode == "hook" && !errors.Is(e.Err, sentinel) {
							t.Error("hook cause lost", e.Err)
						}
					}
				}
				wantHooks := 1
				if mode == "pre_cancel" {
					wantHooks = 0
				}
				if failures != 1 || done != 0 || hooks != wantHooks || requests.Load() != 0 {
					t.Fatal("payload failure opened transport/replayed hook", failures, done, hooks, requests.Load())
				}
				if stats := GetOpenAICodexWebSocketDebugStats(opts.SessionID); stats != nil && (stats.WebSocketFailures != 0 || stats.SSEFallbacks != 0) {
					t.Fatal("local payload failure counted as network", stats)
				}
			})
		}
	}
}

func TestCodexPayloadPreparedOnceAcrossFallbackMain(t *testing.T) {
	var ws, sse atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			ws.Add(1)
			http.Error(w, "no websocket", 426)
			return
		}
		sse.Add(1)
		var reader io.Reader = r.Body
		if r.Header.Get("Content-Encoding") == "zstd" {
			decoder, err := zstd.NewReader(r.Body)
			if err != nil {
				t.Error(err)
				return
			}
			defer decoder.Close()
			reader = decoder
		}
		var payload map[string]any
		if err := json.NewDecoder(reader).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		if payload["marker"] != "1" || payload["model"] != "hook_model" {
			t.Error("fallback changed prepared payload", payload)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"usage\":{}}}\n\n"))
	}))
	defer server.Close()
	model := codexGrammarModel()
	model.BaseURL = server.URL
	hooks, telemetry := 0, 0
	var payload map[string]any
	opts := &goai.StreamOptions{APIKey: payloadJWT, SessionID: t.Name(), Transport: goai.TransportAuto, OnPayload: func(any, *goai.Model) (any, error) {
		hooks++
		payload = map[string]any{"model": "hook_model", "input": []any{}, "marker": strconv.Itoa(hooks), "extra": "<😃>"}
		return payload, nil
	}}
	opts.OnPayloadWithTelemetry = func(value any, _ *goai.Model, _ *goai.TelemetrySpan) (any, error) { telemetry++; return value, nil }
	defer ResetOpenAICodexWebSocketDebugStats(opts.SessionID)
	defer CloseOpenAICodexWebSocketSessions(opts.SessionID)
	var final *goai.Message
	for event := range streamCodex(context.Background(), model, &goai.Context{}, opts) {
		switch e := event.(type) {
		case *goai.DoneEvent:
			final = e.Message
		case *goai.ErrorEvent:
			t.Error(e.Err)
		}
	}
	if final == nil || hooks != 1 || telemetry != 1 || ws.Load() != 1 || sse.Load() != 1 {
		t.Fatal("payload callbacks repeated across fallback", final, hooks, telemetry, ws.Load(), sse.Load())
	}
	if len(final.Diagnostics) != 1 {
		t.Fatal("missing fallback diagnostic", final)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	details := final.Diagnostics[0].Details
	if details["requestBytes"] != len(encoded) || details["eventsEmitted"] != false || details["fallbackTransport"] != "sse" {
		t.Fatal("pre-start byte accounting mismatch", details, len(encoded))
	}
}
