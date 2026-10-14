package oauth

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenRouter120RuntimePerKeyIsolationAndClassifierPreservation(t *testing.T) {
	goai.RegisterBuiltinModels()
	baseline := goai.ListModels(goai.ProviderOpenRouter)
	if len(baseline) < 2 {
		t.Fatal("baseline missing")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := baseline[0].ID
		if r.Header.Get("Authorization") == "Bearer second" {
			id = baseline[1].ID
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": id}}})
	}))
	defer server.Close()
	first, e := RuntimeForAPIKeyContext(context.Background(), "openrouter", "first", server.URL, server.Client())
	if e != nil {
		t.Fatal(e)
	}
	second, e := RuntimeForAPIKeyContext(context.Background(), "openrouter", "second", server.URL, server.Client())
	if e != nil {
		t.Fatal(e)
	}
	if len(first.Models) != 1 || len(second.Models) != 1 || first.Models[0].ID == second.Models[0].ID {
		t.Fatal("credentials mixed", first.Models, second.Models)
	}
	if len(first.ClassifierModels) == 0 || len(first.ClassifierModels) != len(second.ClassifierModels) {
		t.Fatal("nonchat catalog lost")
	}
	store := goai.NewInMemoryModelsStore()
	options := goai.OpenRouterCatalogOptions{BaseURL: server.URL + "/regional", Client: server.Client(), Store: store, AllowNetwork: true}
	credential := &Credentials{Access: "first"}
	live, e := RuntimeForProviderWithCatalog(context.Background(), "openrouter", credential, options)
	if e != nil || len(live.Models) != 1 {
		t.Fatal("OAuth regional admission", live, e)
	}
	options.AllowNetwork = false
	cached, e := RuntimeForProviderWithCatalog(context.Background(), "openrouter", credential, options)
	if e != nil || len(cached.Models) != 1 || cached.Models[0].ID != live.Models[0].ID {
		t.Fatal("OAuth offline catalog", cached, e)
	}
	other, e := RuntimeForProviderWithCatalog(context.Background(), "openrouter", &Credentials{Access: "second"}, options)
	if e != nil || len(other.Models) != len(baseline) {
		t.Fatal("OAuth offline key scope leak", other, e)
	}
	if len(goai.ListModels(goai.ProviderOpenRouter)) != len(baseline) {
		t.Fatal("global catalog mutated")
	}
}
