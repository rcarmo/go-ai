package goai_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func piDecisionFixtureV161() (*goai.ClassifierModel, goai.ClassifierContext) {
	return &goai.ClassifierModel{Type: "classifier", ID: "jev-latest", Name: "Jev Latest", Api: goai.ClassifierApiPiDecision,
		Provider: "radius", BaseURL: "https://radius.example/v1", Input: []string{"text"}, ContextWindow: 262144,
		Cost: goai.ModelCost{Input: 0.5, Output: 1.5}}, goai.ClassifierContext{
		State: map[string]any{"message": "Looks good, ship it."},
		Questions: map[string]goai.ClassifierQuestion{
			"approved":  {Type: "bool", Instructions: "Does the user approve?", Criteria: goai.ClassifierBoolCriteria{True: "Approved", False: "Not approved"}},
			"sentiment": {Type: "choice", Instructions: "What is the sentiment?", Criteria: map[string]string{"positive": "Positive", "negative": "Negative"}},
			"urgency":   {Type: "score", Instructions: "How urgent?", Criteria: []string{"low", "medium", "high"}},
		},
	}
}

const piDecisionAnswersV161 = `{"approved":{"type":"bool","probability":0.96},"sentiment":{"type":"choice","choice":"positive","probabilities":{"positive":0.95,"negative":0.05},"confidence":0.9},"urgency":{"type":"score","score":0.4,"confidence":0.8}}`
const piDecisionResponseV161 = `{"answers":` + piDecisionAnswersV161 + `,"usage":{"input":120,"output":3,"cacheRead":0,"cacheWrite":0,"totalTokens":123,"cost":{"input":0.0001,"output":0.00001,"cacheRead":0,"cacheWrite":0,"total":0.00011}},"responseId":"gen-dec-1"}`

type piDecisionWireV161 struct {
	calls       int
	method, url string
	body        map[string]any
}

func (w *piDecisionWireV161) options(t *testing.T, status int, headers http.Header, response io.ReadCloser) *goai.ClassifierOptions {
	t.Helper()
	zero := 0
	return &goai.ClassifierOptions{APIKey: "radius-key", Context: t.Context(), MaxRetriesOverride: &zero,
		HTTPClient: &http.Client{Transport: &fetchOptionWire{roundTrip: func(req *http.Request) (*http.Response, error) {
			defer req.Body.Close()
			w.calls++
			w.method, w.url = req.Method, req.URL.String()
			if req.Header.Get("Authorization") != "Bearer radius-key" || req.Header.Get("Content-Type") != "application/json" {
				return nil, errors.New("wrong scripted Pi Decision request headers")
			}
			if err := json.NewDecoder(req.Body).Decode(&w.body); err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: status, Header: headers, Body: response}, nil
		}}}}
}

func requirePiDecisionResultV161(t *testing.T, model *goai.ClassifierModel, ctx goai.ClassifierContext, options *goai.ClassifierOptions) *goai.ClassifierResult {
	t.Helper()
	result, err := goai.Classify(model, ctx, options)
	if err != nil || result == nil {
		t.Fatalf("Classify = %+v, %v; want result", result, err)
	}
	return result
}

func piDecisionRequestV161(t *testing.T, image bool, session *string) map[string]any {
	t.Helper()
	const canonical = `{"model":"jev-latest","context":{"state":{"message":"Looks good, ship it."},"questions":{"approved":{"type":"bool","instructions":"Does the user approve?","criteria":{"true":"Approved","false":"Not approved"}},"sentiment":{"type":"choice","instructions":"What is the sentiment?","criteria":{"positive":"Positive","negative":"Negative"}},"urgency":{"type":"score","instructions":"How urgent?","criteria":["low","medium","high"]}}}}`
	var expected map[string]any
	if err := json.Unmarshal([]byte(canonical), &expected); err != nil {
		t.Fatal(err)
	}
	if image {
		expected["context"].(map[string]any)["images"] = []any{map[string]any{"type": "image", "data": "AAAA", "mimeType": "image/png"}}
	}
	if session != nil {
		expected["options"] = map[string]any{"sessionId": *session}
	}
	return expected
}

// Source: pi v1.6.1 pi-decision.test.ts:39–111.
func TestSourceV161PiDecisionCanonical(t *testing.T) {
	model, ctx := piDecisionFixtureV161()
	var wire piDecisionWireV161
	session := "sess-1"
	opts := wire.options(t, 200, nil, io.NopCloser(strings.NewReader(piDecisionResponseV161)))
	opts.SessionID = &session
	result := requirePiDecisionResultV161(t, model, ctx, opts)
	if wire.calls != 1 {
		t.Errorf("transport calls=%d, want 1", wire.calls)
	}
	if wire.url != "https://radius.example/v1/decision" {
		t.Errorf("request URL=%q", wire.url)
	}
	if wire.method != http.MethodPost {
		t.Errorf("request method=%q, want POST", wire.method)
	}
	wantPayload := piDecisionRequestV161(t, false, &session)
	if !reflect.DeepEqual(wire.body, wantPayload) {
		t.Errorf("canonical payload=%#v, want %#v", wire.body, wantPayload)
	}
	wantAnswers := map[string]goai.ClassifierAnswer{
		"approved":  {Type: "bool", Probability: 0.96},
		"sentiment": {Type: "choice", Choice: "positive", Probabilities: map[string]float64{"positive": 0.95, "negative": 0.05}, Confidence: 0.9},
		"urgency":   {Type: "score", Score: 0.4, Confidence: 0.8},
	}
	wantUsage := &goai.Usage{Input: 120, Output: 3, TotalTokens: 123, Cost: goai.CostBreakdown{Input: 0.0001, Output: 0.00001, Total: 0.00011}}
	if result.Api != "pi-decision" || result.Provider != "radius" || result.Model != "jev-latest" || result.StopReason != goai.StopReasonStop ||
		result.ResponseID != "gen-dec-1" || !reflect.DeepEqual(result.Answers, wantAnswers) || !reflect.DeepEqual(result.Usage, wantUsage) {
		t.Errorf("canonical result=%+v usage=%+v, want typed answers and authoritative usage=%+v", result, result.Usage, wantUsage)
	}
}

