package goai_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

var llamaSourceServerID atomic.Uint32

func llamaSourceModel() *goai.ClassifierModel {
	return &goai.ClassifierModel{ID: "qwen", Name: "qwen", Api: goai.ClassifierApiLlamaCPP, Provider: "llama.cpp", Type: "classifier",
		BaseURL: fmt.Sprintf("http://127.0.0.1:%d/v1", 20000+llamaSourceServerID.Add(1)), Input: []string{"text"}, ContextWindow: 32768}
}

type llamaSourceRequest struct {
	url, path string
	body      map[string]any
	header    http.Header
}

type llamaSourceServer struct {
	requests []llamaSourceRequest
	next     func(string, int) map[string]float64
	template string
	tokens   func(string) []int
	status   int
}

func (s *llamaSourceServer) RoundTrip(req *http.Request) (*http.Response, error) {
	defer req.Body.Close()
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return nil, err
	}
	target := req.URL.String()
	if !req.URL.IsAbs() { // Requests received by an actual HTTP server have an origin-form target.
		scheme := "http"
		if req.TLS != nil {
			scheme = "https"
		}
		target = scheme + "://" + req.Host + req.URL.RequestURI()
	}
	s.requests = append(s.requests, llamaSourceRequest{target, req.URL.Path, body, req.Header.Clone()})
	status := s.status
	var response any
	if status != 0 {
		response = map[string]any{"error": map[string]string{"message": "context overflow"}}
	} else {
		status = http.StatusOK
		switch {
		case strings.HasSuffix(req.URL.Path, "/tokenize"):
			tokenize := s.tokens
			if tokenize == nil {
				tokenize = llamaSourceChars
			}
			response = map[string]any{"tokens": tokenize(body["content"].(string))}
		case strings.HasSuffix(req.URL.Path, "/apply-template"):
			var prompt strings.Builder
			for _, item := range body["messages"].([]any) {
				message := item.(map[string]any)
				fmt.Fprintf(&prompt, "<|%s|>\n%s\n", message["role"], message["content"])
			}
			prompt.WriteString("<|assistant|>\n")
			text := s.template
			if text == "" {
				text = prompt.String()
			}
			response = map[string]string{"prompt": text}
		case strings.HasSuffix(req.URL.Path, "/completion"):
			next := map[string]float64{"A": -0.1, "B": -2.5}
			if s.next != nil {
				next = s.next(body["prompt"].(string), int(body["n_probs"].(float64)))
			}
			var top []map[string]any
			for token, probability := range next {
				top = append(top, map[string]any{"id": []rune(token)[0], "token": token, "bytes": []int{}, "logprob": probability})
			}
			response = map[string]any{"content": "A", "completion_probabilities": []any{map[string]any{"id": 65, "token": "A", "top_logprobs": top}}}
		default:
			return nil, fmt.Errorf("unexpected request path %s", req.URL.Path)
		}
	}
	data, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(data))), Request: req}, nil
}

func (s *llamaSourceServer) matching(path string) []llamaSourceRequest {
	var out []llamaSourceRequest
	for _, r := range s.requests {
		if strings.HasSuffix(r.path, path) {
			out = append(out, r)
		}
	}
	return out
}

func (s *llamaSourceServer) depths() []int {
	out := []int{}
	for _, r := range s.matching("/completion") {
		out = append(out, int(r.body["n_probs"].(float64)))
	}
	return out
}

func llamaSourceChars(s string) []int {
	out := []int{}
	for _, c := range s {
		out = append(out, int(c))
	}
	return out
}

func llamaSourceWords(content string) []int {
	var out []int
	for i, part := range strings.Split(content, "\n") {
		if i > 0 {
			out = append(out, '\n')
		}
		switch part {
		case "Yes":
			out = append(out, 'Y')
		case "No":
			out = append(out, 'N')
		default:
			out = append(out, llamaSourceChars(part)...)
		}
	}
	return out
}

func llamaSourceContext() goai.ClassifierContext {
	return goai.ClassifierContext{State: map[string]any{"message": "Help! My payouts have been failing for 3 days."}, Questions: map[string]goai.ClassifierQuestion{
		"team":     {Type: "choice", Instructions: "Which team should handle this?", Criteria: map[string]string{"billing": "Payments and refunds", "technical": "Bugs and outages", "sales": ""}},
		"urgent":   {Type: "bool", Instructions: "Does this convey urgency?", Criteria: goai.ClassifierBoolCriteria{True: "The user needs help soon", False: "No time pressure"}},
		"severity": {Type: "score", Instructions: "How severe is this?", Criteria: []string{"low", "medium", "high"}},
	}}
}

func llamaSourceOrder() goai.LlamaCPPOrder {
	return goai.LlamaCPPOrder{
		Questions: []string{"team", "urgent", "severity"},
		Choices:   map[string][]string{"team": {"billing", "technical", "sales"}},
	}
}

func llamaSourcePick(instructions string) goai.ClassifierContext {
	return goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{
		"pick": {Type: "choice", Instructions: instructions, Criteria: map[string]string{"a": "", "b": ""}},
	}}
}

func runLlamaSource(t *testing.T, server *llamaSourceServer, model *goai.ClassifierModel, ctx goai.ClassifierContext, opts goai.ClassifierOptions) *goai.ClassifierResult {
	t.Helper()
	opts.HTTPClient = &http.Client{Transport: server}
	return runLlamaSourceUsing(t, model, ctx, opts, goai.Classify)
}

