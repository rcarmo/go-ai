package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
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
	provider.SetRuntimeCredentials(&Credentials{Access: radiusTestToken("etag-owner")})
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

func radiusTestToken(subject string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"radius-test","sub":"` + subject + `"}`))
	return header + "." + payload + ".signature"
}

func TestRadiusModelCacheIsCredentialOwnedAndLegacyTypedRowsForce200(t *testing.T) {
	for _, tc := range []struct {
		name        string
		storedOwner string
		current     string
		legacyTyped bool
	}{
		{name: "foreign owner", storedOwner: "alice", current: "bob"},
		{name: "legacy typed lists", storedOwner: "alice", current: "alice", legacyTyped: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if got := r.Header.Get("If-None-Match"); got != "" {
					t.Errorf("cross-owner/legacy validator = %q; want none", got)
				}
				if got := r.Header.Get("If-Modified-Since"); got != "" {
					t.Errorf("cross-owner/legacy Last-Modified = %q; want none", got)
				}
				writeJSON(t, w, RadiusGatewayConfig{BaseURL: serverURL(r), Models: []RadiusGatewayModel{{ID: "fresh", Name: "Fresh", Input: []string{"text"}, ContextWindow: 10, MaxTokens: 5}}})
			}))
			t.Cleanup(server.Close)

			const id = "radius-owned-cache"
			ownerProvider := NewRadiusProvider(RadiusProviderOptions{ID: id, Gateway: server.URL, Client: server.Client()})
			ownerProvider.SetRuntimeCredentials(&Credentials{Access: radiusTestToken(tc.storedOwner)})
			storedOwner := ownerProvider.credentialOwnerFingerprint(&Credentials{Access: radiusTestToken(tc.storedOwner)})
			stored := &goai.ModelsStoreEntry{
				Models:           []*goai.Model{{Provider: id, ID: "stale"}},
				CheckedAt:        1,
				ETag:             `"stale"`,
				LastModified:     2,
				OwnerFingerprint: storedOwner,
			}
			if !tc.legacyTyped {
				stored.ImageModels = []*goai.ImageModel{}
				stored.ClassifierModels = []*goai.ClassifierModel{}
			}
			store := goai.NewInMemoryModelsStore()
			if err := store.Write(id, stored); err != nil {
				t.Fatal(err)
			}

			provider := NewRadiusProvider(RadiusProviderOptions{ID: id, Gateway: server.URL, Client: server.Client()})
			provider.SetRuntimeCredentials(&Credentials{Access: radiusTestToken(tc.current)})
			runtime := goai.NewModelRuntime(store)
			runtime.SetProvider(provider)
			if got := runtime.Refresh(t.Context(), false); got.Aborted || len(got.Errors) != 0 {
				t.Fatalf("cache-only restore: %+v", got)
			}
			if tc.current != tc.storedOwner && runtime.GetModel(id, "stale") != nil {
				t.Fatal("cache catalog crossed credential owners")
			}
			if tc.current == tc.storedOwner && runtime.GetModel(id, "stale") == nil {
				t.Fatal("owned legacy chat cache was not restored")
			}
			entry, err := provider.RefreshModelEntry(goai.ModelRefreshContext{Provider: id, Store: store, Signal: t.Context(), AllowNetwork: true})
			if err != nil {
				t.Fatal(err)
			}
			if requests != 1 || len(entry.Models) != 1 || entry.Models[0].ID != "fresh" {
				t.Fatalf("fresh owned entry = %+v, requests=%d", entry, requests)
			}
			if entry.OwnerFingerprint != provider.credentialOwnerFingerprint(&Credentials{Access: radiusTestToken(tc.current)}) {
				t.Fatalf("entry owner fingerprint = %q", entry.OwnerFingerprint)
			}
		})
	}
}

func TestRadiusCacheOwnerFingerprintDoesNotHashAccessToken(t *testing.T) {
	provider := NewRadiusProvider(RadiusProviderOptions{})
	token := radiusTestToken("account-7")
	fingerprint := provider.credentialOwnerFingerprint(&Credentials{Access: token})
	if fingerprint == "" {
		t.Fatal("JWT credential owner missing")
	}
	tokenHash := sha256.Sum256([]byte(token))
	if fingerprint == fmt.Sprintf("%x", tokenHash) || strings.Contains(fingerprint, token) {
		t.Fatal("cache fingerprint exposes an access-token hash")
	}
	if got := provider.credentialOwnerFingerprint(&Credentials{Access: "opaque-token"}); got != "" {
		t.Fatalf("opaque token unexpectedly produced owner %q", got)
	}
}

func TestRadiusTypedCatalogReplacementAndNotModified(t *testing.T) {
	var requests, decisions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer "+radiusTestToken("typed-owner") && r.Method == "POST" && r.URL.Path == "/v1/decision" {
			decisions.Add(1)
			_, _ = w.Write([]byte(`{"answers":{"approved":{"type":"bool","probability":0.91}},"usage":{"input":50,"output":1,"cacheRead":0,"cacheWrite":0,"totalTokens":51,"cost":{"input":0.00001,"output":0,"cacheRead":0,"cacheWrite":0,"total":0.00001}}}`))
			return
		}
		if r.Method != "GET" || r.URL.Path != "/v1/config" || r.Header.Get("Authorization") != "Bearer "+radiusTestToken("typed-owner") {
			t.Errorf("unexpected config request %s %s", r.Method, r.URL.Path)
			http.Error(w, "fixture route", http.StatusBadRequest)
			return
		}
		switch requests.Add(1) {
		case 1:
			w.Header().Set("ETag", `"typed"`)
			_, _ = w.Write([]byte(strings.Replace(`{"baseUrl":"https://radius.example/v1","models":[],
                "decision":[{"id":"jev-latest","name":"TypeSafe: Jev Latest","input":["text"],"contextWindow":262144,"cost":{"input":0.1,"output":0.2}}],
                "images":[{"id":"text-only-bad","name":"Not An Image Model","input":["text"],"output":["text"],"cost":{}},
                {"id":"valid-with-extra","name":"Valid Image Model","input":["text","audio","image"],"output":["image","video","text"],"cost":{"input":0.3,"output":2.5}}]}`, "https://radius.example/v1", serverURL(r), 1)))
		case 2:
			if got := r.Header.Get("If-None-Match"); got != `"typed"` {
				t.Errorf("validator = %q; want typed", got)
			}
			w.WriteHeader(http.StatusNotModified)
		case 3:
			w.Header().Set("ETag", `"replaced"`)
			_, _ = w.Write([]byte(`{"baseUrl":"https://radius.example/v1","models":[],"decision":[],"images":[]}`))
		default:
			t.Error("cache-only refresh contacted Radius")
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	provider := NewRadiusProvider(RadiusProviderOptions{ID: "radius-typed-fixture", Gateway: server.URL, Client: server.Client()})
	provider.SetRuntimeCredentials(&Credentials{Access: radiusTestToken("typed-owner")})
	store := goai.NewInMemoryModelsStore()
	runtime := goai.NewModelRuntime(store)
	runtime.SetProvider(provider)
	const id = "radius-typed-fixture"
	refresh := func(r *goai.ModelRuntime, network bool) {
		t.Helper()
		if got := r.Refresh(t.Context(), network); got.Aborted || len(got.Errors) != 0 {
			t.Fatalf("Radius refresh: %+v", got)
		}
	}
	assertTyped := func(r *goai.ModelRuntime) {
		t.Helper()
		if got := r.GetImageModel(id, "text-only-bad"); got != nil {
			t.Fatalf("image without image output was admitted: %+v", got)
		}
		got := r.GetImageModel(id, "valid-with-extra")
		if got == nil || got.Api != goai.ImageApiPiImage || !slices.Equal(got.Input, []string{"text", "image"}) || !slices.Equal(got.Output, []string{"image", "text"}) {
			t.Fatalf("filtered image = %+v", got)
		}
		got.Input[0] = "mutated"
		if again := r.GetImageModel(id, "valid-with-extra"); again.Input[0] != "text" {
			t.Fatalf("caller mutated published image: %+v", again)
		}
		decision := r.GetClassifierModel(id, "jev-latest")
		if decision == nil || decision.Api != goai.ClassifierApiPiDecision || decision.Provider != id || decision.BaseURL != server.URL+"/v1" ||
			decision.ContextWindow != 262144 || !slices.Equal(decision.Input, []string{"text"}) || decision.Cost.Input != 0.1 || decision.Cost.Output != 0.2 {
			t.Fatalf("decision lookup = %+v", decision)
		}
		if got := r.GetModels(id); len(got) != 0 {
			t.Fatalf("typed models leaked into chat: %+v", got)
		}
	}
	refresh(runtime, true)
	assertTyped(runtime)
	refresh(runtime, true)
	assertTyped(runtime)
	cached := goai.NewModelRuntime(store)
	cached.SetProvider(provider)
	refresh(cached, false)
	assertTyped(cached)
	decision, err := goai.Classify(cached.GetClassifierModel(id, "jev-latest"), goai.ClassifierContext{
		State:     map[string]any{"ok": true},
		Questions: map[string]goai.ClassifierQuestion{"approved": {Type: "bool", Instructions: "Approved?", Criteria: goai.ClassifierBoolCriteria{True: "yes", False: "no"}}},
	}, &goai.ClassifierOptions{APIKey: radiusTestToken("typed-owner"), Context: t.Context(), HTTPClient: server.Client()})
	if err != nil || decision == nil || decision.StopReason != goai.StopReasonStop || decision.Answers["approved"].Type != "bool" || decision.Answers["approved"].Probability != 0.91 || decisions.Load() != 1 {
		t.Fatalf("cached Radius decision result = %+v, %v; requests=%d", decision, err, decisions.Load())
	}
	refresh(runtime, true)
	if runtime.GetImageModel(id, "valid-with-extra") != nil || runtime.GetClassifierModel(id, "jev-latest") != nil {
		t.Fatal("empty authoritative refresh retained old typed rows")
	}
	entry, err := store.Read(id)
	if err != nil || entry == nil || entry.ImageModels == nil || len(entry.ImageModels) != 0 || entry.ClassifierModels == nil || len(entry.ClassifierModels) != 0 || entry.ETag != `"replaced"` || requests.Load() != 3 {
		t.Fatalf("replacement cache = %+v, %v; requests=%d", entry, err, requests.Load())
	}
}