func TestSourceV161PiDecisionErrorEnvelope(t *testing.T) {
	model, ctx := piDecisionFixtureV161()
	var wire piDecisionWireV161
	result := requirePiDecisionResultV161(t, model, ctx, wire.options(t, 402, nil,
		io.NopCloser(strings.NewReader(`{"error":{"message":"Insufficient credits","code":"insufficient_credits"}}`))))
	if result.StopReason != goai.StopReasonError {
		t.Errorf("stopReason=%q, want error", result.StopReason)
	}
	if result.Answers == nil || len(result.Answers) != 0 {
		t.Errorf("answers=%+v, want empty map", result.Answers)
	}
	if !strings.Contains(result.ErrorMessage, "Insufficient credits") {
		t.Errorf("errorMessage=%q, want provider explanation", result.ErrorMessage)
	}
}

type piDecisionUnreadableV161 struct{ read bool }

func (r *piDecisionUnreadableV161) Read([]byte) (int, error) {
	r.read = true
	return 0, errors.New("body stream failed")
}
func (*piDecisionUnreadableV161) Close() error { return nil }

func TestSourceV161PiDecisionErrorHook(t *testing.T) {
	model, ctx := piDecisionFixtureV161()
	var wire piDecisionWireV161
	body := new(piDecisionUnreadableV161)
	opts := wire.options(t, 503, http.Header{"Retry-After": {"7"}}, body)
	var responses []goai.ClassifierResponseMetadata
	opts.OnResponse = func(response goai.ClassifierResponseMetadata, source *goai.ClassifierModel) error {
		if body.read || source != model {
			t.Error("response hook must precede the read and receive the same model")
		}
		responses = append(responses, response)
		return nil
	}
	result := requirePiDecisionResultV161(t, model, ctx, opts)
	if result.StopReason != goai.StopReasonError || !body.read {
		t.Errorf("stopReason=%q read=%t, want error and attempted read", result.StopReason, body.read)
	}
	if len(responses) != 1 {
		t.Fatalf("response hooks=%v, want one", responses)
	}
	if responses[0].Status != 503 || responses[0].Headers["Retry-After"] != "7" {
		t.Errorf("response metadata=%+v, want 503 and Retry-After 7", responses[0])
	}
}

func TestSourceV161PiDecisionAnswerValidation(t *testing.T) {
	for _, row := range []struct{ name, old, replacement, diagnostic string }{
		{"unknown-choice", `"choice":"positive"`, `"choice":"invented"`, `unknown choice "invented"`},
		{"out-of-range-probability", `"probability":0.96`, `"probability":1.5`, `out-of-range probability`},
		{"prototype-choice", `"choice":"positive"`, `"choice":"toString"`, `unknown choice "toString"`},
		{"unknown-probability-key", `"negative":0.05`, `"bogus":0.05`, `unknown choice "bogus"`},
	} {
		t.Run(row.name, func(t *testing.T) {
			model, ctx := piDecisionFixtureV161()
			var wire piDecisionWireV161
			body := strings.Replace(piDecisionResponseV161, row.old, row.replacement, 1)
			result := requirePiDecisionResultV161(t, model, ctx, wire.options(t, 200, nil, io.NopCloser(strings.NewReader(body))))
			if result.StopReason != goai.StopReasonError {
				t.Errorf("stopReason=%q, want error", result.StopReason)
			}
			if !strings.Contains(result.ErrorMessage, row.diagnostic) {
				t.Errorf("errorMessage=%q, want %q", result.ErrorMessage, row.diagnostic)
			}
		})
	}
}

func TestSourceV161PiDecisionImageInput(t *testing.T) {
	model, ctx := piDecisionFixtureV161()
	imageModel := *model
	imageModel.Input = []string{"text", "image"}
	ctx.Images = []goai.ImageContent{{Type: "image", Data: "AAAA", MimeType: "image/png"}}
	var wire piDecisionWireV161
	opts := wire.options(t, 200, nil, io.NopCloser(strings.NewReader(`{"answers":`+piDecisionAnswersV161+`}`)))
	result := requirePiDecisionResultV161(t, &imageModel, ctx, opts)
	if result.StopReason != goai.StopReasonStop {
		t.Errorf("image stopReason=%q error=%q", result.StopReason, result.ErrorMessage)
	}
	if wire.calls != 1 {
		t.Errorf("image transport calls=%d, want one", wire.calls)
	}
	wantImagePayload := piDecisionRequestV161(t, true, nil)
	if !reflect.DeepEqual(wire.body, wantImagePayload) {
		t.Errorf("image payload=%#v, want %#v", wire.body, wantImagePayload)
	}
	result = requirePiDecisionResultV161(t, model, ctx, opts)
	if result.StopReason != goai.StopReasonError {
		t.Errorf("text-only stopReason=%q, want error", result.StopReason)
	}
	if !strings.Contains(result.ErrorMessage, "does not support image input") {
		t.Errorf("text-only error=%q", result.ErrorMessage)
	}
	if wire.calls != 1 {
		t.Errorf("after text-only rejection, transport calls=%d, want one", wire.calls)
	}
}
