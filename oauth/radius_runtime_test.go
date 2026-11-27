package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
	_ "github.com/rcarmo/go-ai/images/piimage"
)

func TestDynamicModelCatalogPresenceIncludesTypedRows(t *testing.T) {
	for _, entry := range []*goai.ModelsStoreEntry{
		{ImageModels: []*goai.ImageModel{{ID: "image"}}},
		{ClassifierModels: []*goai.ClassifierModel{{ID: "decision"}}},
	} {
		if !hasDynamicModelCatalog(radiusStaticEntryProvider{entry: entry}) {
			t.Fatalf("typed-only entry was treated as missing: %+v", entry)
		}
	}
	if hasDynamicModelCatalog(radiusStaticEntryProvider{entry: &goai.ModelsStoreEntry{}}) {
		t.Fatal("empty entry was treated as a discovered catalog")
	}
}

type radiusStaticEntryProvider struct{ entry *goai.ModelsStoreEntry }

func (radiusStaticEntryProvider) ID() string                                 { return "radius-static-entry" }
func (radiusStaticEntryProvider) StaticModels() []*goai.Model                { return nil }
func (p radiusStaticEntryProvider) StaticModelEntry() *goai.ModelsStoreEntry { return p.entry }
func (radiusStaticEntryProvider) RefreshModels(goai.ModelRefreshContext) ([]*goai.Model, error) {
	return nil, nil
}

func TestRadiusRuntimeRefreshUpdatesPackageLookups(t *testing.T) {
	goai.ClearModels()
	t.Cleanup(goai.RegisterBuiltinModels)

	gateway := newMutableRadiusGateway(t)
	provider := NewRadiusProvider(RadiusProviderOptions{ID: "radius-runtime", Gateway: gateway.URL, Client: gateway.Client()})
	RegisterProvider(provider)

	if _, err := RuntimeForProvider("radius-runtime", &Credentials{Refresh: "old-refresh"}); err != nil {
		t.Fatalf("runtime credentials: %v", err)
	}
	if model := goai.GetModel("radius-runtime", "radius-a"); model == nil || model.BaseURL != gateway.URL+"/v1" {
		t.Fatalf("radius-a not visible through package lookup: %#v", model)
	}
	if got := len(goai.ListModels("radius-runtime")); got != 1 {
		t.Fatalf("expected one Radius model, got %d", got)
	}

	gateway.setModels([]RadiusGatewayModel{{ID: "radius-b", Name: "Radius B", Input: []string{"text"}, Cost: goai.ModelCost{Input: 2, Output: 3}, ContextWindow: 2000, MaxTokens: 200}})
	result := goai.RefreshModels(t.Context(), true)
	if result.Aborted || len(result.Errors) != 0 {
		t.Fatalf("refresh result: %#v", result)
	}
	if goai.GetModel("radius-runtime", "radius-a") != nil {
		t.Fatal("old Radius model should be replaced after refresh")
	}
	if model := goai.GetModel("radius-runtime", "radius-b"); model == nil || model.Cost.Input != 2 {
		t.Fatalf("radius-b not visible through package lookup: %#v", model)
	}
}

func TestRadiusRuntimeFailedAndOfflineRefreshRetainsCache(t *testing.T) {
	goai.ClearModels()
	t.Cleanup(goai.RegisterBuiltinModels)

	gateway := newMutableRadiusGateway(t)
	provider := NewRadiusProvider(RadiusProviderOptions{ID: "radius-cache", Gateway: gateway.URL, Client: gateway.Client()})
	RegisterProvider(provider)
	if _, err := RuntimeForProvider("radius-cache", &Credentials{Refresh: "old-refresh"}); err != nil {
		t.Fatalf("runtime credentials: %v", err)
	}
	if goai.GetModel("radius-cache", "radius-a") == nil {
		t.Fatal("radius-a missing before failure")
	}

	gateway.failConfig.Store(true)
	failed := goai.RefreshModels(t.Context(), true)
	if failed.Errors["radius-cache"] == nil {
		t.Fatalf("expected Radius refresh error: %#v", failed)
	}
	if goai.GetModel("radius-cache", "radius-a") == nil {
		t.Fatal("cached Radius model should remain after failed network refresh")
	}

	gateway.setModels([]RadiusGatewayModel{{ID: "radius-c", Name: "Radius C", Input: []string{"text"}, Cost: goai.ModelCost{Input: 4, Output: 5}, ContextWindow: 3000, MaxTokens: 300}})
	offline := goai.RefreshModels(t.Context(), false)
	if offline.Aborted || len(offline.Errors) != 0 {
		t.Fatalf("offline refresh result: %#v", offline)
	}
	if goai.GetModel("radius-cache", "radius-a") == nil || goai.GetModel("radius-cache", "radius-c") != nil {
		t.Fatalf("offline refresh should retain cached radius-a only: %#v", goai.ListModels("radius-cache"))
	}
}

