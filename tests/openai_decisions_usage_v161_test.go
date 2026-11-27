package goai_test

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const decisionsUsageStateV161 = `{"text":"The deployment succeeded, thank you."}`
const decisionsUsageAnswersV161 = `[
	{"type":"choice","name":"category","choice":"success","probabilities":[{"value":"success","probability":0.9},{"value":"failure","probability":0.1}],"confidence":0.8},
	{"type":"score","name":"satisfaction","score":1.8,"probabilities":[{"value":0,"label":"low","probability":0.05},{"value":1,"label":"neutral","probability":0.1},{"value":2,"label":"high","probability":0.85}],"confidence":0.7},
	{"type":"predicate","name":"approved","probability":0.95}
]`
const decisionsUsageCacheV161 = `{"input_tokens":2000,"input_tokens_details":{"cached_tokens":1600,"cache_write_tokens":100},"output_tokens":0,"total_tokens":2000}`

func classifyDecisionsUsageV161(t *testing.T, model *goai.ClassifierModel, status int, answers, usage string) *goai.ClassifierResult {
	t.Helper()
	input := goai.ClassifierContext{
		State: map[string]any{"text": "The deployment succeeded, thank you."},
		Questions: map[string]goai.ClassifierQuestion{
			"category":     {Type: "choice", Instructions: "Classify the message", Criteria: map[string]string{"success": "Successful", "failure": ""}},
			"satisfaction": {Type: "score", Instructions: "Score satisfaction", Criteria: []string{"low", "neutral", "high"}},
			"approved":     {Type: "bool", Instructions: "Does the user approve?", Criteria: goai.ClassifierBoolCriteria{True: "Approval", False: "No approval"}},
		},
	}
	var requests, responseHooks int
	body := fmt.Sprintf(`{"answers":%s,"usage":%s}`, answers, usage)
	client := &http.Client{Transport: &fetchOptionWire{roundTrip: func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		requests++
		if r.Method != http.MethodPost || r.URL.String() != "https://api.openai.com/v1/decisions" || r.Header.Get("Authorization") != "Bearer secret" {
			return nil, fmt.Errorf("unexpected Decisions request: %s %s", r.Method, r.URL)
		}
		var wire struct{ Model, Input string }
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			return nil, err
		}
		if wire.Model != "gpt-6-luna" || wire.Input != decisionsUsageStateV161 {
			return nil, fmt.Errorf("unexpected request body: %+v", wire)
		}
		return &http.Response{StatusCode: status, Header: http.Header{"X-Decision-Usage": {"fixture"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}}}
	zero := 0
	result, err := goai.Classify(model, input, &goai.ClassifierOptions{APIKey: "secret", Context: t.Context(), HTTPClient: client, MaxRetriesOverride: &zero,
		OnResponse: func(meta goai.ClassifierResponseMetadata, reported *goai.ClassifierModel) error {
			responseHooks++
			if meta.Status != status || meta.Headers["X-Decision-Usage"] != "fixture" || reported != model {
				return fmt.Errorf("unexpected response metadata: %+v", meta)
			}
			return nil
		},
	})
	wantHooks := 1
	if status != http.StatusOK {
		wantHooks = 0
	}
	if err != nil || result == nil || requests != 1 || responseHooks != wantHooks {
		t.Fatalf("response=%+v err=%v requests=%d response hooks=%d (want %d)", result, err, requests, responseHooks, wantHooks)
	}
	return result
}

func requireDecisionsUsageLunaV161(t *testing.T) *goai.ClassifierModel {
	t.Helper()
	goai.RegisterBuiltinClassifierModels()
	model := goai.GetClassifierModel(goai.ClassifierProviderOpenAI, "gpt-6-luna")
	if model == nil || model.Api != goai.ClassifierApiOpenAIDecisions {
		t.Fatalf("missing catalog OpenAI Decisions Luna: %+v", model)
	}
	return model
}

func requireSuccessfulDecisionsUsageV161(t *testing.T, result *goai.ClassifierResult) {
	t.Helper()
	if result.StopReason != goai.StopReasonStop || result.Answers["approved"].Probability != .95 || result.Answers["category"].Choice != "success" || result.Answers["satisfaction"].Score != 1.8 {
		t.Fatalf("unexpected classifier result: %+v", result)
	}
}

// Source: packages/ai/test/openai-decisions.test.ts:142–167.
func TestSourceV161OpenAIDecisionsCachedUsage(t *testing.T) {
	model := requireDecisionsUsageLunaV161(t)
	if model.Cost.Input != .1 || model.Cost.CacheRead != .1 || model.Cost.CacheWrite != .1 || model.Cost.Output != 0 {
		t.Fatalf("Luna catalog prices changed: %+v", model.Cost)
	}
	result := classifyDecisionsUsageV161(t, model, http.StatusOK, decisionsUsageAnswersV161, decisionsUsageCacheV161)
	requireSuccessfulDecisionsUsageV161(t, result)
	usage := result.Usage
	if usage == nil || usage.Input != 300 || usage.Output != 0 || usage.CacheRead != 1600 || usage.CacheWrite != 100 || usage.TotalTokens != 2000 {
		t.Errorf("Luna usage = %+v, want input=300 output=0 cacheRead=1600 cacheWrite=100 total=2000", usage)
	}
	if usage == nil || math.Abs(usage.Cost.Total-.0002) > 5e-13 {
		t.Errorf("Luna total cost = %+v, want .0002", usage)
	}
}

// Source: packages/ai/test/openai-decisions.test.ts:133–140. This original
// supplies its own cost and tier instead of using the built-in catalog.
func TestSourceV161OpenAIDecisionsLongContextPrice(t *testing.T) {
	model := decisionsModel110("https://api.openai.com/v1")
	model.ContextWindow = 922000
	result := classifyDecisionsUsageV161(t, model, http.StatusOK, decisionsUsageAnswersV161, `{"input_tokens":300000,"output_tokens":0}`)
	requireSuccessfulDecisionsUsageV161(t, result)
	if result.Usage == nil || math.Abs(result.Usage.Cost.Total-.06) > 5e-13 {
		t.Fatalf("long-context usage = %+v, want total .06", result.Usage)
	}
}

func TestOpenAIDecisionsUsageAccountingV161(t *testing.T) {
	for _, row := range []struct {
		name, raw                                   string
		input, output, cacheRead, cacheWrite, total int
	}{
		{"clamp-write-after-read", `{"input_tokens":100,"input_tokens_details":{"cached_tokens":60,"cache_write_tokens":70},"output_tokens":2,"total_tokens":999}`, 0, 2, 60, 40, 102},
		{"clamp-read-to-input", `{"input_tokens":100,"input_tokens_details":{"cached_tokens":110,"cache_write_tokens":20},"output_tokens":2}`, 0, 2, 100, 0, 102},
		{"ignore-invalid-details", `{"input_tokens":20,"input_tokens_details":{"cached_tokens":"10","cache_write_tokens":-2},"output_tokens":3}`, 20, 3, 0, 0, 23},
		{"tier-uses-all-input", `{"input_tokens":300000,"input_tokens_details":{"cached_tokens":250000,"cache_write_tokens":10000},"output_tokens":3}`, 40000, 3, 250000, 10000, 300003},
	} {
		t.Run(row.name, func(t *testing.T) {
			model := requireDecisionsUsageLunaV161(t)
			model.Cost = goai.ModelCost{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4,
				Tiers: []goai.ModelCostTier{{InputTokensAbove: 272000, Input: 5, Output: 6, CacheRead: 7, CacheWrite: 8}}}
			result := classifyDecisionsUsageV161(t, model, http.StatusOK, decisionsUsageAnswersV161, row.raw)
			requireSuccessfulDecisionsUsageV161(t, result)
			usage := result.Usage
			if usage == nil || usage.Input != row.input || usage.Output != row.output || usage.CacheRead != row.cacheRead || usage.CacheWrite != row.cacheWrite || usage.TotalTokens != row.total {
				t.Fatalf("usage = %+v, want input=%d output=%d read=%d write=%d total=%d", usage, row.input, row.output, row.cacheRead, row.cacheWrite, row.total)
			}
			if row.name == "tier-uses-all-input" {
				cost := usage.Cost
				if math.Abs(cost.Input-.2) > 5e-13 || math.Abs(cost.Output-.000018) > 5e-13 || math.Abs(cost.CacheRead-1.75) > 5e-13 || math.Abs(cost.CacheWrite-.08) > 5e-13 || math.Abs(cost.Total-2.030018) > 5e-13 {
					t.Errorf("tiered cache cost = %+v, want each tier component and total 2.030018", cost)
				}
			}
		})
	}
}

func TestOpenAIDecisionsUsageResponseBoundaryV161(t *testing.T) {
	model := requireDecisionsUsageLunaV161(t)
	t.Run("absent-usage", func(t *testing.T) {
		result := classifyDecisionsUsageV161(t, model, http.StatusOK, decisionsUsageAnswersV161, `null`)
		requireSuccessfulDecisionsUsageV161(t, result)
		if result.Usage != nil {
			t.Fatalf("null usage changed result: %+v", result)
		}
	})
	t.Run("answer-error-retains-billed-usage", func(t *testing.T) {
		result := classifyDecisionsUsageV161(t, model, http.StatusOK, `[]`, decisionsUsageCacheV161)
		if result.StopReason != goai.StopReasonError || len(result.Answers) != 0 || !strings.Contains(result.ErrorMessage, "did not return an answer") {
			t.Fatalf("missing answers result: %+v", result)
		}
		if result.Usage == nil || result.Usage.Input != 300 || result.Usage.CacheRead != 1600 || result.Usage.CacheWrite != 100 || result.Usage.TotalTokens != 2000 {
			t.Errorf("cached billed usage lost on answer error: %+v", result.Usage)
		}
	})
	t.Run("http-error-does-not-use-body-usage", func(t *testing.T) {
		result := classifyDecisionsUsageV161(t, model, http.StatusBadRequest, decisionsUsageAnswersV161, decisionsUsageCacheV161)
		if result.StopReason != goai.StopReasonError || result.Usage != nil || len(result.Answers) != 0 {
			t.Errorf("HTTP error treated as a billed result: %+v", result)
		}
	})
}
