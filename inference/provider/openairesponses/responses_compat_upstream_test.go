package openairesponses

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type capturedResponsesRequest struct {
	Header http.Header
	Body   map[string]any
	Path   string
}

func captureResponsesRequest(t *testing.T, model *goai.Model, opts *goai.StreamOptions, contexts ...*goai.Context) capturedResponsesRequest {
	t.Helper()
	var captured capturedResponsesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		captured.Header = r.Header.Clone()
		captured.Path = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &captured.Body); err != nil {
			t.Fatalf("unmarshal request body: %v; body=%s", err, body)
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"usage\":{\"input_tokens\":0,\"output_tokens\":0,\"total_tokens\":0}}}\n\n"))
	}))
	t.Cleanup(server.Close)

	requestModel := *model
	if strings.Contains(requestModel.BaseURL, "openrouter.ai") {
		requestModel.BaseURL = server.URL + "/openrouter.ai"
	} else {
		requestModel.BaseURL = server.URL
	}
	if opts == nil {
		opts = &goai.StreamOptions{}
	}
	if opts.APIKey == "" {
		opts.APIKey = "test-key"
	}
	conv := &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}
	if len(contexts) > 0 {
		conv = contexts[0]
	}
	for range streamResponses(context.Background(), &requestModel, conv, opts) {
	}
	return captured
}

func TestOpenAIResponsesCompatSessionAffinityFormats(t *testing.T) {
	for _, tc := range []struct {
		name        string
		model       *goai.Model
		wantSession string
		wantClient  string
		wantX       string
	}{
		{
			name:        "openai default",
			model:       &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses},
			wantSession: "session-123",
			wantClient:  "session-123",
		},
		{
			name:  "openrouter explicit compat",
			model: &goai.Model{ID: "gpt-test", Provider: "proxy", Api: goai.ApiOpenAIResponses, ResponsesCompat: &goai.OpenAIResponsesCompat{SessionAffinityFormat: "openrouter"}},
			wantX: "session-123",
		},
		{
			name:  "openrouter provider autodetect",
			model: &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenRouter, Api: goai.ApiOpenAIResponses},
			wantX: "session-123",
		},
		{
			name:  "openrouter endpoint autodetect",
			model: &goai.Model{ID: "gpt-test", Provider: "proxy", Api: goai.ApiOpenAIResponses, BaseURL: "https://openrouter.ai/api/v1"},
			wantX: "session-123",
		},
		{
			name:       "openai no session explicit compat",
			model:      &goai.Model{ID: "gpt-test", Provider: "proxy", Api: goai.ApiOpenAIResponses, ResponsesCompat: &goai.OpenAIResponsesCompat{SessionAffinityFormat: "openai-nosession"}},
			wantClient: "session-123",
		},
		{
			name:       "opencode generated compat",
			model:      &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenCode, Api: goai.ApiOpenAIResponses, ResponsesCompat: &goai.OpenAIResponsesCompat{SessionAffinityFormat: "openai-nosession"}},
			wantClient: "session-123",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured := captureResponsesRequest(t, tc.model, &goai.StreamOptions{SessionID: "session-123"})
			if got := captured.Header.Get("session-id"); got != tc.wantSession {
				t.Fatalf("session-id=%q, want %q; headers=%#v", got, tc.wantSession, captured.Header)
			}
			if got := captured.Header.Get("x-client-request-id"); got != tc.wantClient {
				t.Fatalf("x-client-request-id=%q, want %q; headers=%#v", got, tc.wantClient, captured.Header)
			}
			if got := captured.Header.Get("x-session-id"); got != tc.wantX {
				t.Fatalf("x-session-id=%q, want %q; headers=%#v", got, tc.wantX, captured.Header)
			}
			if got := captured.Body["prompt_cache_key"]; got != "session-123" {
				t.Fatalf("prompt_cache_key=%#v, want session-123", got)
			}
		})
	}
}