type mutableRadiusGateway struct {
	*httptest.Server
	failConfig atomic.Bool
	models     atomic.Value // []RadiusGatewayModel
}

func newMutableRadiusGateway(t *testing.T) *mutableRadiusGateway {
	t.Helper()
	gateway := &mutableRadiusGateway{}
	gateway.setModels([]RadiusGatewayModel{{ID: "radius-a", Name: "Radius A", Input: []string{"text"}, Cost: goai.ModelCost{Input: 1, Output: 2}, ContextWindow: 1000, MaxTokens: 100}})
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/oauth", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]interface{}{"issuer": gateway.URL, "authorizationEndpoint": gateway.URL + "/authorize", "tokenEndpoint": gateway.URL + "/token", "deviceAuthorizationEndpoint": gateway.URL + "/device", "verificationEndpoint": gateway.URL + "/verify", "clientId": "radius-client", "scope": "openid offline_access radius", "deviceCodeGrantType": radiusDeviceGrant})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		body, _ := url.ParseQuery(readBody(t, r))
		if body.Get("grant_type") != "refresh_token" || body.Get("refresh_token") == "" {
			t.Fatalf("unexpected token form: %#v", body)
		}
		writeJSON(t, w, map[string]interface{}{"access_token": "access-1", "refresh_token": "refresh-1", "expires_in": 3600, "scope": "radius"})
	})
	mux.HandleFunc("/v1/config", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-1" {
			t.Fatalf("authorization=%q", got)
		}
		if gateway.failConfig.Load() {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("temporary"))
			return
		}
		writeJSON(t, w, RadiusGatewayConfig{BaseURL: gateway.URL + "/v1", Models: gateway.models.Load().([]RadiusGatewayModel)})
	})
	gateway.Server = httptest.NewServer(mux)
	t.Cleanup(gateway.Close)
	return gateway
}

func (g *mutableRadiusGateway) setModels(models []RadiusGatewayModel) { g.models.Store(models) }

// Adapt the Radius provider image original's fixture to an isolated loopback
// gateway. Only the URL changes; the config and output bodies retain the
// original records and image response.
func newRadiusImageGateway(t *testing.T) (*httptest.Server, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	config, err := os.ReadFile("testdata/radius-image-config.json")
	if err != nil {
		t.Fatal(err)
	}
	response, err := os.ReadFile("testdata/radius-image-response.json")
	if err != nil {
		t.Fatal(err)
	}
	var configs, images atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+radiusTestToken("image-owner") {
			t.Errorf("unexpected synthetic bearer header %q", got)
			http.Error(w, "fixture authorization failed", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/config":
			configs.Add(1)
			if got := r.Header.Get("Accept"); got != "application/json" {
				t.Errorf("config Accept = %q; want application/json", got)
			}
			_, _ = w.Write([]byte(strings.Replace(string(config), "https://radius.example/v1", "http://"+r.Host+"/v1", 1)))
		case "POST /v1/image":
			images.Add(1)
			var request any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode image request: %v", err)
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			want := map[string]any{"model": "nano-banana", "context": map[string]any{"input": []any{map[string]any{"type": "text", "text": "A fox"}}}}
			if !reflect.DeepEqual(request, want) {
				t.Errorf("image request = %#v; want %#v", request, want)
			}
			_, _ = w.Write(response)
		default:
			t.Errorf("unexpected Radius request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &configs, &images
}

func refreshRadiusImageFixture(t *testing.T, gateway *httptest.Server) *goai.InMemoryModelsStore {
	t.Helper()
	provider := NewRadiusProvider(RadiusProviderOptions{ID: "radius-image-fixture", Gateway: gateway.URL, Client: gateway.Client()})
	provider.SetRuntimeCredentials(&Credentials{Access: radiusTestToken("image-owner")})
	store := goai.NewInMemoryModelsStore()
	read := func(allowNetwork bool) {
		t.Helper()
		runtime := goai.NewModelRuntime(store)
		runtime.SetProvider(provider)
		if got := runtime.Refresh(t.Context(), allowNetwork); got.Aborted || len(got.Errors) != 0 {
			t.Fatalf("Radius refresh: %+v", got)
		}
		var ids []string
		for _, model := range runtime.GetModels("radius-image-fixture") {
			if model.Api != goai.ApiPiMessages || model.BaseURL != gateway.URL+"/v1" || model.Provider != "radius-image-fixture" {
				t.Fatalf("unexpected chat route: %+v", model)
			}
			ids = append(ids, model.ID)
		}
		if !reflect.DeepEqual(ids, []string{"balanced", "organization-only"}) {
			t.Fatalf("Radius chat models = %v; want exactly balanced, organization-only", ids)
		}
	}
	read(true)
	read(false)
	return store
}

// This exact image request/result is a supplemental adapter control. The
// explicit model is not a native mapping of Radius's typed discovery.
func TestRadiusRuntimeImageFixturePreservesChatAndExplicitRoute(t *testing.T) {
	gateway, configs, imageRequests := newRadiusImageGateway(t)
	refreshRadiusImageFixture(t, gateway)
	model := &images.ImagesModel{ID: "nano-banana", Name: "Nano Banana", Api: images.ImagesApiPiImage,
		Provider: images.ImagesProviderRadius, BaseURL: gateway.URL + "/v1", Input: []string{"text", "image"}, Output: []string{"image", "text"},
		Cost: goai.ModelCost{Input: 0.3, Output: 2.5, CacheRead: 0.03, CacheWrite: 0.083333}}
	result, err := images.GenerateImages(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "A fox"}}},
		&images.ImagesOptions{APIKey: radiusTestToken("image-owner"), Context: t.Context(), HTTPClient: gateway.Client(), MaxRetriesSet: true})
	if err != nil || result == nil || result.StopReason != goai.StopReasonStop || result.ResponseID != "gen-img-radius-1" || result.ErrorMessage != "" {
		t.Fatalf("Radius Pi Image result = %+v, %v; want gen-img-radius-1 success", result, err)
	}
	want := []images.ImageOutput{{Type: "image", Data: "iVBORw0KGgo=", MimeType: "image/png"}}
	if !reflect.DeepEqual(result.Output, want) {
		t.Fatalf("Radius image output = %+v; want %+v", result.Output, want)
	}
	if configs.Load() != 1 || imageRequests.Load() != 1 {
		t.Fatalf("requests: config=%d image=%d; want one of each and no network refresh of the cache", configs.Load(), imageRequests.Load())
	}
}