func runOrderedLlamaSource(t *testing.T, server *llamaSourceServer, model *goai.ClassifierModel, ctx goai.ClassifierContext, order goai.LlamaCPPOrder, opts goai.ClassifierOptions) *goai.ClassifierResult {
	t.Helper()
	opts.HTTPClient = &http.Client{Transport: server}
	return runLlamaSourceUsing(t, model, ctx, opts, func(model *goai.ClassifierModel, ctx goai.ClassifierContext, opts *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
		return goai.ClassifyLlamaCPPWithOrder(model, ctx, order, opts)
	})
}

func runLlamaSourceUsing(t *testing.T, model *goai.ClassifierModel, ctx goai.ClassifierContext, opts goai.ClassifierOptions,
	classify func(*goai.ClassifierModel, goai.ClassifierContext, *goai.ClassifierOptions) (*goai.ClassifierResult, error),
) *goai.ClassifierResult {
	t.Helper()
	if opts.Context == nil {
		opts.Context = t.Context()
	}
	result, err := classify(model, ctx, &opts)
	if err != nil || result == nil {
		t.Fatalf("result=%v err=%v", result, err)
	}
	return result
}

func checkLlamaResult(t *testing.T, result *goai.ClassifierResult, reason goai.StopReason, message string) {
	t.Helper()
	if result.StopReason != reason || (message != "" && !strings.Contains(result.ErrorMessage, message)) || (reason == goai.StopReasonStop && result.ErrorMessage != "") {
		t.Errorf("stop=%s error=%q; want %s with %q", result.StopReason, result.ErrorMessage, reason, message)
	}
}

func checkLlamaFloat(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.IsNaN(got) || math.Abs(got-want) > 1e-12 {
		t.Errorf("%s=%g; want %g", label, got, want)
	}
}

