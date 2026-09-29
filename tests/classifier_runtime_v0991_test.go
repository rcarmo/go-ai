package goai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func classifierContextV0991() goai.ClassifierContext {
	return goai.ClassifierContext{State: "hello", Questions: map[string]goai.ClassifierQuestion{
		"safe":  {Type: "bool"},
		"kind":  {Type: "choice", Choices: []string{"a", "b"}},
		"score": {Type: "score"},
	}}
}

func TestV0991SystemOneClassifierRequestParsingHooksAndUsage(t *testing.T) {
	var sawPayload bool
	var sawResponse bool
	var gotPath string
	var gotAuth string
	var gotQuestions map[string]map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var payload struct {
			Model     string                    `json:"model"`
			State     string                    `json:"state"`
			Questions map[string]map[string]any `json:"questions"`
			Hooked    bool                      `json:"hooked"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		gotQuestions = payload.Questions
		if payload.Model != "jev-latest" || !payload.Hooked {
			t.Fatalf("payload=%#v", payload)
		}
		w.Header().Set("X-Test", "ok")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"usage": map[string]any{"input_tokens": 1000, "output_tokens": 10},
			"answers": map[string]any{
				"safe":  map[string]any{"type": "noul", "noul": 0.75},
				"kind":  map[string]any{"type": "choice", "choice": "a", "probabilities": map[string]any{"a": 0.7, "b": 0.3}, "confidence": 0.8},
				"score": map[string]any{"type": "score", "score": 0.42, "confidence": 0.9},
			},
		})
	}))
	defer server.Close()
	model := &goai.ClassifierModel{ID: "jev-latest", Type: "classifier", Provider: goai.ClassifierProviderTypeSafe, Api: goai.ClassifierApiTypeSafeSystemOne, BaseURL: server.URL, Cost: goai.ModelCost{Input: 1, Output: 2}}
	result, err := goai.Classify(model, classifierContextV0991(), &goai.ClassifierOptions{APIKey: "key", OnPayload: func(payload map[string]any, model *goai.ClassifierModel) (map[string]any, error) {
		sawPayload = true
		payload["hooked"] = true
		return payload, nil
	}, OnResponse: func(response goai.ClassifierResponseMetadata, model *goai.ClassifierModel) error {
		sawResponse = response.Status == 200 && response.Headers["X-Test"] == "ok"
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != goai.StopReasonStop || !sawPayload || !sawResponse || gotPath != "/systemone" || gotAuth != "Bearer key" {
		t.Fatalf("result=%#v sawPayload=%v sawResponse=%v path=%q auth=%q", result, sawPayload, sawResponse, gotPath, gotAuth)
	}
	if gotQuestions["safe"]["type"] != "noul" {
		t.Fatalf("bool question was not mapped to noul: %#v", gotQuestions)
	}
	if result.Answers["safe"].Type != "bool" || result.Answers["safe"].Probability != 0.75 || result.Answers["kind"].Choice != "a" || result.Answers["kind"].Probabilities["b"] != 0.3 || result.Answers["score"].Score != 0.42 {
		t.Fatalf("answers=%#v", result.Answers)
	}
	if result.Usage == nil || result.Usage.Input != 1000 || result.Usage.Output != 10 || result.Usage.Cost.Input != 0.001 || result.Usage.Cost.Output != 0.00002 {
		t.Fatalf("usage=%#v", result.Usage)
	}
}

func TestV0991CloudflareClassifierRoutesAccountPlaceholder(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"safe": map[string]any{"type": "noul", "noul": 0.2}}})
	}))
	defer server.Close()
	model := &goai.ClassifierModel{ID: "typesafe/jev", Type: "classifier", Provider: goai.ClassifierProviderCloudflareWorkersAI, Api: goai.ClassifierApiCloudflareWorkersAI, BaseURL: server.URL + "/client/v4/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai"}
	ctx := goai.ClassifierContext{State: "s", Questions: map[string]goai.ClassifierQuestion{"safe": {Type: "bool"}}}
	result, _ := goai.Classify(model, ctx, &goai.ClassifierOptions{APIKey: "cf", Env: goai.ProviderEnv{"CLOUDFLARE_ACCOUNT_ID": "acct"}})
	if result.StopReason != goai.StopReasonStop || gotPath != "/client/v4/accounts/acct/ai/run/typesafe/jev" || gotAuth != "Bearer cf" {
		t.Fatalf("result=%#v path=%q auth=%q", result, gotPath, gotAuth)
	}
}

func TestV0991ClassifierMalformedAnswersPreserveUsageCost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"usage": map[string]any{"input_tokens": 1000, "output_tokens": 1000}, "answers": map[string]any{"safe": map[string]any{"type": "bad"}}})
	}))
	defer server.Close()
	model := &goai.ClassifierModel{ID: "jev-latest", Type: "classifier", Provider: goai.ClassifierProviderTypeSafe, Api: goai.ClassifierApiTypeSafeSystemOne, BaseURL: server.URL, Cost: goai.ModelCost{Input: 1, Output: 2}}
	result, _ := goai.Classify(model, goai.ClassifierContext{State: "s", Questions: map[string]goai.ClassifierQuestion{"safe": {Type: "bool"}}}, &goai.ClassifierOptions{APIKey: "key"})
	if result.StopReason != goai.StopReasonError || result.Usage == nil || result.Usage.Cost.Total != 0.003 {
		t.Fatalf("malformed result=%#v", result)
	}
}

func TestV0991ClassifierValidationRetryTimeoutAndCancel(t *testing.T) {
	badAPI := &goai.ClassifierModel{ID: "m", Provider: goai.ClassifierProviderTypeSafe, Api: "bad", BaseURL: "http://127.0.0.1"}
	result, _ := goai.Classify(badAPI, classifierContextV0991(), &goai.ClassifierOptions{APIKey: "key"})
	if result.StopReason != goai.StopReasonError || result.ErrorMessage == "" {
		t.Fatalf("bad api result=%#v", result)
	}
	model := &goai.ClassifierModel{ID: "jev-latest", Provider: goai.ClassifierProviderTypeSafe, Api: goai.ClassifierApiTypeSafeSystemOne, BaseURL: "http://127.0.0.1"}
	missing, _ := goai.Classify(model, classifierContextV0991(), nil)
	if missing.StopReason != goai.StopReasonError || missing.ErrorMessage == "" {
		t.Fatalf("missing key result=%#v", missing)
	}

	attempts := 0
	retryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"safe": map[string]any{"type": "noul", "noul": 0.1}, "kind": map[string]any{"type": "choice", "choice": "a", "probabilities": map[string]any{"a": 1}, "confidence": 1}, "score": map[string]any{"type": "score", "score": 0.1, "confidence": 1}}})
	}))
	defer retryServer.Close()
	model.BaseURL = retryServer.URL
	retried, _ := goai.Classify(model, classifierContextV0991(), &goai.ClassifierOptions{APIKey: "key", MaxRetries: 1, MaxRetryDelayMs: 1})
	if retried.StopReason != goai.StopReasonStop || attempts != 2 {
		t.Fatalf("retry result=%#v attempts=%d", retried, attempts)
	}

	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(50 * time.Millisecond) }))
	defer slowServer.Close()
	model.BaseURL = slowServer.URL
	timedOut, _ := goai.Classify(model, classifierContextV0991(), &goai.ClassifierOptions{APIKey: "key", TimeoutMs: 1})
	if timedOut.StopReason != goai.StopReasonError || timedOut.ErrorMessage == "" {
		t.Fatalf("timeout result=%#v", timedOut)
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled, _ := goai.Classify(model, classifierContextV0991(), &goai.ClassifierOptions{APIKey: "key", Context: cancelCtx})
	if cancelled.StopReason != goai.StopReasonAborted {
		t.Fatalf("cancel result=%#v", cancelled)
	}
}
