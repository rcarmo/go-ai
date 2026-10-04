package goai_test

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func lookupIssue2Model(id string) *goai.Model {
	enabled := true
	return &goai.Model{
		ID: id, Provider: "lookup-issue2", Name: id, Api: goai.ApiOpenAICompletions,
		Input: []string{"text", "image"}, Headers: map[string]string{"X-Test": "original"},
		InputLimits: &goai.ModelInputLimits{Images: &goai.ModelImageInputLimits{Resize: &goai.ModelImageResizeOptions{MaxWidth: 640}}},
		PromptCache: &goai.ModelPromptCache{Short: 300}, Enabled: &enabled,
		Providers: []goai.ModelProviderInfo{{ID: "original"}},
	}
}

func lookupIssue2Runtime(size int) *goai.ModelRuntime {
	models := make([]*goai.Model, size)
	for i := range models {
		models[i] = lookupIssue2Model(fmt.Sprintf("model-%d", i))
	}
	r := goai.NewModelRuntime(nil)
	r.SetProvider(goai.StaticModelProvider{Provider: "lookup-issue2", Models: models})
	return r
}

func TestModelRuntimeLookupIssue2MatchOnlyAndDetached(t *testing.T) {
	r := lookupIssue2Runtime(3)
	for _, provider := range []goai.Provider{"lookup-issue2", ""} {
		for _, id := range []string{"model-0", "model-1", "model-2"} {
			got := r.GetModel(provider, id)
			if got == nil || !reflect.DeepEqual(got, lookupIssue2Model(id)) {
				t.Fatalf("lookup %q/%q got %#v", provider, id, got)
			}
			got.Input[0] = "mutated"
			got.Headers["X-Test"] = "mutated"
			got.InputLimits.Images.Resize.MaxWidth = 1
			got.PromptCache.Short = 1
			*got.Enabled = false
			got.Providers[0].ID = "mutated"
			if again := r.GetModel(provider, id); !reflect.DeepEqual(again, lookupIssue2Model(id)) {
				t.Fatalf("returned model aliases owned clone fields: %#v", again)
			}
		}
		if got := r.GetModel(provider, "missing"); got != nil {
			t.Fatalf("missing model returned %#v", got)
		}
	}
	if got := r.GetModel("unknown", "model-1"); got != nil {
		t.Fatalf("unknown provider returned %#v", got)
	}
	other := lookupIssue2Model("other-unique")
	other.Provider = "other"
	r.SetProvider(goai.StaticModelProvider{Provider: "other", Models: []*goai.Model{other}})
	if got := r.GetModel("", other.ID); got == nil || got.Provider != "other" {
		t.Fatalf("empty-provider lookup omitted second provider: %#v", got)
	}
	if got := r.GetModel("lookup-issue2", other.ID); got != nil {
		t.Fatalf("provider-specific lookup leaked other provider: %#v", got)
	}
}

func TestModelRuntimeLookupIssue2NilEntriesAndFirstMatch(t *testing.T) {
	r := goai.NewModelRuntime(nil)
	first, second := lookupIssue2Model("same"), lookupIssue2Model("same")
	first.Name, second.Name = "first", "second"
	r.SetProvider(goai.StaticModelProvider{Provider: "lookup-issue2", Models: []*goai.Model{nil, first, nil, second}})
	for _, provider := range []goai.Provider{"lookup-issue2", ""} {
		got := r.GetModel(provider, "same")
		if got == nil || got.Name != "first" {
			t.Fatalf("first match changed: %#v", got)
		}
		if r.GetModel(provider, "missing") != nil {
			t.Fatal("nil entries produced a match")
		}
	}
}

var lookupIssue2Sink *goai.Model

func TestModelRuntimeLookupIssue2AllocationsIndependentOfCatalogue(t *testing.T) {
	small, large := lookupIssue2Runtime(1), lookupIssue2Runtime(512)
	for _, provider := range []goai.Provider{"lookup-issue2", ""} {
		hit := func(r *goai.ModelRuntime, id string) float64 {
			return testing.AllocsPerRun(100, func() { lookupIssue2Sink = r.GetModel(provider, id) })
		}
		a, b := hit(small, "model-0"), hit(large, "model-511")
		if a != b {
			t.Fatalf("lookup allocations grow with catalogue: provider%q small%.0f large%.0f", provider, a, b)
		}
		miss := testing.AllocsPerRun(100, func() { lookupIssue2Sink = large.GetModel(provider, "missing") })
		if miss != 0 {
			t.Fatalf("missing lookup clones catalogue: %.0f allocations", miss)
		}
	}
}

func TestModelRuntimeLookupIssue2ConcurrentReplacementAndRefresh(t *testing.T) {
	r := lookupIssue2Runtime(8)
	ctx := context.Background()
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 150; i++ {
				for _, provider := range []goai.Provider{"lookup-issue2", ""} {
					got := r.GetModel(provider, "model-0")
					if got == nil || got.ID != "model-0" || got.Provider != "lookup-issue2" {
						t.Errorf("inconsistent match %#v", got)
						return
					}
					got.Headers["X-Test"] = "caller mutation"
					got.InputLimits.Images.Resize.MaxWidth = 1
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			r.SetProvider(goai.StaticModelProvider{Provider: "lookup-issue2", Models: []*goai.Model{lookupIssue2Model("model-0"), lookupIssue2Model("model-1")}})
			result := r.Refresh(ctx, true)
			if result.Aborted || len(result.Errors) != 0 {
				t.Errorf("refresh failed %#v", result)
				return
			}
		}
	}()
	wg.Wait()
	if got := r.GetModel("lookup-issue2", "model-0"); got.Headers["X-Test"] != "original" || got.InputLimits.Images.Resize.MaxWidth != 640 {
		t.Fatal("lookup mutation escaped")
	}
}

func BenchmarkModelRuntimeLookupIssue2(b *testing.B) {
	for _, size := range []int{1, 512} {
		r := lookupIssue2Runtime(size)
		for _, provider := range []goai.Provider{"lookup-issue2", ""} {
			for _, id := range []string{fmt.Sprintf("model-%d", size-1), "missing"} {
				name := fmt.Sprintf("size%d/provider%q/%s", size, provider, id)
				b.Run(name, func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						lookupIssue2Sink = r.GetModel(provider, id)
					}
				})
			}
		}
	}
}
