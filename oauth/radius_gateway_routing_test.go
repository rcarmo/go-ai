package oauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRadiusOAuthUsesGatewayEndpointsDirectly(t *testing.T) {
	var urls []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		urls = append(urls, r.URL.Path)
		switch r.URL.Path {
		case "/v1/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("client_id") != "pi-gateway" || r.Form.Get("refresh_token") != "old" {
				t.Fatalf("unexpected form: %#v", r.Form)
			}
			writeJSON(t, w, map[string]interface{}{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600})
		case "/v1/config":
			writeJSON(t, w, RadiusGatewayConfig{BaseURL: server.URL + "/v1", Models: []RadiusGatewayModel{{ID: "m", Name: "M", Input: []string{"text"}, ContextWindow: 1, MaxTokens: 1}}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	provider := NewRadiusProvider(RadiusProviderOptions{Gateway: server.URL, Client: server.Client()})
	creds, err := provider.RefreshToken(&Credentials{Refresh: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if creds.Access != "access" || len(urls) != 2 || urls[0] != "/v1/oauth/token" || urls[1] != "/v1/config" {
		t.Fatalf("creds=%#v urls=%#v", creds, urls)
	}
}
