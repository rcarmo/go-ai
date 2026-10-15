package goai_test

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

type regionalProvider120 struct{ seen []string }

func (*regionalProvider120) ID() string                  { return "regional-refresh-120" }
func (*regionalProvider120) BaseURL() string             { return "https://regional.example.test/v1" }
func (*regionalProvider120) StaticModels() []*goai.Model { return nil }
func (p *regionalProvider120) RefreshModels(ctx goai.ModelRefreshContext) ([]*goai.Model, error) {
	p.seen = append(p.seen, ctx.BaseURL)
	return nil, nil
}

type regionalEntryProvider120 struct{ *regionalProvider120 }

func (p regionalEntryProvider120) RefreshModelEntry(ctx goai.ModelRefreshContext) (*goai.ModelsStoreEntry, error) {
	p.seen = append(p.seen, ctx.BaseURL)
	return &goai.ModelsStoreEntry{}, nil
}
func TestModelRuntime120RegisteredBaseURLReachesRefresh(t *testing.T) {
	for _, entry := range []bool{false, true} {
		t.Run(map[bool]string{false: "models", true: "entry"}[entry], func(t *testing.T) {
			p := &regionalProvider120{}
			var provider goai.DynamicModelProvider = p
			if entry {
				provider = regionalEntryProvider120{p}
			}
			runtime := goai.NewModelRuntime(nil)
			runtime.SetProvider(provider)
			for i := 0; i < 2; i++ {
				result := runtime.Refresh(context.Background(), true)
				if len(result.Errors) != 0 || result.Aborted {
					t.Fatal(result)
				}
			}
			if len(p.seen) != 2 {
				t.Fatal("refresh calls", p.seen)
			}
			for _, url := range p.seen {
				if url != "https://regional.example.test/v1" {
					t.Fatal("registered endpoint lost", p.seen)
				}
			}
		})
	}
}