// Source: pi v1.6.0 packages/ai/test/llama-cpp-classify.test.ts:117-398.
func TestSourceV160LlamaCPPClassifier(t *testing.T) {
	t.Run("answers-and-request-fields", func(t *testing.T) { // 117
		server := &llamaSourceServer{tokens: llamaSourceWords, next: func(prompt string, _ int) map[string]float64 {
			switch {
			case strings.Contains(prompt, "Answer Yes or No."):
				return map[string]float64{"Y": -0.05, "N": -3}
			case strings.Contains(prompt, "Answer with one level number."):
				return map[string]float64{"2": -0.2, "1": -1.8, "0": -4}
			default:
				return map[string]float64{"B": -0.3, "A": -1.5, "C": -3}
			}
		}}
		model := llamaSourceModel()
		result := runOrderedLlamaSource(t, server, model, llamaSourceContext(), llamaSourceOrder(), goai.ClassifierOptions{APIKey: "local"})
		checkLlamaResult(t, result, goai.StopReasonStop, "")
		choice, urgent, severity := result.Answers["team"], result.Answers["urgent"], result.Answers["severity"]
		if len(result.Answers) != 3 || choice.Type != "choice" || choice.Choice != "technical" || len(choice.Probabilities) != 3 || urgent.Type != "bool" || severity.Type != "score" {
			t.Errorf("unexpected answer shape: %#v", result.Answers)
		}
		choiceSum := math.Exp(-1.5) + math.Exp(-0.3) + math.Exp(-3)
		for key, logprob := range map[string]float64{"billing": -1.5, "technical": -0.3, "sales": -3} {
			checkLlamaFloat(t, key, choice.Probabilities[key], math.Exp(logprob)/choiceSum)
		}
		checkLlamaFloat(t, "choice confidence", choice.Confidence, (3*math.Exp(-0.3)/choiceSum-1)/2)
		checkLlamaFloat(t, "urgent probability", urgent.Probability, 1/(1+math.Exp(-2.95)))
		scoreSum := math.Exp(-4) + math.Exp(-1.8) + math.Exp(-0.2)
		checkLlamaFloat(t, "severity score", severity.Score, (math.Exp(-1.8)+2*math.Exp(-0.2))/scoreSum)
		checkLlamaFloat(t, "severity confidence", severity.Confidence, (3*math.Exp(-0.2)/scoreSum-1)/2)
		for _, request := range server.requests {
			if !strings.HasPrefix(request.url, strings.TrimSuffix(model.BaseURL, "/v1")+"/") || request.body["model"] != "qwen" || request.header.Get("Authorization") != "Bearer local" {
				t.Errorf("unexpected request: %#v", request)
			}
		}
		if completions, templates := server.matching("/completion"), server.matching("/apply-template"); len(completions) != 3 || len(templates) != 3 {
			t.Fatalf("completion=%d template=%d; want three each", len(completions), len(templates))
		} else {
			for key, want := range map[string]any{"n_predict": float64(1), "n_probs": float64(256), "post_sampling_probs": false, "cache_prompt": true} {
				if got := completions[0].body[key]; got != want {
					t.Errorf("%s=%v; want %v", key, got, want)
				}
			}
			if got := templates[0].body["chat_template_kwargs"]; !reflect.DeepEqual(got, map[string]any{"enable_thinking": false}) {
				t.Errorf("chat_template_kwargs=%v", got)
			}
		}
	})
	t.Run("rendered-question-and-reused-prefix", func(t *testing.T) { // 157, 201
		server := &llamaSourceServer{tokens: llamaSourceWords, next: func(_ string, _ int) map[string]float64 {
			return map[string]float64{"A": -1, "B": -2, "C": -3, "Y": -1, "N": -2, "0": -1, "1": -2, "2": -3}
		}}
		result := runOrderedLlamaSource(t, server, llamaSourceModel(), llamaSourceContext(), llamaSourceOrder(), goai.ClassifierOptions{})
		checkLlamaResult(t, result, goai.StopReasonStop, "")
		state := "State:\n{\n \"message\": \"Help! My payouts have been failing for 3 days.\"\n}"
		team := "Question: Which team should handle this?\n\nOptions:\n- billing: Payments and refunds\n- technical: Bugs and outages\n- sales"
		urgent := "Question: Does this convey urgency?\n\nYes means: The user needs help soon\nNo means: No time pressure"
		severity := "Question: How severe is this?\n\nLevels:\n0. low\n1. medium\n2. high"
		prefix := strings.Join([]string{state, "Task: answer each of the following questions about the state.", team, urgent, severity, state, ""}, "\n\n")
		templates := server.matching("/apply-template")
		if len(templates) != 3 {
			t.Fatalf("template calls=%d", len(templates))
		}
		finals := []string{"Question: Which team should handle this?\n\nOptions:\nA. billing: Payments and refunds\nB. technical: Bugs and outages\nC. sales\n\nAnswer with one letter.", urgent + "\n\nAnswer Yes or No.", severity + "\n\nAnswer with one level number."}
		for i, request := range templates {
			messages := request.body["messages"].([]any)
			got := messages[1].(map[string]any)["content"]
			if got != prefix+finals[i] {
				t.Errorf("question %d content:\n%v\nwant:\n%s", i, got, prefix+finals[i])
			}
		}
	})
	t.Run("temperature-division", func(t *testing.T) { // 216
		server := newLlamaSourceLoopback(t, &llamaSourceServer{}, llamaLoopbackPrefix())
		ctx := llamaSourcePick("Pick one")
		result := runLlamaSourceUsing(t, server.model(), ctx, goai.ClassifierOptions{HTTPClient: server.http.Client(), Temperature: 2}, goai.Classify)
		checkLlamaResult(t, result, goai.StopReasonStop, "")
		probabilities := result.Answers["pick"].Probabilities
		if _, a := probabilities["a"]; !a || len(probabilities) != 2 {
			t.Fatalf("unexpected probability keys: %v", probabilities)
		}
		if _, b := probabilities["b"]; !b {
			t.Fatalf("missing probability for b: %v", probabilities)
		}
		want := 1 / (1 + math.Exp(-1.2))
		checkLlamaFloat(t, "a", probabilities["a"], want)
		checkLlamaFloat(t, "b", probabilities["b"], 1-want)
		assertLlamaSingleReadoutV160(t, server, 2)

		// Public equivalence to the private helper's pre-scaled-input check.
		preScaled := newLlamaSourceLoopback(t, &llamaSourceServer{next: func(string, int) map[string]float64 {
			return map[string]float64{"A": -0.1 / 2, "B": -2.5 / 2}
		}}, llamaLoopbackPrefix())
		unit := preScaled.classify(t, preScaled.model(), ctx)
		checkLlamaResult(t, unit, goai.StopReasonStop, "")
		if !maps.Equal(probabilities, unit.Answers["pick"].Probabilities) {
			t.Errorf("temperature-scaled probabilities = %v, pre-scaled input = %v", probabilities, unit.Answers["pick"].Probabilities)
		}
		assertLlamaSingleReadoutV160(t, preScaled, 2)
	})
	t.Run("explicit-zero-temperature", func(t *testing.T) { // 229
		server := &llamaSourceServer{tokens: llamaSourceWords, next: func(_ string, _ int) map[string]float64 {
			return map[string]float64{"A": -1, "B": -2, "C": -3, "Y": -1, "N": -2, "0": -1, "1": -2, "2": -3}
		}}
		zero := 0.0
		result := runLlamaSource(t, server, llamaSourceModel(), llamaSourceContext(), goai.ClassifierOptions{TemperatureOverride: &zero})
		checkLlamaResult(t, result, goai.StopReasonError, "Temperature must be a positive number, got 0")
		if len(server.requests) != 0 {
			t.Errorf("sent %d requests for invalid temperature", len(server.requests))
		}
	})
	t.Run("readout-escalation", func(t *testing.T) { // 238
		for _, recover := range []bool{true, false} {
			server := &llamaSourceServer{next: func(_ string, depth int) map[string]float64 {
				if recover && depth >= 4096 {
					return map[string]float64{"A": -0.1, "B": -9}
				}
				return map[string]float64{"A": -0.1}
			}}
			result := runLlamaSource(t, server, llamaSourceModel(), llamaSourcePick("Pick one"), goai.ClassifierOptions{})
			want := []int{256, 4096}
			if recover {
				checkLlamaResult(t, result, goai.StopReasonStop, "")
			} else {
				checkLlamaResult(t, result, goai.StopReasonError, "did not rank labels B for pick within the top 32768 tokens")
				if len(result.Answers) != 0 {
					t.Errorf("invented answer for missing label: %v", result.Answers)
				}
				want = append(want, 32768)
			}
			if got := server.depths(); !reflect.DeepEqual(got, want) {
				t.Errorf("depths=%v; want %v", got, want)
			}
		}
	})
	t.Run("close-thinking", func(t *testing.T) { // 257
		server := newLlamaSourceLoopback(t, &llamaSourceServer{template: "<|assistant|>\n<think>"}, llamaLoopbackPrefix())
		server.classify(t, server.model(), llamaSourcePick("Pick"))
		if requests := server.matching("/completion"); len(requests) != 1 || requests[0].body["prompt"] != "<|assistant|>\n<think></think>" {
			t.Errorf("completed prompt: %#v", requests)
		}
	})
	t.Run("single-token-labels", func(t *testing.T) { // 269
		merging := newLlamaSourceLoopback(t, &llamaSourceServer{tokens: func(content string) []int {
			if strings.HasPrefix(content, "\n") && len(content) > 1 {
				return []int{1000}
			}
			return llamaSourceChars(content)
		}}, llamaLoopbackPrefix())
		checkLlamaResult(t, merging.classify(t, merging.model(), llamaSourcePick("Pick")), goai.StopReasonStop, "")
		assertLlamaLoopbackTokenInputs(t, merging.matching("/tokenize"), "qwen", []string{"\n", "\n", "\nA", "\nB", "A", "B"})
		split := newLlamaSourceLoopback(t, &llamaSourceServer{}, llamaLoopbackPrefix())
		checkLlamaResult(t, split.classify(t, split.model(), llamaSourceBool()), goai.StopReasonError, `Label "Yes" is not a single token for qwen`)
	})
	t.Run("token-cache", func(t *testing.T) { // 292
		server := newLlamaSourceLoopback(t, &llamaSourceServer{}, llamaLoopbackPrefix())
		model, ctx := server.model(), llamaSourcePick("Pick")
		server.classify(t, model, ctx)
		first := len(server.matching("/tokenize"))
		server.classify(t, model, ctx)
		if total := len(server.matching("/tokenize")); first == 0 || total != first {
			t.Errorf("tokenizations: first=%d total=%d", first, total)
		}
	})
	t.Run("option-bounds", func(t *testing.T) { // 306
		server := &llamaSourceServer{}
		wire := newLlamaSourceLoopback(t, server, llamaLoopbackPrefix())
		ctx := llamaSourcePick("Pick")
		criteria := map[string]string{}
		for i := range 63 {
			criteria[fmt.Sprintf("option%d", i)] = ""
		}
		ctx.Questions["pick"] = goai.ClassifierQuestion{Type: "choice", Instructions: "Pick", Criteria: criteria}
		checkLlamaResult(t, wire.classify(t, wire.model(), ctx), goai.StopReasonError, "A choice question needs 2 to 62 options, got 63")
		ctx.Questions = map[string]goai.ClassifierQuestion{"rate": {Type: "score", Instructions: "Rate", Criteria: []string{"only"}}}
		checkLlamaResult(t, wire.classify(t, wire.model(), ctx), goai.StopReasonError, "A score question needs 2 to 10 levels, got 1")
		wire.mu.Lock()
		defer wire.mu.Unlock()
		if len(server.requests) != 0 {
			t.Errorf("sent requests before validating bounds: %#v", server.requests)
		}
	})
	t.Run("request-hooks", func(t *testing.T) { // 325
		const prompt = "<|assistant|>\n"
		server := newLlamaSourceLoopback(t, &llamaSourceServer{template: prompt}, llamaLoopbackPrefix())
		var payloads []map[string]any
		var statuses []int
		result := runLlamaSourceUsing(t, server.model(), llamaSourcePick("Pick"), goai.ClassifierOptions{
			HTTPClient: server.http.Client(),
			OnPayload: func(payload map[string]any, _ *goai.ClassifierModel) (map[string]any, error) {
				payloads = append(payloads, maps.Clone(payload))
				if prior := len(server.matching("/completion")); prior != 0 {
					t.Errorf("completion requests before OnPayload = %d, want 0", prior)
				}
				out := maps.Clone(payload)
				out["id_slot"] = 1
				return out, nil
			},
			OnResponse: func(meta goai.ClassifierResponseMetadata, _ *goai.ClassifierModel) error {
				statuses = append(statuses, meta.Status)
				if prior := len(server.matching("/completion")); prior != 1 {
					t.Errorf("completion requests before OnResponse = %d, want 1", prior)
				}
				return nil
			},
		}, goai.Classify)
		checkLlamaResult(t, result, goai.StopReasonStop, "")
		if len(payloads) != 1 {
			t.Fatalf("OnPayload calls = %d, want 1", len(payloads))
		}
		wantPayload := map[string]any{"model": "qwen", "prompt": prompt, "n_predict": 1, "n_probs": 256, "post_sampling_probs": false, "cache_prompt": true, "temperature": 0}
		if payloads[0]["n_predict"] != 1 || !reflect.DeepEqual(payloads[0], wantPayload) {
			t.Errorf("OnPayload value = %#v, want %#v", payloads[0], wantPayload)
		}
		if !slices.Equal(statuses, []int{200}) {
			t.Errorf("OnResponse statuses = %v, want [200]", statuses)
		}
		completions := server.matching("/completion")
		if len(completions) != 1 || completions[0].body["id_slot"] != float64(1) {
			t.Fatalf("completion requests missing transformed id_slot: %#v", completions)
		}
		wantWire := map[string]any{"model": "qwen", "prompt": prompt, "n_predict": float64(1), "n_probs": float64(256), "post_sampling_probs": false, "cache_prompt": true, "temperature": float64(0), "id_slot": float64(1)}
		if !reflect.DeepEqual(completions[0].body, wantWire) {
			t.Errorf("completion wire body = %#v, want %#v", completions[0].body, wantWire)
		}
		for _, path := range []string{"/tokenize", "/apply-template"} {
			for _, req := range server.matching(path) {
				if _, leaked := req.body["id_slot"]; leaked {
					t.Errorf("completion hook changed %s body: %#v", path, req.body)
				}
			}
		}
		if requests, tokens, templates := len(server.matching("")), len(server.matching("/tokenize")), len(server.matching("/apply-template")); requests != 6 || tokens != 4 || templates != 1 {
			t.Errorf("requests=%d tokenize=%d templates=%d, want 6, 4, 1", requests, tokens, templates)
		}
	})
	t.Run("server-error-and-cancel", func(t *testing.T) { // 352
		zero := 0
		server := newLlamaSourceLoopback(t, &llamaSourceServer{status: http.StatusBadRequest}, llamaLoopbackPrefix())
		result := runLlamaSourceUsing(t, server.model(), llamaSourceContext(), goai.ClassifierOptions{HTTPClient: server.http.Client(), MaxRetriesOverride: &zero}, goai.Classify)
		checkLlamaResult(t, result, goai.StopReasonError, "llama.cpp error (400)")
		const want = `llama.cpp error (400): {"error":{"message":"context overflow"}}`
		if !strings.Contains(result.ErrorMessage, "context overflow") || result.ErrorMessage != want {
			t.Errorf("server error = %q, want %q", result.ErrorMessage, want)
		}
		if requests := server.matching(""); len(requests) != 1 || requests[0].path != server.prefix+"/tokenize" {
			t.Errorf("400 reached unexpected endpoint or retried: %#v", requests)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		aborted := newLlamaSourceLoopback(t, &llamaSourceServer{}, llamaLoopbackPrefix())
		checkLlamaResult(t, runLlamaSourceUsing(t, aborted.model(), llamaSourceContext(), goai.ClassifierOptions{Context: ctx, HTTPClient: aborted.http.Client()}, goai.Classify), goai.StopReasonAborted, "")
		if requests := aborted.matching(""); len(requests) != 0 {
			t.Errorf("already-canceled request reached the server: %#v", requests)
		}
	})
	t.Run("other-api", func(t *testing.T) { // 373
		// The original calls the llama.cpp provider directly, not dispatch.
		server := &llamaSourceServer{}
		wire := newLlamaSourceLoopback(t, server, llamaLoopbackPrefix())
		model := wire.model()
		model.Api = goai.ClassifierApiTypeSafeSystemOne
		provider := goai.GetClassifierApiProvider(goai.ClassifierApiLlamaCPP)
		result, err := provider.Classify(model, llamaSourceContext(), &goai.ClassifierOptions{HTTPClient: wire.http.Client()})
		wire.mu.Lock()
		defer wire.mu.Unlock()
		if err != nil || len(server.requests) != 0 {
			t.Fatalf("err=%v requests=%d", err, len(server.requests))
		}
		checkLlamaResult(t, result, goai.StopReasonError, "Unsupported classifier API: typesafe-system-one")
	})
	t.Run("base-urls", func(t *testing.T) { // 381
		for _, tc := range []struct {
			name, suffix, rootSuffix string
			listen                   func(http.Handler) *httptest.Server
		}{
			{"trailing-v1", "/v1/", "", httptest.NewServer},
			{"tls-prefix-v1", "/prefix/v1", "/prefix", httptest.NewTLSServer},
			{"no-version", "", "", httptest.NewServer},
		} {
			t.Run(tc.name, func(t *testing.T) {
				server := newLlamaSourceLoopbackUsing(t, &llamaSourceServer{}, tc.rootSuffix, tc.listen)
				model := server.model()
				model.BaseURL = server.http.URL + tc.suffix
				// Keep the paths exact even if a repeated run reuses a server port.
				model.ID = fmt.Sprintf("url-fixture-%d", llamaSourceServerID.Add(1))
				checkLlamaResult(t, server.classify(t, model, llamaSourcePick("Pick")), goai.StopReasonStop, "")
				got := map[string]int{}
				for _, req := range server.matching("") {
					got[req.url]++
				}
				root := server.http.URL + tc.rootSuffix
				want := map[string]int{root + "/tokenize": 4, root + "/apply-template": 1, root + "/completion": 1}
				if !maps.Equal(got, want) {
					t.Errorf("base %s: wire targets=%v, want %v", model.BaseURL, got, want)
				}
			})
		}
	})
	t.Run("confidence-and-expected-score", func(t *testing.T) { // 387
		for _, tc := range []struct {
			name, labels string
			probs        []float64
			confidence   float64
		}{
			{"three-choices", "ABC", []float64{.89, .06, .05}, .835},
			{"uniform", "AB", []float64{.5, .5}, 0},
			{"certain", "ABC", []float64{1, 0, 0}, 1},
		} {
			t.Run(tc.name, func(t *testing.T) {
				server := llamaSourceProbabilityLoopback(t, tc.labels, tc.probs)
				criteria := map[string]string{}
				for i := range tc.probs {
					criteria[string("abc"[i])] = ""
				}
				ctx := llamaSourcePick("")
				ctx.Questions["pick"] = goai.ClassifierQuestion{Type: "choice", Criteria: criteria}
				result := server.classify(t, server.model(), ctx)
				checkLlamaResult(t, result, goai.StopReasonStop, "")
				answer := result.Answers["pick"]
				if answer.Type != "choice" {
					t.Fatalf("answer=%#v, want choice", answer)
				}
				if tc.confidence == 0 || tc.confidence == 1 {
					if answer.Confidence != tc.confidence {
						t.Errorf("confidence = %g, want exactly %g", answer.Confidence, tc.confidence)
					}
				} else {
					checkLlamaFloat(t, "confidence", answer.Confidence, tc.confidence)
				}
				if fields := llamaSourceAnswerJSON(t, answer); len(fields) != 4 || fields["type"] != "choice" || fields["choice"] != "a" || fields["probabilities"] == nil || fields["confidence"] != answer.Confidence {
					t.Errorf("choice JSON with no score = %v", fields)
				}
				assertLlamaSingleReadoutV160(t, server, len(tc.labels))
			})
		}
		t.Run("expected-score", func(t *testing.T) {
			server := llamaSourceProbabilityLoopback(t, "012", []float64{.2, .3, .5})
			fixture := goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{"q": {Type: "score", Criteria: []string{"a", "b", "c"}}}}
			result := server.classify(t, server.model(), fixture)
			checkLlamaResult(t, result, goai.StopReasonStop, "")
			answer := result.Answers["q"]
			checkLlamaFloat(t, "score", answer.Score, 1.3)
			checkLlamaFloat(t, "confidence", answer.Confidence, .25)
			if fields := llamaSourceAnswerJSON(t, answer); len(fields) != 3 || fields["type"] != "score" || fields["score"] != answer.Score || fields["confidence"] != answer.Confidence {
				t.Errorf("score JSON = %v; want type, score and confidence only", fields)
			}
			assertLlamaSingleReadoutV160(t, server, 3)
		})
	})
}

