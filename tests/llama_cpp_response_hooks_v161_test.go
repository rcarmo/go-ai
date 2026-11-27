package goai_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

var llamaResponseFixtureIDV161 atomic.Uint64

type unreadableLlamaResponseV161 struct{ reads, closes int }

func (b *unreadableLlamaResponseV161) Read([]byte) (int, error) {
	b.reads++
	return 0, errors.New("broken fixture body")
}
func (b *unreadableLlamaResponseV161) Close() error { b.closes++; return nil }

// Source: pi v1.6.1 llama-cpp-classify.test.ts:325–374. The unreadable
// and rejected-body rows separately exercise the header phase.
func TestSourceV161LlamaCPPResponseHooks(t *testing.T) {
	for _, tc := range []struct {
		name       string
		maxRetries int
		statuses   []int
		stop       goai.StopReason
	}{
		{"success", 0, []int{200}, goai.StopReasonStop},
		{"failed-retry", 1, []int{503, 503}, goai.StopReasonError},
		{"unreadable-body", 0, []int{503}, goai.StopReasonError},
		{"hook-rejection", 1, []int{503}, goai.StopReasonError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := fmt.Sprintf("http://llama-%d.test:9931", llamaResponseFixtureIDV161.Add(1))
			model := &goai.ClassifierModel{Type: "classifier", Api: goai.ClassifierApiLlamaCPP, Provider: "llama.cpp", ID: "qwen",
				Name: "qwen", BaseURL: base + "/v1", Input: []string{"text"}, ContextWindow: 32768}
			ctx := goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{
				"pick": {Type: "choice", Instructions: "Pick", Criteria: map[string]string{"a": "", "b": ""}},
			}}
			if tc.name != "success" {
				ctx = goai.ClassifierContext{
					State: map[string]any{"message": "Help! My payouts have been failing for 3 days."},
					Questions: map[string]goai.ClassifierQuestion{
						"team": {Type: "choice", Instructions: "Which team should handle this?",
							Criteria: map[string]string{"billing": "Payments and refunds", "technical": "Bugs and outages", "sales": ""}},
						"urgent":   {Type: "bool", Instructions: "Does this convey urgency?", Criteria: goai.ClassifierBoolCriteria{True: "The user needs help soon", False: "No time pressure"}},
						"severity": {Type: "score", Instructions: "How severe is this?", Criteria: []string{"low", "medium", "high"}},
					},
				}
			}
			var responses, observedAt []int
			var paths []string
			var preflight, completions int
			var firstCompletion []byte
			var payloads []map[string]any
			var sentIDSlot any
			broken := new(unreadableLlamaResponseV161)
			opts := &goai.ClassifierOptions{Context: t.Context(), MaxRetriesOverride: &tc.maxRetries}
			opts.OnResponse = func(response goai.ClassifierResponseMetadata, m *goai.ClassifierModel) error {
				responses = append(responses, response.Status)
				observedAt = append(observedAt, completions)
				if m != model || (response.Status == 503 && response.Headers["Retry-After-Ms"] != "0") {
					t.Errorf("unexpected completion hook metadata: %+v", response)
				}
				if broken.reads != 0 {
					t.Error("response hook ran after reading the body")
				}
				if tc.name == "hook-rejection" {
					return errors.New("fixture refused response")
				}
				return nil
			}
			if tc.name == "success" {
				opts.OnPayload = func(payload map[string]any, _ *goai.ClassifierModel) (map[string]any, error) {
					payloads = append(payloads, payload)
					transformed := maps.Clone(payload)
					transformed["id_slot"] = 1
					return transformed, nil
				}
			}
			opts.HTTPClient = &http.Client{Transport: &fetchOptionWire{roundTrip: func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				path := r.URL.Path
				if r.Method != http.MethodPost || r.URL.String() != base+path {
					return nil, fmt.Errorf("unexpected llama.cpp request %s %s", r.Method, r.URL)
				}
				paths = append(paths, path)
				encoded, err := io.ReadAll(r.Body)
				if err != nil {
					return nil, err
				}
				var payload map[string]any
				if err := json.Unmarshal(encoded, &payload); err != nil {
					return nil, err
				}
				resp := &http.Response{StatusCode: 200, Header: make(http.Header)}
				switch path {
				case "/tokenize":
					preflight++
					tokens, _ := json.Marshal([]rune(payload["content"].(string)))
					resp.Body = io.NopCloser(strings.NewReader(`{"tokens":` + string(tokens) + `}`))
				case "/apply-template":
					preflight++
					resp.Body = io.NopCloser(strings.NewReader(`{"prompt":"PROMPT\n"}`))
				case "/completion":
					completions++
					if completions == 1 {
						firstCompletion = encoded
					} else if !slices.Equal(firstCompletion, encoded) {
						return nil, errors.New("completion retry did not replay the request body")
					}
					sentIDSlot = payload["id_slot"]
					resp.Body = io.NopCloser(strings.NewReader(`{"completion_probabilities":[{"top_logprobs":[{"id":65,"logprob":-0.1},{"id":66,"logprob":-2.5}]}]}`))
					if tc.name != "success" {
						resp.StatusCode = 503
						resp.Header.Set("Retry-After-Ms", "0")
						resp.Body = io.NopCloser(strings.NewReader("busy"))
						if tc.name == "unreadable-body" || tc.name == "hook-rejection" {
							resp.Body = broken
						}
					}
				default:
					return nil, fmt.Errorf("unexpected llama.cpp endpoint %s", path)
				}
				return resp, nil
			}}}
			result, err := goai.Classify(model, ctx, opts)
			if err != nil || result == nil {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if result.StopReason != tc.stop {
				t.Errorf("stopReason=%q error=%q, want %q", result.StopReason, result.ErrorMessage, tc.stop)
			}
			if !slices.Equal(responses, tc.statuses) {
				t.Errorf("response hook statuses=%v, want %v", responses, tc.statuses)
			}
			if preflight == 0 || completions != len(tc.statuses) {
				t.Errorf("paths=%v, want preflight and %d completions", paths, len(tc.statuses))
			}
			for i, at := range observedAt {
				if at != i+1 {
					t.Errorf("hook %d came after completion attempt %d, want %d", i, at, i+1)
				}
			}
			if tc.name == "success" {
				if len(payloads) != 1 {
					t.Fatalf("payload hooks=%d, want one", len(payloads))
				}
				if payloads[0]["n_predict"] != 1 {
					t.Errorf("payload n_predict=%v, want 1", payloads[0]["n_predict"])
				}
				if sentIDSlot != float64(1) {
					t.Errorf("request id_slot=%v, want 1", sentIDSlot)
				}
			}
			if tc.name == "unreadable-body" && (broken.reads != 1 || broken.closes != 1) {
				t.Errorf("unreadable body read/close=%d/%d, want 1/1", broken.reads, broken.closes)
			}
			if tc.name == "hook-rejection" && (broken.reads != 0 || broken.closes != 1 || !strings.Contains(result.ErrorMessage, "fixture refused response")) {
				t.Errorf("rejected hook read/close=%d/%d result=%+v", broken.reads, broken.closes, result)
			}
		})
	}
}
