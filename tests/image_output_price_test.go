package goai_test

import (
	"math"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
)

func TestOpenRouterImageTokenSplit(t *testing.T) {
	model := openRouterImageFixtureModel("google/gemini-3-pro-image", goai.ModelCost{Input: 2, Output: 12, ImageOutput: new(120.0), CacheRead: .2, CacheWrite: .375})
	got, _ := openRouterImageFixtureCall(t, model, "A compass rose", `{"id":"gen-reasoning-1","usage":{"prompt_tokens":15,"completion_tokens":2290,"completion_tokens_details":{"reasoning_tokens":1000,"image_tokens":1120}},"choices":[{"finish_reason":"stop","message":{"content":null,"images":[{"image_url":"data:image/png;base64,ZmFrZS1wbmc="}]}}]}`, &images.ImagesOptions{APIKey: "test"})
	if got.Usage == nil {
		t.Fatalf("result=%+v", got)
	}
	if got.Usage.Output != 2290 || got.Usage.Reasoning != 1000 || got.Usage.ImageOutput != 1120 || !got.Usage.ImageOutputSet {
		t.Errorf("token split=%+v", got.Usage)
	}
	if math.Abs(got.Usage.Cost.Output-(1120*120+1170*12)/1_000_000.0) > 1e-9 {
		t.Errorf("split cost=%+v", got.Usage.Cost)
	}
	if got.Usage.Cost.TotalFromProvider == nil || *got.Usage.Cost.TotalFromProvider {
		t.Errorf("cost provenance=%+v", got.Usage.Cost)
	}
}

func TestImageOutputPrices(t *testing.T) {
	model := &goai.Model{ID: "google/gemini-3-pro-image", Provider: "openrouter", Cost: goai.ModelCost{Input: 2, Output: 12, ImageOutput: new(120.0), CacheRead: .2, CacheWrite: .375}}
	for _, row := range []struct {
		name    string
		tokens  int
		present bool
		want    float64
	}{
		{"split", 1120, true, .13742},
		{"omitted", 0, false, .1644},
		{"over-output", 5000, true, .1644},
	} {
		t.Run(row.name, func(t *testing.T) {
			usage := &goai.Usage{Input: 10, Output: 1370, Reasoning: 250, ImageOutput: row.tokens, ImageOutputSet: row.present, TotalTokens: 1380}
			cost := goai.CalculateCost(model, usage)
			got := cost.Output
			if row.name == "split" {
				got = cost.Total
			}
			if math.Abs(got-row.want) > 1e-9 {
				t.Errorf("image output price=%v; want %v", got, row.want)
			}
		})
	}
	model.Cost.ImageOutput = nil
	if got := goai.CalculateCost(model, &goai.Usage{Output: 1370, ImageOutput: 1120, ImageOutputSet: true}).Output; math.Abs(got-.01644) > 1e-9 {
		t.Errorf("legacy output price=%v; want 0.01644", got)
	}
}

func TestImageModelCopiesOwnOptionalOutputRate(t *testing.T) {
	model := &goai.Model{Cost: goai.ModelCost{ImageOutput: new(120.0)}}
	provider := goai.StaticModelProvider{Models: []*goai.Model{model}}
	first := provider.StaticModels()[0]
	*first.Cost.ImageOutput = 0
	if got := *provider.StaticModels()[0].Cost.ImageOutput; got != 120 {
		t.Fatalf("mutating a detached price changed the source model: %v", got)
	}
}
