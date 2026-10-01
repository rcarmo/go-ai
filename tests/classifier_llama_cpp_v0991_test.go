package goai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func llamaModelV0991(base string) *goai.ClassifierModel {
	return &goai.ClassifierModel{ID: "llama", Type: "classifier", Provider: "llama", Api: goai.ClassifierApiLlamaCPP, BaseURL: base + "/v1"}
}

func TestV0991LlamaCPPClassifierProductionPathEscalationHooksAndCache(t *testing.T) {
	var paths []string
	var completionDepths []int
	var completionPayloadHooked bool
	var sawResponse bool
	tokenizeCount := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		switch r.URL.Path {
		case "/tokenize":
			content := payload["content"].(string)
			tokenizeCount[content]++
			var tokens []int
			switch content {
			case "\n":
				tokens = []int{10}
			case "\nYes":
				tokens = []int{10, 101}
			case "\nNo":
				tokens = []int{10, 102}
			case "Yes":
				tokens = []int{101}
			case "No":
				tokens = []int{102}
			default:
				tokens = []int{999}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tokens": tokens})
		case "/apply-template":
			if kwargs, ok := payload["chat_template_kwargs"].(map[string]any); !ok || kwargs["enable_thinking"] != false {
				t.Fatalf("apply-template payload=%#v", payload)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"prompt": "PROMPT<think>"})
		case "/completion":
			if payload["hooked"] != true {
				t.Fatalf("completion payload was not hooked: %#v", payload)
			}
			depth := int(payload["n_probs"].(float64))
			completionDepths = append(completionDepths, depth)
			entries := []map[string]any{{"id": 101, "logprob": -0.2}}
			if depth >= 4096 {
				entries = append(entries, map[string]any{"id": 102, "logprob": -1.2})
			}
			w.Header().Set("X-Test", "ok")
			_ = json.NewEncoder(w).Encode(map[string]any{"completion_probabilities": []any{map[string]any{"top_logprobs": entries}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	ctx := goai.ClassifierContext{State: map[string]any{"text": "state"}, Questions: map[string]goai.ClassifierQuestion{"safe": {Type: "bool", Instructions: "Is the state safe?", Criteria: goai.ClassifierBoolCriteria{True: "safe", False: "unsafe"}}}}
	result, err := goai.Classify(llamaModelV0991(server.URL), ctx, &goai.ClassifierOptions{Temperature: 0.5, OnPayload: func(payload map[string]any, model *goai.ClassifierModel) (map[string]any, error) {
		completionPayloadHooked = true
		payload["hooked"] = true
		return payload, nil
	}, OnResponse: func(response goai.ClassifierResponseMetadata, model *goai.ClassifierModel) error {
		sawResponse = response.Status == 200 && response.Headers["X-Test"] == "ok"
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != goai.StopReasonStop || !completionPayloadHooked || !sawResponse {
		t.Fatalf("result=%#v hooked=%v response=%v", result, completionPayloadHooked, sawResponse)
	}
	if !reflect.DeepEqual(completionDepths, []int{256, 4096}) {
		t.Fatalf("completion depths=%#v", completionDepths)
	}
	if result.Answers["safe"].Type != "bool" || result.Answers["safe"].Probability <= 0.85 {
		t.Fatalf("answer=%#v", result.Answers["safe"])
	}
	if tokenizeCount["\nYes"] != 1 || tokenizeCount["\nNo"] != 1 {
		t.Fatalf("label token cache counts=%#v paths=%#v", tokenizeCount, paths)
	}
	second, _ := goai.Classify(llamaModelV0991(server.URL), ctx, &goai.ClassifierOptions{OnPayload: func(payload map[string]any, model *goai.ClassifierModel) (map[string]any, error) {
		payload["hooked"] = true
		return payload, nil
	}})
	if second.StopReason != goai.StopReasonStop || tokenizeCount["\nYes"] != 1 || tokenizeCount["\nNo"] != 1 {
		t.Fatalf("second=%#v label cache counts=%#v", second, tokenizeCount)
	}
}

func TestV0991LlamaCPPClassifierChoiceScoreAndErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		switch r.URL.Path {
		case "/tokenize":
			content := payload["content"].(string)
			ids := map[string]int{"\n": 1, "\nA": 11, "\nB": 12, "\n0": 20, "\n1": 21, "A": 11, "B": 12, "0": 20, "1": 21}
			_ = json.NewEncoder(w).Encode(map[string]any{"tokens": []int{ids[content]}})
		case "/apply-template":
			_ = json.NewEncoder(w).Encode(map[string]any{"prompt": "PROMPT"})
		case "/completion":
			prompt := payload["prompt"].(string)
			if prompt == "PROMPT" {
				_ = json.NewEncoder(w).Encode(map[string]any{"completion_probabilities": []any{map[string]any{"top_logprobs": []any{map[string]any{"id": 11, "logprob": -2.1}, map[string]any{"id": 12, "logprob": -0.1}, map[string]any{"id": 20, "logprob": -2}, map[string]any{"id": 21, "logprob": -0.2}}}}})
			}
		}
	}))
	defer server.Close()
	ctx := goai.ClassifierContext{State: map[string]any{"text": "state"}, Questions: map[string]goai.ClassifierQuestion{"kind": {Type: "choice", Instructions: "Choose a colour", Criteria: map[string]string{"red": "red meaning", "blue": "blue meaning"}}, "score": {Type: "score", Instructions: "Rate the state", Criteria: []string{"low", "high"}}}}
	result, _ := goai.Classify(llamaModelV0991(server.URL), ctx, nil)
	if result.StopReason != goai.StopReasonStop || result.Answers["kind"].Choice != "red" || result.Answers["score"].Score <= 0.8 {
		t.Fatalf("result=%#v", result)
	}
	badTemp, _ := goai.Classify(llamaModelV0991(server.URL), ctx, &goai.ClassifierOptions{Temperature: -1})
	if badTemp.StopReason != goai.StopReasonError || badTemp.ErrorMessage == "" {
		t.Fatalf("bad temperature result=%#v", badTemp)
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled, _ := goai.Classify(llamaModelV0991(server.URL), ctx, &goai.ClassifierOptions{Context: cancelCtx})
	if cancelled.StopReason != goai.StopReasonAborted {
		t.Fatalf("cancelled=%#v", cancelled)
	}
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(50 * time.Millisecond) }))
	defer slowServer.Close()
	timedOut, _ := goai.Classify(llamaModelV0991(slowServer.URL), ctx, &goai.ClassifierOptions{TimeoutMs: 1})
	if timedOut.StopReason != goai.StopReasonError || timedOut.ErrorMessage == "" {
		t.Fatalf("timeout=%#v", timedOut)
	}
}
