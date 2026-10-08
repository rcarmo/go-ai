package goai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func decisionsModel110(endpoint string) *goai.ClassifierModel {
	return &goai.ClassifierModel{ID: "gpt-6-luna", Type: "classifier", Provider: goai.ClassifierProviderOpenAI, Api: goai.ClassifierApiOpenAIDecisions, BaseURL: endpoint, Input: []string{"text", "image"}, Cost: goai.ModelCost{Input: .1, Tiers: []goai.ModelCostTier{{InputTokensAbove: 272000, Input: .2}}}}
}
func decisionsAnswers110() []any {
	return []any{
		map[string]any{"name": "bool", "type": "predicate", "probability": 0.0},
		map[string]any{"name": "score", "type": "score", "score": 0.0, "confidence": 1.0},
		map[string]any{"name": "choice", "type": "choice", "choice": "blue", "confidence": .9, "probabilities": []any{map[string]any{"value": "blue", "probability": .8}, map[string]any{"value": "red", "probability": .2}}},
	}
}
func TestDecisions110ProductionWireImagesHooksAndTierUsage(t *testing.T) {
	for _, images := range []bool{false, true} {
		t.Run(fmt.Sprint(images), func(t *testing.T) {
			classCtx := issue1Context()
			classCtx.Questions["choice"] = goai.ClassifierQuestion{Type: "choice", Instructions: "choose", Criteria: map[string]string{"blue": "", "red": "warm"}}
			if images {
				classCtx.Images = []goai.ImageContent{{Type: "image", MimeType: "image/png", Data: "AA=="}}
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path != "/v1/decisions" || r.Header.Get("Authorization") != "Bearer option" || r.Header.Get("X-Test") != "option" {
					t.Error("request routing/header precedence", r.URL, r.Header)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if body["model"] != "gpt-6-luna" || body["hooked"] != true {
					t.Error(body)
				}
				if images {
					msg := body["input"].([]any)[0].(map[string]any)
					parts := msg["content"].([]any)
					if msg["role"] != "user" || parts[0].(map[string]any)["type"] != "input_text" || parts[1].(map[string]any)["image_url"] != "data:image/png;base64,AA==" {
						t.Error(msg)
					}
				} else if state, ok := body["input"].(string); !ok || !strings.Contains(state, `"text":"hello"`) {
					t.Error("state not JSON text", body)
				}
				questions := map[string]map[string]any{}
				for _, value := range body["questions"].([]any) {
					q := value.(map[string]any)
					questions[q["name"].(string)] = q
				}
				if questions["bool"]["type"] != "predicate" || questions["bool"]["criteria"] != nil || questions["bool"]["instructions"] != "Is it safe?\n\nTrue means: safe\nFalse means: unsafe" {
					t.Error(questions)
				}
				choice := questions["choice"]["choices"].([]any)[0].(map[string]any)
				if choice["value"] != "blue" || choice["description"] != nil {
					t.Error(choice)
				}
				if questions["score"]["levels"].([]any)[2].(map[string]any)["label"] != "good" {
					t.Error(questions)
				}
				w.Header().Set("X-Observed", "yes")
				_ = json.NewEncoder(w).Encode(map[string]any{"usage": map[string]any{"input_tokens": 300000}, "answers": decisionsAnswers110()})
			}))
			defer server.Close()
			model := decisionsModel110(server.URL + "/v1/")
			model.Headers = map[string]string{"Authorization": "Bearer model", "X-Test": "model"}
			payloadCalls, responseCalls := 0, 0
			result, err := goai.Classify(model, classCtx, &goai.ClassifierOptions{APIKey: "key", Headers: map[string]string{"authorization": "Bearer option", "x-test": "option"}, OnPayload: func(body map[string]any, m *goai.ClassifierModel) (map[string]any, error) {
				payloadCalls++
				body["hooked"] = true
				return body, nil
			}, OnResponse: func(meta goai.ClassifierResponseMetadata, m *goai.ClassifierModel) error {
				responseCalls++
				if meta.Status != 200 || meta.Headers["X-Observed"] != "yes" {
					t.Error(meta)
				}
				return nil
			}})
			if err != nil || result.StopReason != goai.StopReasonStop || len(result.Answers) != 3 || result.Answers["bool"].Type != "bool" || result.Answers["bool"].Probability != 0 || result.Answers["choice"].Choice != "blue" {
				t.Fatal(result, err)
			}
			if result.Usage == nil || math.Abs(result.Usage.Cost.Total-.06) > 1e-12 || payloadCalls != 1 || responseCalls != 1 || requests.Load() != 1 {
				t.Fatal("usage/hooks", result.Usage, payloadCalls, responseCalls, requests.Load())
			}
		})
	}
}

