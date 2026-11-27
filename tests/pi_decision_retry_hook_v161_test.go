package goai_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type observedPiDecisionBodyV161 struct {
	reader io.Reader
	status int
	order  *[]string
	read   bool
}

func (b *observedPiDecisionBodyV161) Read(out []byte) (int, error) {
	if !b.read {
		*b.order = append(*b.order, fmt.Sprintf("read:%d", b.status))
		b.read = true
	}
	return b.reader.Read(out)
}

func (b *observedPiDecisionBodyV161) Close() error {
	*b.order = append(*b.order, fmt.Sprintf("close:%d", b.status))
	return nil
}

// Supplemental production-path evidence for the pinned classifier-shared.ts
// retry loop; pi-decision.test.ts directly checks only the final error hook.
func TestPiDecisionRetryResponseHookV161(t *testing.T) {
	for _, name := range []string{"retry-success", "hook-refusal", "retry-shaped-refusal"} {
		refuse := name != "retry-success"
		t.Run(name, func(t *testing.T) {
			model, ctx := piDecisionFixtureV161()
			var order []string
			var first []byte
			requests := 0
			one := 1
			opts := &goai.ClassifierOptions{APIKey: "radius-key", Context: t.Context(), MaxRetriesOverride: &one}
			opts.OnResponse = func(meta goai.ClassifierResponseMetadata, hookModel *goai.ClassifierModel) error {
				if hookModel != model || meta.Headers["Retry-After-Ms"] != "0" {
					t.Errorf("unexpected hook metadata %+v", meta)
				}
				order = append(order, fmt.Sprintf("hook:%d", meta.Status))
				if name == "retry-shaped-refusal" {
					return &goai.ProviderRequestError{Status: 503, Message: "fixture refused headers"}
				}
				if refuse {
					return errors.New("fixture refused headers")
				}
				return nil
			}
			opts.HTTPClient = &http.Client{Transport: &fetchOptionWire{roundTrip: func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				requests++
				if requests > 2 || r.Method != http.MethodPost || r.URL.String() != "https://radius.example/v1/decision" {
					return nil, fmt.Errorf("unexpected scripted request %d: %s %s", requests, r.Method, r.URL)
				}
				data, err := io.ReadAll(r.Body)
				if err != nil {
					return nil, err
				}
				status, body := 503, "busy"
				if requests == 1 {
					first = data
				} else {
					if !slices.Equal(first, data) {
						return nil, errors.New("retry did not replay the same decision request")
					}
					status, body = 200, piDecisionResponseV161
				}
				order = append(order, fmt.Sprintf("request:%d", status))
				return &http.Response{StatusCode: status, Header: http.Header{"Retry-After-Ms": {"0"}},
					Body: &observedPiDecisionBodyV161{reader: strings.NewReader(body), status: status, order: &order}}, nil
			}}}
			result := requirePiDecisionResultV161(t, model, ctx, opts)
			want := []string{"request:503", "hook:503", "read:503", "close:503", "request:200", "hook:200", "read:200", "close:200"}
			if refuse {
				want = []string{"request:503", "hook:503", "close:503"}
				if result.StopReason != goai.StopReasonError || !strings.Contains(result.ErrorMessage, "fixture refused headers") || requests != 1 {
					t.Errorf("refused hook result=%+v requests=%d, want one failed attempt", result, requests)
				}
			} else if result.StopReason != goai.StopReasonStop || result.ResponseID != "gen-dec-1" || result.Answers["approved"].Probability != 0.96 || requests != 2 {
				t.Errorf("retry result=%+v requests=%d, want successful second attempt", result, requests)
			}
			if !slices.Equal(order, want) {
				t.Errorf("response order = %v, want %v", order, want)
			}
		})
	}
}