// Supplemental: ordered choices do not depend on particular source labels,
// and the existing Classify path still sorts ordinary maps.
func TestLlamaCPPExplicitChoiceOrderV160(t *testing.T) {
	ctx := goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{
		"pick": {Type: "choice", Instructions: "Pick one", Criteria: map[string]string{"later": "first by intent", "earlier": ""}},
	}}
	for _, tc := range []struct {
		name, first, last string
		ordered           bool
	}{
		{"map-default", "earlier", "later: first by intent", false},
		{"explicit", "later: first by intent", "earlier", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := &llamaSourceServer{next: func(_ string, _ int) map[string]float64 { return map[string]float64{"A": -0.1, "B": -3} }}
			var result *goai.ClassifierResult
			if tc.ordered {
				order := goai.LlamaCPPOrder{Choices: map[string][]string{"pick": {"later", "earlier"}}}
				result = runOrderedLlamaSource(t, server, llamaSourceModel(), ctx, order, goai.ClassifierOptions{})
			} else {
				result = runLlamaSource(t, server, llamaSourceModel(), ctx, goai.ClassifierOptions{})
			}
			checkLlamaResult(t, result, goai.StopReasonStop, "")
			wantChoice, _, _ := strings.Cut(tc.first, ":")
			if got := result.Answers["pick"].Choice; got != wantChoice {
				t.Errorf("selected choice = %q, want %q", got, wantChoice)
			}
			templates := server.matching("/apply-template")
			if len(templates) != 1 {
				t.Fatalf("template requests = %d, want 1", len(templates))
			}
			messages := templates[0].body["messages"].([]any)
			got := messages[1].(map[string]any)["content"]
			want := "State:\n{}\n\nTask: answer the following question about the state.\n\nQuestion: Pick one\n\nOptions:\n- " + tc.first + "\n- " + tc.last + "\n\nState:\n{}\n\nQuestion: Pick one\n\nOptions:\nA. " + tc.first + "\nB. " + tc.last + "\n\nAnswer with one letter."
			if got != want {
				t.Errorf("question content:\n%v\nwant:\n%s", got, want)
			}
		})
	}
}

