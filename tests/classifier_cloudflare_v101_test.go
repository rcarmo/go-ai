package goai_test

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestV101CloudflareDirectClassifiersAndMalformedUsage(t *testing.T) {
	for _, id := range []string{"@cf/cloudflare/clef", "@cf/cloudflare/clef-flash"} {
		for _, mode := range []string{"direct", "nested", "null-answers", "success-false"} {
			t.Run(id+"/"+mode, func(t *testing.T) {
				hooks := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var p map[string]any
					_ = json.NewDecoder(r.Body).Decode(&p)
					if r.URL.Path != "/run" || p["model"] != id || p["input"] == nil {
						t.Errorf("request=%s %#v", r.URL, p)
					}
					result := map[string]any{"answers": issue1Answers(), "usage": map[string]any{"input_tokens": 1000000, "output_tokens": 0}}
					if mode == "null-answers" {
						result["answers"] = nil
						result["state"] = "Completed"
						result["result"] = map[string]any{"answers": issue1Answers()}
					}
					if mode == "nested" {
						result = map[string]any{"state": "Completed", "result": result}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"success": mode != "success-false", "result": result})
				}))
				defer server.Close()
				cost := 0.24
				if strings.HasSuffix(id, "-flash") {
					cost = 0.09
				}
				model := &goai.ClassifierModel{ID: id, Api: goai.ClassifierApiCloudflareWorkersAI, Provider: goai.ClassifierProviderCloudflareWorkersAI, BaseURL: server.URL, Cost: goai.ModelCost{Input: cost}}
				result, err := goai.Classify(model, issue1Context(), &goai.ClassifierOptions{APIKey: "test", OnResponse: func(goai.ClassifierResponseMetadata, *goai.ClassifierModel) error { hooks++; return nil }})
				if err != nil || hooks != 1 {
					t.Fatalf("err=%v hooks=%d", err, hooks)
				}
				if mode == "success-false" {
					if result.StopReason != goai.StopReasonError || result.Usage != nil {
						t.Fatalf("result=%#v", result)
					}
					return
				}
				if result.Usage == nil || result.Usage.Cost.Input != cost {
					t.Fatalf("usage=%#v", result.Usage)
				}
				if mode == "null-answers" {
					if result.StopReason != goai.StopReasonError || len(result.Answers) != 0 {
						t.Fatalf("null result=%#v", result)
					}
				} else if result.StopReason != goai.StopReasonStop || len(result.Answers) != 3 {
					t.Fatalf("result=%#v", result)
				}
			})
		}
	}
}