func TestOpenAIResponsesCacheRetentionNoneOmitsGeneratedAffinityHeaders(t *testing.T) {
	model := &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses}
	captured := captureResponsesRequest(t, model, &goai.StreamOptions{SessionID: "session-123", CacheRetention: goai.CacheRetentionNone})
	for _, name := range []string{"session-id", "session_id", "x-client-request-id", "x-session-id"} {
		if len(captured.Header.Values(name)) != 0 {
			t.Fatalf("none retention generated %s: %#v", name, captured.Header)
		}
	}
	if _, ok := captured.Body["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key should be omitted when cacheRetention none: %#v", captured.Body)
	}
}

func TestOpenAIResponsesExplicitHeadersOverrideGeneratedAffinityHeaders(t *testing.T) {
	model := &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses}
	captured := captureResponsesRequest(t, model, &goai.StreamOptions{
		SessionID: "session-123",
		Headers: map[string]string{
			"session-id":          "override-session",
			"x-client-request-id": "override-request",
		},
	})
	if got := captured.Header.Get("session-id"); got != "override-session" {
		t.Fatalf("session-id=%q, want override-session", got)
	}
	if got := captured.Header.Get("x-client-request-id"); got != "override-request" {
		t.Fatalf("x-client-request-id=%q, want override-request", got)
	}
}

func TestOpenAIResponsesRequiredToolChoiceSerializes(t *testing.T) {
	model := &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}, Tools: []goai.Tool{{Name: "ping", Description: "Ping", Parameters: json.RawMessage(`{"type":"object"}`)}}}, &goai.StreamOptions{ToolChoice: goai.ToolChoiceRequired})
	if req.ToolChoice != "required" {
		t.Fatalf("tool_choice=%q, want required", req.ToolChoice)
	}
}

func TestOpenAIResponsesServiceTierCostMultipliers(t *testing.T) {
	for _, tc := range []struct {
		modelID     string
		serviceTier string
		multiplier  float64
	}{
		{"gpt-5.4", "priority", 2},
		{"gpt-5.5", "priority", 2.5},
		{"gpt-5.5", "flex", 0.5},
	} {
		t.Run(tc.modelID+"/"+tc.serviceTier, func(t *testing.T) {
			model := &goai.Model{ID: tc.modelID, Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, Cost: goai.ModelCost{Input: 2.5, Output: 15, CacheRead: 0.25, CacheWrite: 3}}
			body := strings.NewReader(`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","service_tier":"` + tc.serviceTier + `","usage":{"input_tokens":400000,"output_tokens":100000,"total_tokens":500000,"input_tokens_details":{"cached_tokens":100000,"cache_write_tokens":100000}}}}` + "\n\n")
			ch := make(chan goai.Event, 8)
			processStreamWithOptions(body, model, nil, goai.NewAssistantEventSender(ch))
			close(ch)
			var done *goai.DoneEvent
			for ev := range ch {
				if d, ok := ev.(*goai.DoneEvent); ok {
					done = d
				}
			}
			if done == nil || done.Message == nil || done.Message.Usage == nil {
				t.Fatalf("missing done usage: %#v", done)
			}
			wantInput := 200000.0 * model.Cost.Input / 1_000_000.0 * tc.multiplier
			wantOutput := 100000.0 * model.Cost.Output / 1_000_000.0 * tc.multiplier
			wantCacheRead := 100000.0 * model.Cost.CacheRead / 1_000_000.0 * tc.multiplier
			wantCacheWrite := 100000.0 * model.Cost.CacheWrite / 1_000_000.0 * tc.multiplier
			cost := done.Message.Usage.Cost
			if cost.Input != wantInput || cost.Output != wantOutput || cost.CacheRead != wantCacheRead || cost.CacheWrite != wantCacheWrite {
				t.Fatalf("cost=%#v, want input=%v output=%v cacheRead=%v cacheWrite=%v", cost, wantInput, wantOutput, wantCacheRead, wantCacheWrite)
			}
			if cost.Total != cost.Input+cost.Output+cost.CacheRead+cost.CacheWrite {
				t.Fatalf("total=%v does not match components %#v", cost.Total, cost)
			}
		})
	}
}

