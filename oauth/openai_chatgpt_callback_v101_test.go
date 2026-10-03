package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestV101ChatGPTPublicLoginOccupied1455BeforeCallbacks(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:1455")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var exchanges atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { exchanges.Add(1) }))
	defer server.Close()
	p := NewOpenAIChatGPTProvider(server.Client())
	p.tokenURL = server.URL
	p.deviceID = "11111111-1111-4111-8111-111111111111"
	callbacks := 0
	_, err = p.Login(LoginCallbacks{OnAuth: func(AuthInfo) { callbacks++ }, OnPrompt: func(Prompt) (string, error) { callbacks++; return "", nil }, OnProgress: func(string) { callbacks++ }})
	if err == nil || !strings.Contains(err.Error(), "1455") || !strings.Contains(err.Error(), "Codex") || callbacks != 0 || exchanges.Load() != 0 {
		t.Fatalf("err=%v callbacks=%d exchanges=%d", err, callbacks, exchanges.Load())
	}
}
func TestV101ChatGPTBindErrorsNoFallback(t *testing.T) {
	p := NewOpenAIChatGPTProvider(nil)
	p.callbackHost = "invalid.invalid.invalid"
	p.deviceID = "11111111-1111-4111-8111-111111111111"
	callbacks := 0
	_, err := p.Login(LoginCallbacks{OnAuth: func(AuthInfo) { callbacks++ }, OnPrompt: func(Prompt) (string, error) { callbacks++; return "", nil }})
	if err == nil || callbacks != 0 {
		t.Fatalf("err=%v callbacks=%d", err, callbacks)
	}
}
func TestV101ChatGPTCallbackWinsManualAndClosesIdleConnections(t *testing.T) {
	var exchanges atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "refresh_token": "refresh", "id_token": "id", "scope": openAIChatGPTScope, "expires_in": 3600})
	}))
	defer server.Close()
	p := NewOpenAIChatGPTProvider(server.Client())
	p.tokenURL = server.URL
	p.callbackPort = 0
	p.deviceID = "11111111-1111-4111-8111-111111111111"
	ready := make(chan AuthInfo, 1)
	manualCleaned := make(chan struct{})
	promptEntered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := p.Login(LoginCallbacks{OnAuth: func(info AuthInfo) { ready <- info }, OnPromptContext: func(ctx context.Context, _ Prompt) (string, error) {
			defer close(manualCleaned)
			close(promptEntered)
			<-ctx.Done()
			return "", ctx.Err()
		}})
		done <- err
	}()
	info := <-ready
	u, _ := url.Parse(info.URL)
	redirect, _ := url.Parse(u.Query().Get("redirect_uri"))
	idle, err := net.Dial("tcp", redirect.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	<-promptEntered
	redirect.RawQuery = url.Values{"code": {"code"}, "client_id": {"issued"}, "state": {u.Query().Get("state")}}.Encode()
	resp, err := http.Get(redirect.String())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("callback did not win")
	}
	select {
	case <-manualCleaned:
	default:
		t.Fatal("login returned before context prompt cleanup")
	}
	if exchanges.Load() != 1 {
		t.Fatalf("exchanges=%d", exchanges.Load())
	}
	_ = idle.SetReadDeadline(time.Now().Add(time.Second))
	var one [1]byte
	if _, err := idle.Read(one[:]); err != io.EOF {
		t.Fatalf("idle connection not closed: %v", err)
	}
	rebound, err := net.Listen("tcp", redirect.Host)
	if err != nil {
		t.Fatalf("listener leaked: %v", err)
	}
	rebound.Close()
}
func TestV101ChatGPTCancellationClosesAcceptedConnection(t *testing.T) {
	p := NewOpenAIChatGPTProvider(nil)
	p.callbackPort = 0
	p.deviceID = "11111111-1111-4111-8111-111111111111"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan AuthInfo, 1)
	promptEntered := make(chan struct{})
	promptCleaned := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := p.loginContext(ctx, LoginCallbacks{OnAuth: func(info AuthInfo) { ready <- info }, OnPromptContext: func(ctx context.Context, _ Prompt) (string, error) {
			defer close(promptCleaned)
			close(promptEntered)
			<-ctx.Done()
			return "", ctx.Err()
		}})
		done <- err
	}()
	info := <-ready
	u, _ := url.Parse(info.URL)
	redirect, _ := url.Parse(u.Query().Get("redirect_uri"))
	idle, err := net.Dial("tcp", redirect.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	<-promptEntered
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel succeeded")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("cancel stuck")
	}
	select {
	case <-promptCleaned:
	default:
		t.Fatal("cancel returned before prompt cleanup")
	}
	_ = idle.SetReadDeadline(time.Now().Add(time.Second))
	var one [1]byte
	if _, err := idle.Read(one[:]); err != io.EOF {
		t.Fatalf("cancel idle connection=%v", err)
	}
	rebound, err := net.Listen("tcp", redirect.Host)
	if err != nil {
		t.Fatalf("cancel listener leaked: %v", err)
	}
	rebound.Close()
}
