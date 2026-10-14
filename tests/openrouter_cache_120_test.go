package goai_test

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenRouter120OfflineCredentialEndpointIsolationAndExpansion(t *testing.T) {
	store := goai.NewInMemoryModelsStore()
	baseline := []*goai.Model{{ID: "a", Provider: goai.ProviderOpenRouter}, {ID: "b", Provider: goai.ProviderOpenRouter}}
	expanded := false
	failed := false
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if failed {
			w.WriteHeader(500)
			return
		}
		ids := []any{map[string]any{"id": "a"}}
		if expanded {
			ids = append(ids, map[string]any{"id": "b"})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": ids})
	}))
	defer server.Close()
	opts := goai.OpenRouterCatalogOptions{BaseURL: server.URL + "/region", Client: server.Client(), Store: store, AllowNetwork: true}
	first, e := goai.ResolveOpenRouterCatalog(context.Background(), "key-one", baseline, opts)
	if e != nil || len(first) != 1 {
		t.Fatal(first, e)
	}
	first[0].Name = "mutated"
	opts.AllowNetwork = false
	cached, e := goai.ResolveOpenRouterCatalog(context.Background(), "key-one", baseline, opts)
	if e != nil || len(cached) != 1 || cached[0].Name == "mutated" || calls != 1 {
		t.Fatal(cached, e, calls)
	}
	other, e := goai.ResolveOpenRouterCatalog(context.Background(), "key-two", baseline, opts)
	if e != nil || len(other) != 2 {
		t.Fatal("key scope leaked", other, e)
	}
	opts.BaseURL = server.URL + "/other-region"
	other, e = goai.ResolveOpenRouterCatalog(context.Background(), "key-one", baseline, opts)
	if e != nil || len(other) != 2 {
		t.Fatal("region scope leaked", other, e)
	}
	opts.BaseURL = server.URL + "/region"
	opts.AllowNetwork = true
	expanded = true
	next, e := goai.ResolveOpenRouterCatalog(context.Background(), "key-one", baseline, opts)
	if e != nil || len(next) != 2 {
		t.Fatal("cached filter prevented expansion", next, e)
	}
	failed = true
	if _, e = goai.ResolveOpenRouterCatalog(context.Background(), "key-one", baseline, opts); e == nil {
		t.Fatal("server error suppressed")
	}
	opts.AllowNetwork = false
	cached, e = goai.ResolveOpenRouterCatalog(context.Background(), "key-one", baseline, opts)
	if e != nil || len(cached) != 2 {
		t.Fatal("failed refresh destroyed cache", cached, e)
	}
}