func TestRadiusRuntimeDetachesCredentialTypedCatalog(t *testing.T) {
	thinking := "medium"
	cfg := &RadiusGatewayConfig{
		BaseURL: "https://radius.example/v1",
		Models: []RadiusGatewayModel{{ID: "chat", Name: "Chat", Input: []string{"text"}, ContextWindow: 1, MaxTokens: 1,
			ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMedium): &thinking},
			Cost:             goai.ModelCost{ImageOutput: new(2.0), Tiers: []goai.ModelCostTier{{Output: 3}}}}},
		Images:   []RadiusGatewayImage{{ID: "image", Name: "Image", Input: []string{"text"}, Output: []string{"image"}, Cost: goai.ModelCost{ImageOutput: new(4.0)}}},
		Decision: []RadiusGatewayDecision{{ID: "decision", Name: "Decision", Input: []string{"text"}, ContextWindow: 5, Cost: goai.ModelCost{Tiers: []goai.ModelCostTier{{Output: 6}}}}},
	}
	provider := NewRadiusProvider(RadiusProviderOptions{ID: "radius-detached"})
	provider.SetRuntimeCredentials(&Credentials{Extra: map[string]interface{}{"gatewayConfig": cfg}})
	cfg.Models[0].Input[0] = "mutated"
	cfg.Models[0].Cost.Tiers[0].Output = 0
	*cfg.Models[0].ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingMedium)] = "low"
	cfg.Images[0].Input[0] = "mutated"
	*cfg.Images[0].Cost.ImageOutput = 0
	cfg.Decision[0].Cost.Tiers[0].Output = 0

	runtime := goai.NewModelRuntime(nil)
	runtime.SetProvider(provider)
	chat := runtime.GetModel("radius-detached", "chat")
	image := runtime.GetImageModel("radius-detached", "image")
	decision := runtime.GetClassifierModel("radius-detached", "decision")
	if chat == nil || chat.Input[0] != "text" || chat.Cost.Tiers[0].Output != 3 || *chat.Cost.ImageOutput != 2 || *chat.ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingMedium)] != "medium" {
		t.Fatalf("credential chat snapshot leaked: %+v", chat)
	}
	if image == nil || image.Input[0] != "text" || *image.Cost.ImageOutput != 4 {
		t.Fatalf("credential image snapshot leaked: %+v", image)
	}
	if decision == nil || decision.Cost.Tiers[0].Output != 6 {
		t.Fatalf("credential decision snapshot leaked: %+v", decision)
	}
	chat.Input[0] = "reader"
	image.Input[0] = "reader"
	decision.Input[0] = "reader"
	if runtime.GetModel("radius-detached", "chat").Input[0] != "text" || runtime.GetImageModel("radius-detached", "image").Input[0] != "text" || runtime.GetClassifierModel("radius-detached", "decision").Input[0] != "text" {
		t.Fatal("runtime returned mutable typed snapshot fields")
	}
}