func TestLlamaCPPRejectsMalformedOrderV160(t *testing.T) {
	for _, tc := range []struct {
		name, err string
		order     goai.LlamaCPPOrder
	}{
		{"missing question", "invalid llama.cpp question order: expected 3 keys, got 2", goai.LlamaCPPOrder{Questions: []string{"team", "urgent"}}},
		{"duplicate question", `invalid llama.cpp question order: duplicate key "team"`, goai.LlamaCPPOrder{Questions: []string{"team", "urgent", "team"}}},
		{"unknown question", `invalid llama.cpp question order: unknown key "other"`, goai.LlamaCPPOrder{Questions: []string{"team", "urgent", "other"}}},
		{"missing choice", `invalid llama.cpp choice order for "team": expected 3 keys, got 2`, goai.LlamaCPPOrder{Choices: map[string][]string{"team": {"billing", "technical"}}}},
		{"duplicate choice", `invalid llama.cpp choice order for "team": duplicate key "billing"`, goai.LlamaCPPOrder{Choices: map[string][]string{"team": {"billing", "technical", "billing"}}}},
		{"unknown choice", `invalid llama.cpp choice order for "team": unknown key "other"`, goai.LlamaCPPOrder{Choices: map[string][]string{"team": {"billing", "technical", "other"}}}},
		{"non-choice override", `llama.cpp choice order references non-choice question "urgent"`, goai.LlamaCPPOrder{Choices: map[string][]string{"urgent": {"true", "false"}}}},
		{"nil choice override", `invalid llama.cpp choice order for "team": missing keys`, goai.LlamaCPPOrder{Choices: map[string][]string{"team": nil}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := &llamaSourceServer{}
			result := runOrderedLlamaSource(t, server, llamaSourceModel(), llamaSourceContext(), tc.order, goai.ClassifierOptions{})
			if result.StopReason != goai.StopReasonError || result.ErrorMessage != tc.err || len(result.Answers) != 0 || len(server.requests) != 0 {
				t.Fatalf("result=%+v requests=%d; want pre-transport error %q", result, len(server.requests), tc.err)
			}
		})
	}
}

