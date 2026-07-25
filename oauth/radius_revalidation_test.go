package oauth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestRadiusDynamicRefreshUsesETagAndReusesCacheOnNotModified(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/config" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		requests++
		if requests == 1 {
			w.Header().Set("ETag", `"v1"`)
			w.Header().Set("Last-Modified", "Sat, 25 Jul 2026 18:00:00 GMT")
			writeJSON(t, w, RadiusGatewayConfig{BaseURL: serverURL(r), Models: []RadiusGatewayModel{{ID: "fresh", Name: "Fresh", Input: []string{"text"}, ContextWindow: 10, MaxTokens: 5}}})
			return
		}
		if r.Header.Get("If-None-Match") != `"v1"` || r.Header.Get("If-Modified-Since") == "" {
			t.Fatalf("missing validators: %#v", r.Header)
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	provider := NewRadiusProvider(RadiusProviderOptions{ID: "radius-etag", Gateway: server.URL, Client: server.Client()})
	provider.SetRuntimeCredentials(&Credentials{Access: "token"})
	store := goai.NewInMemoryModelsStore()
	first, err := provider.RefreshModelEntry(goai.ModelRefreshContext{Provider: "radius-etag", Store: store, Signal: t.Context(), AllowNetwork: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.ETag != `"v1"` || first.LastModified == 0 || len(first.Models) != 1 || first.Models[0].ID != "fresh" {
		t.Fatalf("first=%#v", first)
	}
	if err := store.Write("radius-etag", first); err != nil {
		t.Fatal(err)
	}
	second, err := provider.RefreshModelEntry(goai.ModelRefreshContext{Provider: "radius-etag", Store: store, Signal: t.Context(), AllowNetwork: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Models) != 1 || second.Models[0].ID != "fresh" || second.ETag != `"v1"` || requests != 2 {
		t.Fatalf("second=%#v requests=%d", second, requests)
	}
}

func serverURL(r *http.Request) string { return "http://" + r.Host + "/v1" }