func TestRadiusRuntimeDiscoversImageRouteFromCache(t *testing.T) {
	gateway, configs, imageRequests := newRadiusImageGateway(t)
	provider := NewRadiusProvider(RadiusProviderOptions{ID: "radius-image-discovery", Gateway: gateway.URL, Client: gateway.Client()})
	provider.SetRuntimeCredentials(&Credentials{Access: radiusTestToken("image-owner")})
	store := goai.NewInMemoryModelsStore()
	live := goai.NewModelRuntime(store)
	live.SetProvider(provider)
	if got := live.Refresh(t.Context(), true); got.Aborted || len(got.Errors) != 0 {
		t.Fatalf("live Radius refresh: %+v", got)
	}
	const id = "radius-image-discovery"
	want := &goai.ImageModel{ID: "nano-banana", Name: "Nano Banana", Type: "image", Api: goai.ImageApiPiImage,
		Provider: id, BaseURL: gateway.URL + "/v1", Input: []string{"text", "image"}, Output: []string{"image", "text"},
		Cost: goai.ModelCost{Input: 0.3, Output: 2.5, CacheRead: 0.03, CacheWrite: 0.083333}}
	if got := live.GetImageModel(id, "nano-banana"); !reflect.DeepEqual(got, want) {
		t.Fatalf("live typed model = %+v; want %+v", got, want)
	}
	var ids []string
	for _, model := range live.GetModels(id) {
		ids = append(ids, model.ID)
	}
	if !reflect.DeepEqual(ids, []string{"balanced", "organization-only"}) {
		t.Fatalf("live chat IDs = %v", ids)
	}
	cached := goai.NewModelRuntime(store)
	cachedProvider := NewRadiusProvider(RadiusProviderOptions{ID: id, Gateway: gateway.URL, Client: gateway.Client()})
	cachedProvider.SetRuntimeCredentials(&Credentials{Access: radiusTestToken("image-owner")})
	cached.SetProvider(cachedProvider)
	if got := cached.Refresh(t.Context(), false); got.Aborted || len(got.Errors) != 0 {
		t.Fatalf("cache-only Radius refresh: %+v", got)
	}
	if got := cached.GetImageModel(id, "nano-banana"); !reflect.DeepEqual(got, want) {
		t.Fatalf("cached typed model = %+v; want %+v", got, want)
	}

	// Exercise both public compatibility lookups over the same installed runtime.
	if err := goai.RegisterRuntimeModels(cached); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = goai.RegisterRuntimeModels(goai.NewModelRuntime(nil))
		goai.RegisterBuiltinModels()
	})
	if got := goai.GetImageModel(id, "nano-banana"); !reflect.DeepEqual(got, want) {
		t.Fatalf("public image lookup = %+v; want %+v", got, want)
	}
	model := images.GetImageModel(id, "nano-banana")
	if model == nil || model.Api != images.ImagesApiPiImage || model.Provider != id || !reflect.DeepEqual(model.Output, want.Output) {
		t.Fatalf("compatibility lookup = %+v", model)
	}
	result, err := images.GenerateImages(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "A fox"}}},
		&images.ImagesOptions{APIKey: radiusTestToken("image-owner"), Context: t.Context(), HTTPClient: gateway.Client(), MaxRetriesSet: true})
	if err != nil || result == nil || result.StopReason != goai.StopReasonStop || result.ResponseID != "gen-img-radius-1" || result.ErrorMessage != "" {
		t.Fatalf("discovered Radius image result = %+v, %v; want gen-img-radius-1 success", result, err)
	}
	wantOutput := []images.ImageOutput{{Type: "image", Data: "iVBORw0KGgo=", MimeType: "image/png"}}
	if !reflect.DeepEqual(result.Output, wantOutput) {
		t.Fatalf("discovered Radius image output = %+v; want %+v", result.Output, wantOutput)
	}
	if configs.Load() != 1 || imageRequests.Load() != 1 {
		t.Fatalf("requests: config=%d image=%d; want one of each", configs.Load(), imageRequests.Load())
	}
}
