package goai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

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
	s.requests = append(s.requests, llamaSourceRequest{req.URL.String(), req.URL.Path, body, req.Header.Clone()})
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

func llamaSourcePick(instructions string) goai.ClassifierContext {
	return goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{
		"pick": {Type: "choice", Instructions: instructions, Criteria: map[string]string{"a": "", "b": ""}},
	}}
}

func runLlamaSource(t *testing.T, server *llamaSourceServer, model *goai.ClassifierModel, ctx goai.ClassifierContext, opts goai.ClassifierOptions) *goai.ClassifierResult {
	t.Helper()
	opts.HTTPClient = &http.Client{Transport: server}
	if opts.Context == nil {
		opts.Context = t.Context()
	}
	result, err := goai.Classify(model, ctx, &opts)
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
				// Go sorts keys, so technical is C rather than B.
				return map[string]float64{"C": -0.3, "A": -1.5, "B": -3}
			}
		}}
		model := llamaSourceModel()
		result := runLlamaSource(t, server, model, llamaSourceContext(), goai.ClassifierOptions{APIKey: "local"})
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
		result := runLlamaSource(t, server, llamaSourceModel(), llamaSourceContext(), goai.ClassifierOptions{})
		checkLlamaResult(t, result, goai.StopReasonStop, "")
		state := "State:\n{\n \"message\": \"Help! My payouts have been failing for 3 days.\"\n}"
		team := "Question: Which team should handle this?\n\nOptions:\n- billing: Payments and refunds\n- sales\n- technical: Bugs and outages"
		urgent := "Question: Does this convey urgency?\n\nYes means: The user needs help soon\nNo means: No time pressure"
		severity := "Question: How severe is this?\n\nLevels:\n0. low\n1. medium\n2. high"
		prefix := strings.Join([]string{state, "Task: answer each of the following questions about the state.", severity, team, urgent, state, ""}, "\n\n")
		templates := server.matching("/apply-template")
		if len(templates) != 3 {
			t.Fatalf("template calls=%d", len(templates))
		}
		finals := []string{severity + "\n\nAnswer with one level number.", "Question: Which team should handle this?\n\nOptions:\nA. billing: Payments and refunds\nB. sales\nC. technical: Bugs and outages\n\nAnswer with one letter.", urgent + "\n\nAnswer Yes or No."}
		for i, request := range templates {
			messages := request.body["messages"].([]any)
			got := messages[1].(map[string]any)["content"]
			if got != prefix+finals[i] {
				t.Errorf("question %d content:\n%v\nwant:\n%s", i, got, prefix+finals[i])
			}
		}
	})
	t.Run("temperature-division", func(t *testing.T) { // 216
		server := &llamaSourceServer{}
		result := runLlamaSource(t, server, llamaSourceModel(), llamaSourcePick("Pick one"), goai.ClassifierOptions{Temperature: 2})
		checkLlamaResult(t, result, goai.StopReasonStop, "")
		want := 1 / (1 + math.Exp(-1.2))
		checkLlamaFloat(t, "a", result.Answers["pick"].Probabilities["a"], want)
		checkLlamaFloat(t, "b", result.Answers["pick"].Probabilities["b"], 1-want)
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
		server := &llamaSourceServer{template: "<|assistant|>\n<think>"}
		runLlamaSource(t, server, llamaSourceModel(), llamaSourcePick("Pick"), goai.ClassifierOptions{})
		if requests := server.matching("/completion"); len(requests) != 1 || requests[0].body["prompt"] != "<|assistant|>\n<think></think>" {
			t.Errorf("completed prompt: %#v", requests)
		}
	})
	t.Run("single-token-labels", func(t *testing.T) { // 269
		merging := &llamaSourceServer{tokens: func(content string) []int {
			if strings.HasPrefix(content, "\n") && len(content) > 1 {
				return []int{1000}
			}
			return llamaSourceChars(content)
		}}
		checkLlamaResult(t, runLlamaSource(t, merging, llamaSourceModel(), llamaSourcePick("Pick"), goai.ClassifierOptions{}), goai.StopReasonStop, "")
		ctx := goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{"ok": {Type: "bool", Instructions: "OK?", Criteria: goai.ClassifierBoolCriteria{}}}}
		checkLlamaResult(t, runLlamaSource(t, &llamaSourceServer{}, llamaSourceModel(), ctx, goai.ClassifierOptions{}), goai.StopReasonError, `Label "Yes" is not a single token for qwen`)
	})
	t.Run("token-cache", func(t *testing.T) { // 292
		server, model := &llamaSourceServer{}, llamaSourceModel()
		ctx := llamaSourcePick("Pick")
		runLlamaSource(t, server, model, ctx, goai.ClassifierOptions{})
		first := len(server.matching("/tokenize"))
		runLlamaSource(t, server, model, ctx, goai.ClassifierOptions{})
		if total := len(server.matching("/tokenize")); first == 0 || total != first {
			t.Errorf("tokenizations: first=%d total=%d", first, total)
		}
	})
	t.Run("option-bounds", func(t *testing.T) { // 306
		server := &llamaSourceServer{}
		ctx := llamaSourcePick("Pick")
		criteria := map[string]string{}
		for i := range 63 {
			criteria[fmt.Sprintf("option%d", i)] = ""
		}
		ctx.Questions["pick"] = goai.ClassifierQuestion{Type: "choice", Instructions: "Pick", Criteria: criteria}
		checkLlamaResult(t, runLlamaSource(t, server, llamaSourceModel(), ctx, goai.ClassifierOptions{}), goai.StopReasonError, "A choice question needs 2 to 62 options, got 63")
		ctx.Questions = map[string]goai.ClassifierQuestion{"rate": {Type: "score", Instructions: "Rate", Criteria: []string{"only"}}}
		checkLlamaResult(t, runLlamaSource(t, server, llamaSourceModel(), ctx, goai.ClassifierOptions{}), goai.StopReasonError, "A score question needs 2 to 10 levels, got 1")
		if len(server.requests) != 0 {
			t.Errorf("sent requests before validating bounds: %#v", server.requests)
		}
	})
	t.Run("request-hooks", func(t *testing.T) { // 325
		server := &llamaSourceServer{}
		var payloads []map[string]any
		var statuses []int
		runLlamaSource(t, server, llamaSourceModel(), llamaSourcePick("Pick"), goai.ClassifierOptions{
			OnPayload: func(payload map[string]any, _ *goai.ClassifierModel) (map[string]any, error) {
				payloads = append(payloads, payload)
				out := map[string]any{"id_slot": 1}
				for k, v := range payload {
					out[k] = v
				}
				return out, nil
			},
			OnResponse: func(meta goai.ClassifierResponseMetadata, _ *goai.ClassifierModel) error {
				statuses = append(statuses, meta.Status)
				return nil
			},
		})
		completions := server.matching("/completion")
		if len(payloads) != 1 || fmt.Sprint(payloads[0]["n_predict"]) != "1" || !reflect.DeepEqual(statuses, []int{200}) || len(completions) != 1 || completions[0].body["id_slot"] != float64(1) {
			t.Errorf("payloads=%v statuses=%v completions=%#v", payloads, statuses, completions)
		}
	})
	t.Run("server-error-and-cancel", func(t *testing.T) { // 352
		zero := 0
		server := &llamaSourceServer{status: http.StatusBadRequest}
		result := runLlamaSource(t, server, llamaSourceModel(), llamaSourceContext(), goai.ClassifierOptions{MaxRetriesOverride: &zero})
		checkLlamaResult(t, result, goai.StopReasonError, "llama.cpp error (400)")
		if !strings.Contains(result.ErrorMessage, "context overflow") {
			t.Errorf("lost server details: %q", result.ErrorMessage)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		checkLlamaResult(t, runLlamaSource(t, &llamaSourceServer{}, llamaSourceModel(), llamaSourceContext(), goai.ClassifierOptions{Context: ctx}), goai.StopReasonAborted, "")
	})
	t.Run("other-api", func(t *testing.T) { // 373
		// The original calls the llama.cpp provider directly, not dispatch.
		server, model := &llamaSourceServer{}, llamaSourceModel()
		model.Api = goai.ClassifierApiTypeSafeSystemOne
		provider := goai.GetClassifierApiProvider(goai.ClassifierApiLlamaCPP)
		result, err := provider.Classify(model, llamaSourceContext(), &goai.ClassifierOptions{HTTPClient: &http.Client{Transport: server}})
		if err != nil || len(server.requests) != 0 {
			t.Fatalf("err=%v requests=%d", err, len(server.requests))
		}
		checkLlamaResult(t, result, goai.StopReasonError, "Unsupported classifier API: typesafe-system-one")
	})
	t.Run("base-urls", func(t *testing.T) { // 381
		for _, relative := range []struct{ scheme, suffix, rootSuffix string }{{"http", "/v1/", ""}, {"https", "/prefix/v1", "/prefix"}, {"http", "", ""}} {
			server, model := &llamaSourceServer{}, llamaSourceModel()
			base := relative.scheme + "://" + strings.TrimSuffix(strings.TrimPrefix(model.BaseURL, "http://"), "/v1")
			model.BaseURL = base + relative.suffix
			checkLlamaResult(t, runLlamaSource(t, server, model, llamaSourcePick("Pick"), goai.ClassifierOptions{}), goai.StopReasonStop, "")
			if completions := server.matching("/completion"); len(completions) != 1 || completions[0].url != base+relative.rootSuffix+"/completion" {
				t.Errorf("base %s: completion requests=%#v", model.BaseURL, completions)
			}
		}
	})
	t.Run("confidence-and-expected-score", func(t *testing.T) { // 387
		for _, tc := range []struct {
			probs             []float64
			score, confidence float64
		}{
			{[]float64{.89, .06, .05}, .16, .835},
			{[]float64{.5, .5}, .5, 0},
			{[]float64{1, 0, 0}, 0, 1},
			{[]float64{.2, .3, .5}, 1.3, .25},
		} {
			server := &llamaSourceServer{next: func(_ string, _ int) map[string]float64 {
				logprobs := map[string]float64{}
				for i, p := range tc.probs {
					logprobs[fmt.Sprint(i)] = -1000 // rounds to zero after exp, valid JSON logit
					if p > 0 {
						logprobs[fmt.Sprint(i)] = math.Log(p)
					}
				}
				return logprobs
			}}
			fixture := goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{"q": {Type: "score", Criteria: []string{"a", "b", "c"}[:len(tc.probs)]}}}
			result := runLlamaSource(t, server, llamaSourceModel(), fixture, goai.ClassifierOptions{})
			checkLlamaResult(t, result, goai.StopReasonStop, "")
			answer := result.Answers["q"]
			if answer.Type != "score" {
				t.Errorf("answer=%#v", answer)
			}
			checkLlamaFloat(t, "score", answer.Score, tc.score)
			checkLlamaFloat(t, "confidence", answer.Confidence, tc.confidence)
		}
	})
}
