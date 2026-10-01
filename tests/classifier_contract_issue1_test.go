package goai_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func issue1Context() goai.ClassifierContext {
	return goai.ClassifierContext{
		State: map[string]any{"text": "hello", "nested": map[string]any{"items": []any{nil, true, 2.5, "data"}}, "large": json.Number("9007199254740993")},
		Questions: map[string]goai.ClassifierQuestion{
			"choice": {Type: "choice", Instructions: "Choose a colour", Criteria: map[string]string{"red": "warm", "blue": "cool"}},
			"score":  {Type: "score", Instructions: "Rate quality", Criteria: []string{"poor", "fair", "good"}},
			"bool":   {Type: "bool", Instructions: "Is it safe?", Criteria: goai.ClassifierBoolCriteria{True: "safe", False: "unsafe"}},
		},
	}
}

func issue1Answers() map[string]any {
	return map[string]any{
		"choice": map[string]any{"type": "choice", "choice": "red", "probabilities": map[string]any{"red": 1, "blue": 0}, "confidence": 1},
		"score":  map[string]any{"type": "score", "score": 0, "confidence": 0},
		"bool":   map[string]any{"type": "noul", "noul": 0},
	}
}

func TestIssue1ClassifierPublicJSONRoundTripAndZeroAnswers(t *testing.T) {
	data, err := json.Marshal(issue1Context())
	if err != nil {
		t.Fatal(err)
	}
	var decoded goai.ClassifierContext
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(roundTrip) != string(data) {
		t.Fatalf("JSON changed: %s -> %s", data, roundTrip)
	}
	if _, ok := decoded.Questions["choice"].Criteria.(map[string]string); !ok {
		t.Fatalf("choice criteria=%T", decoded.Questions["choice"].Criteria)
	}
	if _, ok := decoded.Questions["score"].Criteria.([]string); !ok {
		t.Fatalf("score criteria=%T", decoded.Questions["score"].Criteria)
	}
	if _, ok := decoded.Questions["bool"].Criteria.(goai.ClassifierBoolCriteria); !ok {
		t.Fatalf("bool criteria=%T", decoded.Questions["bool"].Criteria)
	}
	for _, answer := range []goai.ClassifierAnswer{
		{Type: "choice", Choice: "", Probabilities: map[string]float64{"zero": 0}},
		{Type: "score"}, {Type: "bool"},
	} {
		t.Run(answer.Type, func(t *testing.T) {
			data, err := json.Marshal(answer)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			_ = json.Unmarshal(data, &fields)
			wantFields := map[string]int{"choice": 4, "score": 3, "bool": 2}[answer.Type]
			if len(fields) != wantFields {
				t.Fatalf("fields=%s", data)
			}
			for _, key := range map[string][]string{"choice": {"choice", "probabilities", "confidence"}, "score": {"score", "confidence"}, "bool": {"probability"}}[answer.Type] {
				if _, ok := fields[key]; !ok {
					t.Fatalf("missing %s: %s", key, data)
				}
			}
			var roundTrip goai.ClassifierAnswer
			if err := json.Unmarshal(data, &roundTrip); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(answer, roundTrip) {
				t.Fatalf("round trip=%#v", roundTrip)
			}
		})
	}
}