// Use real HTTP without a provider process. The prefix keeps the process-wide
// cache isolated even if the OS reuses a listener port between test repeats.
func llamaLoopbackPrefix() string {
	return fmt.Sprintf("/r36-%d", llamaSourceServerID.Add(1))
}

type llamaSourceLoopback struct {
	mu     sync.Mutex
	fake   *llamaSourceServer
	http   *httptest.Server
	prefix string
}

func newLlamaSourceLoopback(t *testing.T, fake *llamaSourceServer, prefix string) *llamaSourceLoopback {
	t.Helper()
	return newLlamaSourceLoopbackUsing(t, fake, prefix, httptest.NewServer)
}

func newLlamaSourceLoopbackUsing(t *testing.T, fake *llamaSourceServer, prefix string, listen func(http.Handler) *httptest.Server) *llamaSourceLoopback {
	t.Helper()
	s := &llamaSourceLoopback{fake: fake, prefix: prefix}
	s.http = listen(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, prefix+"/") {
			t.Errorf("unexpected classifier request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		resp, err := fake.RoundTrip(r)
		s.mu.Unlock()
		if err != nil {
			t.Error(err)
			http.Error(w, "fixture error", http.StatusBadRequest)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(resp.StatusCode)
		if _, err := io.Copy(w, resp.Body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(s.http.Close)
	return s
}

func (s *llamaSourceLoopback) model() *goai.ClassifierModel {
	model := llamaSourceModel()
	model.BaseURL = s.http.URL + s.prefix + "/v1"
	return model
}

func (s *llamaSourceLoopback) classify(t *testing.T, model *goai.ClassifierModel, ctx goai.ClassifierContext) *goai.ClassifierResult {
	t.Helper()
	return runLlamaSourceUsing(t, model, ctx, goai.ClassifierOptions{HTTPClient: s.http.Client()}, goai.Classify)
}

func (s *llamaSourceLoopback) matching(path string) []llamaSourceRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fake.matching(path)
}

func llamaSourceBool() goai.ClassifierContext {
	return goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{
		"ok": {Type: "bool", Instructions: "OK?", Criteria: goai.ClassifierBoolCriteria{}},
	}}
}

func assertLlamaLoopbackTokenInputs(t *testing.T, requests []llamaSourceRequest, model string, want []string) {
	t.Helper()
	got := make([]string, 0, len(requests))
	for _, req := range requests {
		if req.body["model"] != model || req.body["add_special"] != false || req.body["parse_special"] != false || len(req.body) != 4 {
			t.Errorf("wrong tokenize payload for %q: %v", model, req.body)
		}
		content, ok := req.body["content"].(string)
		if !ok {
			t.Errorf("non-string tokenization input: %v", req.body["content"])
		}
		got = append(got, content)
	}
	expected := slices.Clone(want)
	slices.Sort(got)
	slices.Sort(expected)
	if !slices.Equal(got, expected) {
		t.Errorf("tokenization inputs for %q = %q, want %q", model, got, expected)
	}
}

func TestLlamaCPPTokenCacheIsolationV160(t *testing.T) {
	prefix := llamaLoopbackPrefix()
	first := newLlamaSourceLoopback(t, &llamaSourceServer{}, prefix)
	second := newLlamaSourceLoopback(t, &llamaSourceServer{}, prefix)
	fixture := llamaSourcePick("Pick")
	model := first.model()
	inputs := []string{"\n", "\n", "\nA", "\nB"}
	checkLlamaResult(t, first.classify(t, model, fixture), goai.StopReasonStop, "")
	firstTokens := first.matching("/tokenize")
	assertLlamaLoopbackTokenInputs(t, firstTokens, model.ID, inputs)

	sameRoot := *model
	sameRoot.BaseURL = strings.TrimSuffix(model.BaseURL, "/v1")
	checkLlamaResult(t, first.classify(t, &sameRoot, fixture), goai.StopReasonStop, "")
	if got := len(first.matching("/tokenize")); got != len(firstTokens) {
		t.Fatalf("same normalized root re-tokenized: got %d, want %d", got, len(firstTokens))
	}

	otherModel := *model
	otherModel.ID = "other-model"
	checkLlamaResult(t, first.classify(t, &otherModel, fixture), goai.StopReasonStop, "")
	assertLlamaLoopbackTokenInputs(t, first.matching("/tokenize")[len(firstTokens):], otherModel.ID, inputs)

	otherServer := *model
	otherServer.BaseURL = second.model().BaseURL
	checkLlamaResult(t, second.classify(t, &otherServer, fixture), goai.StopReasonStop, "")
	assertLlamaLoopbackTokenInputs(t, second.matching("/tokenize"), otherServer.ID, inputs)
}

// Supplemental adapter contracts, kept separate from the original declarations.
func TestLlamaCPPSettledTokenCacheV160(t *testing.T) {
	t.Run("stable-split", func(t *testing.T) {
		server := newLlamaSourceLoopback(t, &llamaSourceServer{}, llamaLoopbackPrefix())
		model := server.model()
		const want = `Label "Yes" is not a single token for qwen`
		result := server.classify(t, model, llamaSourceBool())
		checkLlamaResult(t, result, goai.StopReasonError, want)
		before := server.matching("/tokenize")
		assertLlamaLoopbackTokenInputs(t, before, model.ID, []string{"\n", "\nYes", "Yes"})
		checkLlamaResult(t, server.classify(t, model, llamaSourceBool()), goai.StopReasonError, want)
		if got := len(server.matching("/tokenize")); got != len(before) {
			t.Errorf("settled split re-tokenized: got %d, want %d", got, len(before))
		}
	})
	t.Run("retry-decode-failure", func(t *testing.T) {
		fail := true
		server := newLlamaSourceLoopback(t, &llamaSourceServer{tokens: func(content string) []int {
			if content == "\nA" && fail {
				fail = false
				return nil // JSON null is not a tokenization result.
			}
			return llamaSourceChars(content)
		}}, llamaLoopbackPrefix())
		model, ctx := server.model(), llamaSourcePick("Pick")
		checkLlamaResult(t, server.classify(t, model, ctx), goai.StopReasonError, "llama.cpp returned an unexpected tokenization")
		first := server.matching("/tokenize")
		assertLlamaLoopbackTokenInputs(t, first, model.ID, []string{"\n", "\nA"})
		checkLlamaResult(t, server.classify(t, model, ctx), goai.StopReasonStop, "")
		assertLlamaLoopbackTokenInputs(t, server.matching("/tokenize")[len(first):], model.ID, []string{"\n", "\n", "\nA", "\nB"})
	})
	t.Run("duplicate-token", func(t *testing.T) {
		server := newLlamaSourceLoopback(t, &llamaSourceServer{tokens: func(content string) []int {
			if content == "\nA" || content == "\nB" {
				return []int{10, 65}
			}
			return llamaSourceChars(content)
		}}, llamaLoopbackPrefix())
		model, ctx := server.model(), llamaSourcePick("Pick")
		for i := 0; i < 2; i++ {
			got := server.classify(t, model, ctx)
			const want = "Labels share a token for qwen: A, B"
			if got.ErrorMessage != want || got.StopReason != goai.StopReasonError || len(got.Answers) != 0 {
				t.Errorf("attempt %d: stop=%q error=%q answers=%v; want error %q", i, got.StopReason, got.ErrorMessage, got.Answers, want)
			}
		}
		assertLlamaLoopbackTokenInputs(t, server.matching("/tokenize"), model.ID, []string{"\n", "\n", "\nA", "\nB"})
		if got := len(server.matching("/completion")); got != 0 {
			t.Errorf("duplicate token IDs reached completion: %d", got)
		}
	})
}

// Supplemental: the original pre-aborts its signal. This also cancels an
// already admitted request and checks the real loopback peer sees it.
func TestLlamaCPPInFlightCallerCancellationV160(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	guard, stopGuard := context.WithTimeout(t.Context(), 5*time.Second)
	defer stopGuard()
	entered := make(chan map[string]any, 1)
	peerCanceled := make(chan error, 1)
	releasePeer := make(chan struct{})
	defer close(releasePeer)
	var requests atomic.Int32
	prefix := llamaLoopbackPrefix()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempt := requests.Add(1); attempt != 1 || r.Method != http.MethodPost || r.URL.Path != prefix+"/tokenize" {
			t.Errorf("unexpected request %d: %s %s", attempt, r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		entered <- body
		select {
		case <-r.Context().Done():
			peerCanceled <- r.Context().Err()
		case <-releasePeer:
		}
	}))
	t.Cleanup(server.Close)
	model := llamaSourceModel()
	model.BaseURL = server.URL + prefix + "/v1"
	type outcome struct {
		result *goai.ClassifierResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := goai.Classify(model, llamaSourcePick("Pick"), &goai.ClassifierOptions{Context: ctx, HTTPClient: server.Client()})
		done <- outcome{result, err}
	}()
	select {
	case body := <-entered:
		want := map[string]any{"model": "qwen", "content": "\n", "add_special": false, "parse_special": false}
		if !reflect.DeepEqual(body, want) {
			t.Fatalf("first tokenization = %#v, want %#v", body, want)
		}
	case <-guard.Done():
		t.Fatal("classifier did not reach the loopback server")
	}
	cancel()
	select {
	case err := <-peerCanceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("peer context = %v, want canceled", err)
		}
	case <-guard.Done():
		t.Fatal("loopback peer did not receive caller cancellation")
	}
	select {
	case got := <-done:
		if got.err != nil || got.result == nil || got.result.StopReason != goai.StopReasonAborted || len(got.result.Answers) != 0 {
			t.Fatalf("classification result = %+v, err=%v, want aborted with no answers", got.result, got.err)
		}
	case <-guard.Done():
		t.Fatal("classifier did not return after cancellation")
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("requests after cancellation = %d, want 1", got)
	}
}