func TestOpenAIResponsesOffReasoningNoneMatrix(t *testing.T) {
	offNone := "none"
	for _, tc := range []struct {
		name          string
		model         *goai.Model
		wantReasoning bool
		wantEffort    string
	}{
		{name: "off mapped emits none", model: &goai.Model{ID: "gpt-5.4", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: &offNone}}, wantReasoning: true, wantEffort: "none"},
		{name: "off unsupported omits reasoning", model: &goai.Model{ID: "gpt-5", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: nil}}},
		{name: "copilot default omits reasoning", model: &goai.Model{ID: "gpt-5.4", Provider: goai.ProviderGitHubCopilot, Api: goai.ApiOpenAIResponses, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: &offNone}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := buildRequest(tc.model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, nil)
			if !tc.wantReasoning {
				if req.Reasoning != nil {
					t.Fatalf("reasoning=%#v, want omitted", req.Reasoning)
				}
				return
			}
			if req.Reasoning == nil || req.Reasoning.Effort != tc.wantEffort {
				t.Fatalf("reasoning=%#v, want effort %q", req.Reasoning, tc.wantEffort)
			}
		})
	}
}

func TestXAIResponsesAlwaysRequestsEncryptedReasoningInclude(t *testing.T) {
	model := &goai.Model{ID: "grok-4.5", Provider: goai.ProviderXAI, Api: goai.ApiOpenAIResponses, Reasoning: true, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsLongCacheRetention: boolPtr(false)}}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, nil)
	if len(req.Include) != 1 || req.Include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include=%#v, want encrypted reasoning include", req.Include)
	}
}

func boolPtr(v bool) *bool { return &v }