func TestIssue1ClassifierRejectsMalformedPublicJSON(t *testing.T) {
	for _, raw := range []string{
		`{"state":null,"questions":{}}`, `{"state":"legacy","questions":{}}`, `{"state":[],"questions":{}}`,
		`{"state":{},"questions":null}`, `{"state":{}}`, `{"questions":{}}`,
		`{"state":{"number":1e400},"questions":{}}`,
	} {
		var ctx goai.ClassifierContext
		if err := json.Unmarshal([]byte(raw), &ctx); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{
		`{"type":"choice","criteria":{}}`, `{"type":"choice","instructions":null,"criteria":{}}`,
		`{"type":"choice","instructions":"","criteria":{"a":null}}`, `{"type":"choice","instructions":"","criteria":["a"]}`,
		`{"type":"score","instructions":"","criteria":[null]}`, `{"type":"score","instructions":"","criteria":{}}`,
		`{"type":"bool","instructions":"","criteria":{"true":""}}`, `{"type":"bool","instructions":"","criteria":{"true":null,"false":""}}`,
		`{"type":"noul","instructions":"","criteria":{"true":"","false":""}}`,
	} {
		var question goai.ClassifierQuestion
		if err := json.Unmarshal([]byte(raw), &question); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{
		`null`, `[]`, `{}`, `{"type":"noul","noul":0}`, `{"type":"bool"}`, `{"type":"bool","probability":null}`,
		`{"type":"score","score":0}`, `{"type":"score","score":null,"confidence":0}`,
		`{"type":"choice","choice":"a","probabilities":{},"confidence":null}`,
		`{"type":"choice","choice":"a","probabilities":{"a":null},"confidence":0}`,
	} {
		var answer goai.ClassifierAnswer
		if err := json.Unmarshal([]byte(raw), &answer); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, answer := range []goai.ClassifierAnswer{{Type: "bad"}, {Type: "choice"}, {Type: "bool", Probability: math.Inf(1)}, {Type: "score", Score: math.NaN()}} {
		if _, err := json.Marshal(answer); err == nil {
			t.Errorf("marshalled invalid answer %#v", answer)
		}
	}
}

func TestIssue1ClassifierRejectsNonJSONStateBeforeTransport(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	cycle := map[string]any{}
	cycle["cycle"] = cycle
	for _, value := range []any{math.NaN(), math.Inf(1), json.Number("1e400"), []byte("bytes"), map[int]string{1: "key"}, struct{ Name string }{"struct"}, func() {}, make(chan int), cycle} {
		ctx := issue1Context()
		ctx.State["bad"] = value
		model := &goai.ClassifierModel{ID: "jev", Api: goai.ClassifierApiTypeSafeSystemOne, BaseURL: server.URL}
		result, err := goai.Classify(model, ctx, &goai.ClassifierOptions{APIKey: "key"})
		if err != nil || result.StopReason != goai.StopReasonError || result.ErrorMessage == "" {
			t.Errorf("value=%T result=%#v err=%v", value, result, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("requests=%d", requests.Load())
	}
}

func TestIssue1ClassifierSystemOneTransportContracts(t *testing.T) {
	for _, api := range []goai.ClassifierApi{goai.ClassifierApiTypeSafeSystemOne, goai.ClassifierApiCloudflareWorkersAI} {
		t.Run(string(api), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wantPath := "/systemone"
				if api == goai.ClassifierApiCloudflareWorkersAI {
					wantPath = "/run"
				}
				if r.URL.Path != wantPath || r.Header.Get("Authorization") != "Bearer option" || r.Header.Get("X-Custom") != "option" {
					t.Errorf("request=%s headers=%v", r.URL, r.Header)
				}
				var payload map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					return
				}
				if string(payload["model"]) != `"jev"` || string(payload["hooked"]) != "true" {
					t.Errorf("payload=%s", payload)
				}
				input := payload
				if api == goai.ClassifierApiCloudflareWorkersAI {
					if payload["state"] != nil || payload["questions"] != nil {
						t.Errorf("flat Cloudflare request: %s", payload)
					}
					if err := json.Unmarshal(payload["input"], &input); err != nil {
						t.Error(err)
						return
					}
				} else if payload["input"] != nil {
					t.Errorf("nested TypeSafe request: %s", payload)
				}
				if !strings.Contains(string(input["state"]), `9007199254740993`) || !strings.Contains(string(input["state"]), `[null,true,2.5,"data"]`) {
					t.Errorf("state=%s", input["state"])
				}
				var questions map[string]map[string]any
				_ = json.Unmarshal(input["questions"], &questions)
				for id, expected := range issue1Context().Questions {
					wantType := expected.Type
					if wantType == "bool" {
						wantType = "noul"
					}
					if questions[id]["type"] != wantType || questions[id]["instructions"] != expected.Instructions || len(questions[id]) != 3 {
						t.Errorf("question=%v", questions[id])
					}
					wantJSON, _ := json.Marshal(expected.Criteria)
					var want any
					_ = json.Unmarshal(wantJSON, &want)
					if !reflect.DeepEqual(want, questions[id]["criteria"]) {
						t.Errorf("criteria=%v want=%v", questions[id]["criteria"], want)
					}
				}
				body := map[string]any{"answers": issue1Answers(), "usage": map[string]any{"input_tokens": 10, "output_tokens": 5}}
				if api == goai.ClassifierApiCloudflareWorkersAI {
					body = map[string]any{"success": true, "result": map[string]any{"state": "Completed", "result": body}}
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer server.Close()
			model := &goai.ClassifierModel{ID: "jev", Api: api, BaseURL: server.URL + "/", Headers: map[string]string{"Authorization": "Bearer model", "X-Custom": "model"}, Cost: goai.ModelCost{Input: 1, Output: 2}}
			result, err := goai.Classify(model, issue1Context(), &goai.ClassifierOptions{APIKey: "key", Headers: map[string]string{"Authorization": "Bearer option", "X-Custom": "option"}, OnPayload: func(p map[string]any, _ *goai.ClassifierModel) (map[string]any, error) {
				p["hooked"] = true
				return p, nil
			}})
			if err != nil || result.StopReason != goai.StopReasonStop || len(result.Answers) != 3 || result.Usage == nil || math.Abs(result.Usage.Cost.Total-0.00002) > 1e-15 {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if result.Answers["bool"].Type != "bool" || result.Answers["score"].Score != 0 || result.Answers["choice"].Probabilities["blue"] != 0 {
				t.Fatalf("answers=%#v", result.Answers)
			}
		})
	}
}

func TestIssue1ClassifierMalformedWireAnswersKeepUsage(t *testing.T) {
	cases := map[string]any{
		"missing": nil, "wrong bool type": map[string]any{"type": "bool", "probability": 0},
		"missing choice confidence": map[string]any{"type": "choice", "choice": "red", "probabilities": map[string]any{}},
		"null probability":          map[string]any{"type": "choice", "choice": "red", "probabilities": map[string]any{"red": nil}, "confidence": 0},
		"score string":              map[string]any{"type": "score", "score": "0", "confidence": 0},
	}
	for name, answer := range cases {
		t.Run(name, func(t *testing.T) {
			id := "bool"
			if strings.Contains(name, "choice") || name == "null probability" {
				id = "choice"
			}
			if strings.Contains(name, "score") {
				id = "score"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				answers := issue1Answers()
				answers[id] = answer
				_ = json.NewEncoder(w).Encode(map[string]any{"answers": answers, "usage": map[string]any{"input_tokens": 10, "output_tokens": 5}})
			}))
			defer server.Close()
			result, _ := goai.Classify(&goai.ClassifierModel{ID: "jev", Api: goai.ClassifierApiTypeSafeSystemOne, BaseURL: server.URL}, issue1Context(), &goai.ClassifierOptions{APIKey: "key"})
			if result.StopReason != goai.StopReasonError || result.Usage == nil || result.Usage.TotalTokens != 15 || len(result.Answers) != 0 {
				t.Fatalf("result=%#v", result)
			}
		})
	}
}

func TestIssue1ClassifierOverflowAnswerPreservesBilledUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"usage":{"input_tokens":10,"output_tokens":5},"answers":{"bool":{"type":"noul","noul":1e400}}}`))
	}))
	defer server.Close()
	ctx := issue1Context()
	ctx.Questions = map[string]goai.ClassifierQuestion{"bool": ctx.Questions["bool"]}
	result, _ := goai.Classify(&goai.ClassifierModel{ID: "jev", Api: goai.ClassifierApiTypeSafeSystemOne, BaseURL: server.URL}, ctx, &goai.ClassifierOptions{APIKey: "key"})
	if result.StopReason != goai.StopReasonError || result.Usage == nil || result.Usage.TotalTokens != 15 || !strings.Contains(result.ErrorMessage, "invalid probability") {
		t.Fatalf("result=%#v", result)
	}
}

func TestIssue1CloudflareClassifierRejectsMalformedEnvelopes(t *testing.T) {
	for _, body := range []string{`{"answers":{}}`, `{"success":false,"result":{"state":"Completed","result":{}}}`, `{"result":{"state":"Running","result":{}}}`, `{"result":{"state":"Completed","result":[]}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		result, _ := goai.Classify(&goai.ClassifierModel{ID: "jev", Api: goai.ClassifierApiCloudflareWorkersAI, BaseURL: server.URL}, issue1Context(), &goai.ClassifierOptions{APIKey: "key"})
		server.Close()
		if result.StopReason != goai.StopReasonError {
			t.Fatalf("body=%s result=%#v", body, result)
		}
	}
}

func TestIssue1ClassifierResponseHookErrorAndEmptyUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": issue1Answers(), "usage": map[string]any{"input_tokens": 0, "output_tokens": -1}})
	}))
	defer server.Close()
	model := &goai.ClassifierModel{ID: "jev", Api: goai.ClassifierApiTypeSafeSystemOne, BaseURL: server.URL}
	result, _ := goai.Classify(model, issue1Context(), &goai.ClassifierOptions{APIKey: "key"})
	if result.StopReason != goai.StopReasonStop || result.Usage == nil || result.Usage.TotalTokens != 0 {
		t.Fatalf("result=%#v", result)
	}
	result, _ = goai.Classify(model, issue1Context(), &goai.ClassifierOptions{APIKey: "key", OnResponse: func(goai.ClassifierResponseMetadata, *goai.ClassifierModel) error { return errors.New("hook failed") }})
	if result.StopReason != goai.StopReasonError || !strings.Contains(result.ErrorMessage, "hook failed") {
		t.Fatalf("result=%#v", result)
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	result, _ = goai.Classify(model, issue1Context(), &goai.ClassifierOptions{APIKey: "key", Context: cancelCtx})
	if result.StopReason != goai.StopReasonAborted {
		t.Fatalf("result=%#v", result)
	}
}