func TestDecisions110MalformedAndRefusalKeepBilledUsage(t *testing.T) {
	cases := map[string]any{
		"refusal":     []any{map[string]any{"name": "bool", "type": "refusal"}},
		"missing":     []any{},
		"mistyped":    []any{map[string]any{"name": "bool", "type": "bool", "probability": .5}},
		"null-number": []any{map[string]any{"name": "bool", "type": "predicate", "probability": nil}},
		"not-array":   map[string]any{},
	}
	for name, answers := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"usage": map[string]any{"input_tokens": 164, "output_tokens": 0}, "answers": answers})
			}))
			defer server.Close()
			classCtx := issue1Context()
			classCtx.Questions = map[string]goai.ClassifierQuestion{"bool": classCtx.Questions["bool"]}
			result, err := goai.Classify(decisionsModel110(server.URL), classCtx, &goai.ClassifierOptions{APIKey: "key"})
			if err != nil || result.StopReason != goai.StopReasonError || len(result.Answers) != 0 || result.Usage == nil || result.Usage.Input != 164 {
				t.Fatal(result, err)
			}
		})
	}
	for _, bad := range []string{`{"answers":[{"name":"choice","type":"choice","choice":"red","confidence":1,"probabilities":[null]}],"usage":{"input_tokens":1}}`, `{"answers":[{"name":"choice","type":"choice","choice":"red","confidence":1,"probabilities":[{"value":"red","probability":1e400}]}],"usage":{"input_tokens":1}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(bad)) }))
		classCtx := issue1Context()
		classCtx.Questions = map[string]goai.ClassifierQuestion{"choice": classCtx.Questions["choice"]}
		result, _ := goai.Classify(decisionsModel110(server.URL), classCtx, &goai.ClassifierOptions{APIKey: "key"})
		server.Close()
		if result.StopReason != goai.StopReasonError || result.Usage == nil {
			t.Fatal(result)
		}
	}
}
func TestDecisions110ValidationNoTransportAndUnsupportedAPIImages(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	model := decisionsModel110(server.URL)
	classCtx := issue1Context()
	classCtx.Images = make([]goai.ImageContent, 129)
	result, _ := goai.Classify(model, classCtx, &goai.ClassifierOptions{APIKey: "key"})
	if result.StopReason != goai.StopReasonError || !strings.Contains(result.ErrorMessage, "at most 128") {
		t.Fatal(result)
	}
	classCtx.Images = classCtx.Images[:1]
	model.Input = []string{"text"}
	result, _ = goai.Classify(model, classCtx, &goai.ClassifierOptions{APIKey: "key"})
	if !strings.Contains(result.ErrorMessage, "does not accept image") {
		t.Fatal(result)
	}
	model.Input = []string{"image"}
	result, _ = goai.Classify(model, issue1Context(), nil)
	if !strings.Contains(result.ErrorMessage, "no API key") {
		t.Fatal(result)
	}
	for _, api := range []goai.ClassifierApi{goai.ClassifierApiTypeSafeSystemOne, goai.ClassifierApiCloudflareWorkersAI, goai.ClassifierApiLlamaCPP} {
		model.Api = api
		result, _ = goai.Classify(model, classCtx, &goai.ClassifierOptions{APIKey: "key"})
		if result.StopReason != goai.StopReasonError || !strings.Contains(result.ErrorMessage, "does not support image") {
			t.Fatal(api, result)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid input caused HTTP", calls.Load())
	}
}
func TestDecisions110GatewayNoRetryOtherRetriesCancelTimeout(t *testing.T) {
	for _, status := range []int{504, 503, 401} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempt := calls.Add(1)
				if attempt == 1 || status != 503 {
					w.Header().Set("retry-after-ms", "0")
					w.WriteHeader(status)
					if status == 504 {
						_, _ = w.Write([]byte("<html>gateway error</html>"))
					} else {
						_, _ = w.Write([]byte(`{"error":{"message":"provider detail"}}`))
					}
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"answers": decisionsAnswers110()})
			}))
			defer server.Close()
			result, err := goai.Classify(decisionsModel110(server.URL), issue1Context(), &goai.ClassifierOptions{APIKey: "key", MaxRetries: 2})
			if err != nil {
				t.Fatal(err)
			}
			if status == 503 {
				if result.StopReason != goai.StopReasonStop || calls.Load() != 2 {
					t.Fatal(result, calls.Load())
				}
			} else {
				if calls.Load() != 1 || result.StopReason != goai.StopReasonError {
					t.Fatal(result, calls.Load())
				}
				if status == 504 && (!strings.Contains(result.ErrorMessage, "600K") || strings.Contains(result.ErrorMessage, "<html>")) {
					t.Fatal(result)
				}
				if status == 401 && !strings.Contains(result.ErrorMessage, "provider detail") {
					t.Fatal(result)
				}
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(30 * time.Millisecond) }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, _ := goai.Classify(decisionsModel110(server.URL), issue1Context(), &goai.ClassifierOptions{APIKey: "key", Context: ctx})
	if result.StopReason != goai.StopReasonAborted {
		t.Fatal(result)
	}
	deadline, cancelDeadline := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelDeadline()
	result, _ = goai.Classify(decisionsModel110(server.URL), issue1Context(), &goai.ClassifierOptions{APIKey: "key", Context: deadline, TimeoutMs: 5})
	if result.StopReason != goai.StopReasonError || result.ErrorMessage == "" {
		t.Fatal(result)
	}
}

func TestDecisions110GeneratedTypedIdentityAndCloneTiers(t *testing.T) {
	goai.RegisterBuiltinModels()
	goai.RegisterBuiltinClassifierModels()
	chat := goai.GetModel(goai.ProviderOpenAI, "gpt-6-luna")
	model := goai.GetClassifierModel(goai.ClassifierProviderOpenAI, "gpt-6-luna")
	if chat == nil || chat.Api != goai.ApiOpenAIResponses || model == nil || model.Api != goai.ClassifierApiOpenAIDecisions || len(model.Cost.Tiers) != 1 || len(model.Input) != 2 {
		t.Fatal(chat, model)
	}
	model.Cost.Tiers[0].Input = 999
	if goai.GetClassifierModel(goai.ClassifierProviderOpenAI, "gpt-6-luna").Cost.Tiers[0].Input != .2 {
		t.Fatal("classifier price tiers alias registry")
	}
}
func TestDecisions110SpecialNamesAndMaximumImages(t *testing.T) {
	classCtx := goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{"__proto__": {Type: "bool", Instructions: "", Criteria: goai.ClassifierBoolCriteria{}}}, Images: make([]goai.ImageContent, 128)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		parts := body["input"].([]any)[0].(map[string]any)["content"].([]any)
		if len(parts) != 129 {
			t.Error("128 images dropped", len(parts))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": []any{map[string]any{"name": "__proto__", "type": "predicate", "probability": 1}}})
	}))
	defer server.Close()
	result, err := goai.Classify(decisionsModel110(server.URL), classCtx, &goai.ClassifierOptions{APIKey: "key"})
	if err != nil || result.StopReason != goai.StopReasonStop || result.Answers["__proto__"].Probability != 1 {
		t.Fatal(result, err)
	}
}