// The pinned compatibility file observes payloads even if a bare [DONE]
// sentinel terminates without a response.completed event.
func TestSourceV160ResponsesCompatibilityWire(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, tc := range []struct {
		provider goai.Provider
		id, mode string
	}{
		{goai.ProviderCloudflareAIGateway, "gpt-5.6-sol", "strict"},
		{goai.ProviderGitHubCopilot, "gpt-5.3-codex", "strict"},
		{goai.ProviderGitHubCopilot, "mai-code-2", "strict"},
		{goai.ProviderGitHubCopilot, "grok-4.7", "strict"},
		{goai.ProviderOpenAI, "gpt-5.6-luna", "required"},
		{goai.ProviderGitHubCopilot, "gpt-6-sol", "omit-reasoning"},
		{goai.ProviderOpenAI, "gpt-5.4", "affinity-official"},
		{goai.ProviderOpenAI, "gpt-5.4", "affinity-proxy"},
		{goai.ProviderOpenAI, "gpt-5.4", "affinity-none"},
		{goai.ProviderOpenAI, "gpt-5.4", "affinity-override"},
	} {
		t.Run(tc.mode+"/"+string(tc.provider)+"/"+tc.id, func(t *testing.T) {
			catalog := goai.GetModel(tc.provider, tc.id)
			if catalog == nil || catalog.Api != goai.ApiOpenAIResponses {
				t.Fatal("missing pinned Responses model")
			}
			model := *catalog
			var requests atomic.Int32
			type receipt struct {
				body   map[string]json.RawMessage
				header http.Header
			}
			received := make(chan receipt, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) != 1 {
					t.Error("unexpected additional Responses request")
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				if r.Method != http.MethodPost || r.URL.Path != "/responses" {
					t.Errorf("Responses method and path = %s %s; want POST /responses", r.Method, r.URL.Path)
				}
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				received <- receipt{body: body, header: r.Header.Clone()}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
			}))
			defer server.Close()
			model.BaseURL = server.URL
			input := &goai.Context{Messages: []goai.Message{goai.UserMessage("Use a tool.")}}
			opts := &goai.StreamOptions{APIKey: "sk-test-key"}
			switch tc.mode {
			case "strict":
				if model.ResponsesCompat == nil || model.ResponsesCompat.SupportsStrictMode == nil || !*model.ResponsesCompat.SupportsStrictMode {
					t.Error("public model does not advertise strict support")
				}
				input.Tools = []goai.Tool{
					{Name: "ordinary", Description: "An ordinary tool", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"number"}},"required":["path"]}`)},
					{Name: "constrained", Description: "A constrained tool", Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"}},
				}
			case "required":
				input.Messages[0] = goai.UserMessage("Do not call ping. Respond with text instead.")
				input.Tools = []goai.Tool{{Name: "ping", Description: "Ping", Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)}}
				opts.ToolChoice = goai.ToolChoiceRequired
			case "omit-reasoning":
				input.SystemPrompt = "sys"
				input.Messages[0] = goai.UserMessage("hi")
			default:
				input.SystemPrompt = "sys"
				input.Messages[0] = goai.UserMessage("hi")
				opts.SessionID = "session-123"
				switch tc.mode {
				case "affinity-proxy":
					model.Provider = goai.Provider("opencode")
				case "affinity-none":
					opts.CacheRetention = goai.CacheRetentionNone
				case "affinity-override":
					opts.Headers = map[string]string{"session-id": "override-session", "x-client-request-id": "override-request"}
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var terminalErr error
			for event := range goai.Stream(ctx, &model, input, opts) {
				if e, ok := event.(*goai.ErrorEvent); ok {
					terminalErr = e.Err
				}
			}
			if requests.Load() != 1 {
				t.Fatalf("Responses request count = %d; want 1 (terminal error: %v)", requests.Load(), terminalErr)
			}
			var captured receipt
			select {
			case captured = <-received:
			case <-ctx.Done():
				t.Fatal("missing decoded Responses body")
			}
			body := captured.body
			switch tc.mode {
			case "strict":
				var tools []map[string]json.RawMessage
				if err := json.Unmarshal(body["tools"], &tools); err != nil || len(tools) != 2 {
					t.Fatalf("Responses tools = %s; want two ordered declarations", body["tools"])
				}
				if string(tools[0]["name"]) != `"ordinary"` || string(tools[0]["strict"]) != "false" || string(tools[1]["name"]) != `"constrained"` || string(tools[1]["strict"]) != "true" {
					t.Errorf("Responses tools = %s; want ordinary strict:false and constrained strict:true", body["tools"])
				}
				// Supplemental: leaving strict off must not make offset required.
				var ordinary struct{ Required []string }
				if err := json.Unmarshal(tools[0]["parameters"], &ordinary); err != nil || len(ordinary.Required) != 1 || ordinary.Required[0] != "path" {
					t.Errorf("ordinary parameters = %s; want only path required", tools[0]["parameters"])
				}
			case "required":
				var items []struct {
					Type  string
					Tools []struct{ Name string }
				}
				if err := json.Unmarshal(body["input"], &items); err != nil || string(body["tool_choice"]) != `"required"` || len(items) != 2 || items[0].Type != "additional_tools" || len(items[0].Tools) != 1 || items[0].Tools[0].Name != "ping" {
					t.Errorf("tool choice = %s, input = %s; want required and exactly two items beginning with additional_tools/ping", body["tool_choice"], body["input"])
				}
			case "omit-reasoning":
				if _, present := body["reasoning"]; present {
					t.Errorf("Copilot reasoning = %s; want omitted", body["reasoning"])
				}
			default:
				wantSession, wantRequest := "session-123", "session-123"
				if tc.mode == "affinity-none" {
					wantSession, wantRequest = "", ""
				} else if tc.mode == "affinity-override" {
					wantSession, wantRequest = "override-session", "override-request"
				}
				for key, want := range map[string]string{"session-id": wantSession, "x-client-request-id": wantRequest, "session_id": ""} {
					got := captured.header.Values(key)
					if (want == "" && len(got) != 0) || (want != "" && (len(got) != 1 || got[0] != want)) {
						t.Errorf("Responses header %s = %v; want %q (empty means absent)", key, got, want)
					}
				}
			}
		})
	}
}

// The published URL and provider remain unmodified at request construction,
// except for the precise proxy clones used by the source fixtures. The test
// redirects every HTTPS dial to its trusted local server.
func TestSourceV160ResponsesRemainingDefaultsWire(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("NO_PROXY", "*")
	type receipt struct {
		body               map[string]json.RawMessage
		header             http.Header
		host, path, method string
	}
	wireRequests := make(chan receipt, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		select {
		case wireRequests <- receipt{body: body, header: r.Header.Clone(), host: r.Host, path: r.URL.Path, method: r.Method}:
		default:
			t.Error("unexpected extra Responses request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	transport := server.Client().Transport.(*http.Transport).Clone()
	tlsDialer := tls.Dialer{Config: transport.TLSClientConfig.Clone()}
	transport.DialTLSContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return tlsDialer.DialContext(ctx, network, server.Listener.Addr().String())
	}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() {
		http.DefaultTransport = previousTransport
		transport.CloseIdleConnections()
	}()

	type fixture struct {
		mode, id string
	}
	fixtures := []fixture{
		{"router-explicit", "gpt-5.4"}, {"router-detected", "gpt-5.4"},
		{"no-session-explicit", "gpt-5.4"}, {"no-session-opencode", "gpt-5.4"}, {"no-session-opencode-proxy", "gpt-5.4"},
		{"clamped-key", "gpt-5.4"}, {"cap-default", "gpt-5.4"}, {"cap-opt-out", "gpt-5.4"},
	}
	for _, id := range []string{"gpt-5.1", "gpt-5.2", "gpt-5.3-codex", "gpt-5.4", "gpt-5.4-mini", "gpt-5.4-nano", "gpt-5.5", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-6-sol", "gpt-6-luna"} {
		fixtures = append(fixtures, fixture{"off-none", id})
	}
	for _, id := range []string{"gpt-5", "gpt-5-mini", "gpt-5-nano", "gpt-5-pro", "gpt-5.2-pro", "gpt-5.4-pro", "gpt-5.5-pro"} {
		fixtures = append(fixtures, fixture{"off-unsupported", id})
	}
	for _, tc := range fixtures {
		t.Run(tc.mode+"/"+tc.id, func(t *testing.T) {
			provider := goai.ProviderOpenAI
			if tc.mode == "no-session-opencode" {
				provider = goai.ProviderOpenCode
			}
			catalog := goai.GetModel(provider, tc.id)
			if catalog == nil || catalog.Api != goai.ApiOpenAIResponses {
				t.Fatal("missing pinned Responses model")
			}
			model := *catalog
			opts := &goai.StreamOptions{APIKey: "sk-test-key"}
			wantRouter, wantClient := "", ""
			cap := 1024
			switch tc.mode {
			case "router-explicit":
				model.Provider, model.BaseURL = goai.Provider("proxy"), "https://proxy.example.com/v1"
				model.ResponsesCompat = &goai.OpenAIResponsesCompat{SessionAffinityFormat: "openrouter"}
				opts.SessionID, wantRouter = "session-proxy", "session-proxy"
			case "router-detected":
				model.Provider, model.BaseURL = goai.ProviderOpenRouter, "https://openrouter.ai/api/v1"
				opts.SessionID, wantRouter = "session-openrouter", "session-openrouter"
			case "no-session-explicit":
				model.Provider, model.BaseURL = goai.Provider("proxy"), "https://proxy.example.com/v1"
				model.ResponsesCompat = &goai.OpenAIResponsesCompat{SessionAffinityFormat: "openai-nosession"}
				opts.SessionID, wantClient = "session-proxy", "session-proxy"
			case "no-session-opencode":
				if model.ResponsesCompat == nil || model.ResponsesCompat.SessionAffinityFormat != "openai-nosession" {
					t.Error("built-in OpenCode model is missing pinned no-session capability")
				}
				opts.SessionID, wantClient = "session-opencode", "session-opencode"
			case "no-session-opencode-proxy":
				model.Provider, model.BaseURL = goai.ProviderOpenCode, "https://proxy.example.com/v1"
				model.ResponsesCompat = &goai.OpenAIResponsesCompat{SessionAffinityFormat: "openai-nosession"}
				opts.SessionID, wantClient = "session-123", "session-123"
			case "clamped-key":
				opts.SessionID = strings.Repeat("x", 67)
			case "cap-default":
				opts.MaxTokens = &cap
			case "cap-opt-out":
				opts.MaxTokens = &cap
				compat := *model.ResponsesCompat
				no := false
				compat.SupportsMaxOutputTokens = &no
				model.ResponsesCompat = &compat
			}
			endpoint, err := url.Parse(model.BaseURL)
			if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" {
				t.Fatalf("invalid offline Responses fixture endpoint %q", model.BaseURL)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var terminalErr error
			for ev := range goai.Stream(ctx, &model, &goai.Context{SystemPrompt: "sys", Messages: []goai.Message{goai.UserMessage("hi")}}, opts) {
				if e, ok := ev.(*goai.ErrorEvent); ok {
					terminalErr = e.Err
				}
			}
			if len(wireRequests) != 1 {
				t.Fatalf("wire request count = %d; want one (terminal error: %v)", len(wireRequests), terminalErr)
			}
			captured := <-wireRequests
			if captured.host != endpoint.Host || captured.path != strings.TrimRight(endpoint.Path, "/")+"/responses" || captured.method != http.MethodPost {
				t.Errorf("request endpoint = %s %s%s; want POST to original Responses URL", captured.method, captured.host, captured.path)
			}
			body := captured.body
			switch tc.mode {
			case "router-explicit", "router-detected", "no-session-explicit", "no-session-opencode", "no-session-opencode-proxy":
				for key, want := range map[string]string{"session-id": "", "session_id": "", "x-client-request-id": wantClient, "x-session-id": wantRouter} {
					values := captured.header.Values(key)
					if (want == "" && len(values) != 0) || (want != "" && (len(values) != 1 || values[0] != want)) {
						t.Errorf("affinity header %s = %v; want %q (empty means absent)", key, values, want)
					}
				}
				if _, present := body["session_id"]; present || string(body["prompt_cache_key"]) != `"`+opts.SessionID+`"` {
					t.Errorf("affinity payload session_id = %s, prompt_cache_key = %s; want absent and %q", body["session_id"], body["prompt_cache_key"], opts.SessionID)
				}
			case "clamped-key":
				if got := string(body["prompt_cache_key"]); got != `"`+strings.Repeat("x", 64)+`"` {
					t.Errorf("prompt_cache_key = %s; want exactly 64 x characters", got)
				}
			case "off-none":
				if got := string(body["reasoning"]); got != `{"effort":"none"}` {
					t.Errorf("reasoning = %s; want exactly none effort", got)
				}
			case "off-unsupported":
				if _, present := body["reasoning"]; present {
					t.Errorf("reasoning = %s; want absent for unsupported off", body["reasoning"])
				}
			case "cap-default":
				if string(body["max_output_tokens"]) != "1024" {
					t.Errorf("max_output_tokens = %s; want 1024", body["max_output_tokens"])
				}
			case "cap-opt-out":
				if _, present := body["max_output_tokens"]; present {
					t.Errorf("max_output_tokens = %s; want absent for cap opt-out", body["max_output_tokens"])
				}
			}
		})
	}
}