func TestIssue1SystemOneResponseHookDispatchAfterDecodedSuccess(t *testing.T) {
	for _, api := range []goai.ClassifierApi{goai.ClassifierApiTypeSafeSystemOne, goai.ClassifierApiCloudflareWorkersAI} {
		for _, mode := range []string{"success", "malformed answers", "hook error", "http error", "invalid JSON", "trailing JSON", "read error", "oversized body", "scalar", "array", "null"} {
			t.Run(string(api)+"/"+mode, func(t *testing.T) {
				body := map[string]any{"answers": issue1Answers(), "usage": map[string]any{"input_tokens": 10, "output_tokens": 5}}
				if mode == "malformed answers" || mode == "hook error" {
					body["answers"] = map[string]any{}
				}
				if api == goai.ClassifierApiCloudflareWorkersAI {
					body = map[string]any{"success": true, "result": map[string]any{"state": "Completed", "result": body}}
				}
				data, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "invalid JSON":
					data = []byte(`{"private":"BODY_SENTINEL",`)
				case "trailing JSON":
					data = append(data, []byte(` {"private":"BODY_SENTINEL"}`)...)
				case "oversized body":
					data = append(data, []byte(strings.Repeat(" ", 1<<20))...)
				case "scalar":
					data = []byte(`"BODY_SENTINEL"`)
				case "array":
					data = []byte(`["BODY_SENTINEL"]`)
				case "null":
					data = []byte(`null`)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-Observed", "ok")
					if mode == "read error" {
						w.Header().Set("Content-Length", fmt.Sprint(len(data)+10))
					}
					if mode == "http error" {
						w.WriteHeader(http.StatusBadRequest)
					}
					_, _ = w.Write(data)
				}))
				defer server.Close()
				var hooks int
				result, err := goai.Classify(&goai.ClassifierModel{ID: "jev", Api: api, BaseURL: server.URL}, issue1Context(), &goai.ClassifierOptions{APIKey: "key", OnResponse: func(metadata goai.ClassifierResponseMetadata, _ *goai.ClassifierModel) error {
					hooks++
					if metadata.Status != http.StatusOK || metadata.Headers["X-Observed"] != "ok" {
						t.Errorf("metadata=%#v", metadata)
					}
					if mode == "hook error" {
						return errors.New("response hook failed")
					}
					return nil
				}})
				if err != nil {
					t.Fatal(err)
				}
				wantHooks := 1
				if mode == "http error" || mode == "invalid JSON" || mode == "trailing JSON" || mode == "read error" || mode == "oversized body" {
					wantHooks = 0
				}
				if hooks != wantHooks {
					t.Errorf("hooks=%d want=%d", hooks, wantHooks)
				}
				wantStop := goai.StopReasonError
				if mode == "success" {
					wantStop = goai.StopReasonStop
				}
				if result.StopReason != wantStop {
					t.Errorf("result=%#v", result)
				}
				if mode == "success" || mode == "malformed answers" {
					if result.Usage == nil || result.Usage.TotalTokens != 15 {
						t.Errorf("usage=%#v", result.Usage)
					}
				} else if result.Usage != nil {
					t.Errorf("unexpected usage=%#v", result.Usage)
				}
				if mode != "success" && len(result.Answers) != 0 {
					t.Errorf("unexpected answers=%#v", result.Answers)
				}
				if mode == "hook error" && !strings.Contains(result.ErrorMessage, "response hook failed") {
					t.Errorf("error=%q", result.ErrorMessage)
				}
				if strings.Contains(result.ErrorMessage, "BODY_SENTINEL") {
					t.Errorf("response leaked: %q", result.ErrorMessage)
				}
			})
		}
	}
}
