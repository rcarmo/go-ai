package oauth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
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
