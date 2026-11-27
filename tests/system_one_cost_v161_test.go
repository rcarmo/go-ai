package goai_test

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV161SystemOneReportedCost(t *testing.T) {
	const state = "The deployment succeeded, thank you."
	const recordedID = "gen-dec-1795523851-QIfD0w71U1hnQQndirYC"
	const answers = `{"category":{"type":"choice","choice":"success","probabilities":{"success":0.9,"failure":0.1},"confidence":0.8},"satisfaction":{"type":"score","score":2,"confidence":0.7},"approved":{"type":"noul","noul":0.95}}`
	const router = "https://openrouter.ai/api/v1"
	const direct = "https://api.typesafe.ai/v1"
	const details = `"cost_details":{"upstream_inference_cost":0.00002,"upstream_inference_prompt_cost":0.00002,"upstream_inference_completions_cost":0}`
	ctx := goai.ClassifierContext{State: map[string]any{"text": state}, Questions: map[string]goai.ClassifierQuestion{
		"category":     {Type: "choice", Instructions: "Classify the message", Criteria: map[string]string{"success": "Successful", "failure": "Failed"}},
		"satisfaction": {Type: "score", Instructions: "Score satisfaction", Criteria: []string{"low", "neutral", "high"}},
		"approved":     {Type: "bool", Instructions: "Does the user approve?", Criteria: goai.ClassifierBoolCriteria{True: "Approval", False: "No approval"}},
	}}
	for _, tc := range []struct {
		name, base, id, modelID, usage string
		provider                       goai.ClassifierProvider
		input, output                  int
		inputRate, total               float64
		billed                         bool
	}{
		{"published-openrouter-shape", router, recordedID, "typesafe/jev-1.13", `{"input_tokens":304,"output_tokens":20,"cost":0.000012768,"is_byok":false,"cost_details":{"upstream_inference_cost":0.000012768,"upstream_inference_prompt_cost":0.000012768,"upstream_inference_completions_cost":0}}`, goai.ClassifierProviderOpenRouter, 304, 20, .042, .000012768, true},
		{"non-byok", router, "gen-dec-2", "typesafe/jev-1.13", `{"input_tokens":308,"output_tokens":23,"cost":0.00002,"is_byok":false}`, goai.ClassifierProviderOpenRouter, 308, 23, .042, .00002, true},
		{"missing-byok", router, "gen-dec-2", "typesafe/jev-1.13", `{"input_tokens":308,"output_tokens":23,"cost":0.00002}`, goai.ClassifierProviderOpenRouter, 308, 23, .042, .000012936, false},
		{"byok", router, "gen-dec-2", "typesafe/jev-1.13", `{"input_tokens":308,"output_tokens":23,"cost":0.000001,"is_byok":true,` + details + `}`, goai.ClassifierProviderOpenRouter, 308, 23, .042, .000021, true},
		{"native", direct, "gen-dec-2", "jev-latest", `{"input_tokens":308,"output_tokens":23,"cost":0.00002,"is_byok":false}`, goai.ClassifierProviderTypeSafe, 308, 23, 0, 0, false},
		{"provider-id-override", "https://override.invalid/api/v1", "gen-dec-2", "typesafe/jev-1.13", `{"input_tokens":308,"output_tokens":23,"cost":0.00002,"is_byok":false}`, goai.ClassifierProviderOpenRouter, 308, 23, .042, .00002, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &goai.ClassifierModel{ID: tc.modelID, Api: goai.ClassifierApiTypeSafeSystemOne, Provider: tc.provider, BaseURL: tc.base, Input: []string{"text"}, Cost: goai.ModelCost{Input: tc.inputRate}}
			response := `{"id":` + fmt.Sprintf("%q", tc.id) + `,"provider":"TypeSafe","model":"typesafe/jev-1.14-20261014","answers":` + answers + `,"usage":` + tc.usage + `}`
			requests := 0
			transport := providerRetryTransportMain(func(req *http.Request) (*http.Response, error) {
				defer req.Body.Close()
				requests++
				if req.Method != http.MethodPost || req.URL.String() != tc.base+"/systemone" || req.Header.Get("Authorization") != "Bearer test-key" {
					return nil, fmt.Errorf("unexpected classifier request %s %s", req.Method, req.URL)
				}
				var body struct {
					Model string
					State map[string]any
				}
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					return nil, err
				}
				if body.Model != tc.modelID || !reflect.DeepEqual(body.State, ctx.State) {
					return nil, fmt.Errorf("unexpected request body %+v", body)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
			})
			out, err := goai.Classify(model, ctx, &goai.ClassifierOptions{APIKey: "test-key", HTTPClient: &http.Client{Transport: transport}})
			if err != nil || out == nil || out.StopReason != goai.StopReasonStop || requests != 1 {
				t.Fatalf("classifier = %#v, %v; requests %d", out, err, requests)
			}
			approved, ok := out.Answers["approved"]
			if !ok || approved.Type != "bool" || approved.Probability != .95 {
				t.Errorf("approved = %#v", out.Answers["approved"])
			}
			if out.ResponseID != tc.id {
				t.Errorf("response ID = %q; want %q", out.ResponseID, tc.id)
			}
			if out.Usage == nil || out.Usage.Input != tc.input || out.Usage.Output != tc.output || out.Usage.TotalTokens != tc.input+tc.output {
				t.Fatalf("usage = %#v", out.Usage)
			}
			if math.Abs(out.Usage.Cost.Total-tc.total) > 5e-13 {
				t.Errorf("total = %.12g; want %.12g", out.Usage.Cost.Total, tc.total)
			}
			if out.Usage.Cost.TotalFromProvider == nil || *out.Usage.Cost.TotalFromProvider != tc.billed {
				t.Errorf("billing provenance = %v; want explicit %v", out.Usage.Cost.TotalFromProvider, tc.billed)
			}
			if math.Abs(out.Usage.Cost.Input-float64(tc.input)*tc.inputRate/1e6) > 5e-13 {
				t.Errorf("input cost overwritten: %v", out.Usage.Cost.Input)
			}
		})
	}
}
