package mistral

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMistral120TimeoutOnlyBoundsHeaders(t *testing.T) {
	for _, mode := range []string{"slow-body", "slow-headers", "caller-cancel"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				_ = r.Body.Close()
				if mode == "slow-headers" {
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				if mode == "caller-cancel" {
					<-r.Context().Done()
					return
				}
				time.Sleep(80 * time.Millisecond)
				w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
			}))
			defer server.Close()
			timeout := 30
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if mode == "caller-cancel" {
				time.AfterFunc(70*time.Millisecond, cancel)
			}
			events := streamMistral(ctx, &goai.Model{ID: "test", Api: goai.ApiMistralConversations, Provider: "mistral", BaseURL: server.URL}, &goai.Context{}, &goai.StreamOptions{APIKey: "test", TimeoutMs: &timeout})
			done, failed := false, false
			for event := range events {
				switch e := event.(type) {
				case *goai.DoneEvent:
					done = true
					if mode != "slow-body" {
						t.Fatal("unexpected completion", mode)
					}
				case *goai.ErrorEvent:
					failed = true
					if mode == "slow-body" {
						t.Fatal("body cancelled by header timeout", e.Err)
					}
					if mode == "slow-headers" && (e.Err == nil || !strings.Contains(e.Err.Error(), "response headers timed out")) {
						t.Fatal("timeout diagnostic lost", e.Err)
					}
				}
			}
			if mode == "slow-body" && !done || mode != "slow-body" && !failed {
				t.Fatal("missing terminal", mode, done, failed)
			}
		})
	}
}