func llamaSourceProbabilityLoopback(t *testing.T, labels string, probabilities []float64) *llamaSourceLoopback {
	t.Helper()
	logprobs := make(map[string]float64, len(labels))
	for i, p := range probabilities {
		logprob := -1000.0 // Exp underflows to zero; unlike -Inf, this is valid JSON.
		if p > 0 {
			logprob = math.Log(p)
		}
		logprobs[string(labels[i])] = logprob
	}
	return newLlamaSourceLoopback(t, &llamaSourceServer{next: func(string, int) map[string]float64 { return logprobs }}, llamaLoopbackPrefix())
}

func llamaSourceAnswerJSON(t *testing.T, answer goai.ClassifierAnswer) map[string]any {
	t.Helper()
	data, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func assertLlamaSingleReadoutV160(t *testing.T, server *llamaSourceLoopback, labels int) {
	t.Helper()
	completions := server.matching("/completion")
	if got := len(completions); got != 1 {
		t.Fatalf("completion count = %d, want 1", got)
	}
	if body := completions[0].body; body["temperature"] != float64(0) || body["n_predict"] != float64(1) || body["n_probs"] != float64(256) {
		t.Errorf("unexpected template readout request: %#v", body)
	}
	if tokens, templates, total := len(server.matching("/tokenize")), len(server.matching("/apply-template")), len(server.matching("")); tokens != 2*labels || templates != 1 || total != 2*labels+2 {
		t.Errorf("requests: tokenize=%d templates=%d total=%d, want %d, 1, %d", tokens, templates, total, 2*labels, 2*labels+2)
	}
}
