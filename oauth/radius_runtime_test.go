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
		if got := r.Header.Get("Authorization"); got != "Bearer radius-key" {
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
	provider.SetRuntimeCredentials(&Credentials{Access: "radius-key"})
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
		&images.ImagesOptions{APIKey: "radius-key", Context: t.Context(), HTTPClient: gateway.Client(), MaxRetriesSet: true})
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
