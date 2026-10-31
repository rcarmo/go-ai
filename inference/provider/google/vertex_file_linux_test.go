//go:build linux

package google

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Exercise the interval after path Stat but before Open. A cancelled reader
// must not block in FIFO Open; uncancelled readers must reject the opened
// non-regular object. If unchanged code blocks, an owned FIFO peer releases it
// and the stream/collector are joined before the test reports its failure.
func TestVertexCredentialStatOpenSwap(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancelled=%t", cancelled), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "credentials.json")
			if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			swapped, opened := make(chan struct{}), make(chan struct{})
			saved := openVertexCredentialFile
			openVertexCredentialFile = func(name string) (*os.File, error) {
				if err := os.Remove(name); err != nil {
					return nil, err
				}
				if err := syscall.Mkfifo(name, 0600); err != nil {
					return nil, err
				}
				if cancelled {
					cancel()
				}
				close(swapped)
				file, err := saved(name)
				close(opened)
				return file, err
			}
			t.Cleanup(func() { openVertexCredentialFile = saved })
			var inferenceCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { inferenceCalls.Add(1); w.WriteHeader(500) }))
			defer server.Close()
			model := &goai.Model{ID: "gemini", Api: goai.ApiGoogleVertex, Provider: goai.ProviderGoogleVertex, BaseURL: server.URL}
			opts := &goai.StreamOptions{APIKey: "<authenticated>", Project: "fixture-project", Location: "global", Env: goai.ProviderEnv{"GOOGLE_APPLICATION_CREDENTIALS": path}}
			stream := goai.Stream(ctx, model, &goai.Context{Messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "hello"}}}}}, opts)
			type result struct {
				errors, done int
				failure      *goai.ErrorEvent
			}
			finished := make(chan result, 1)
			go func() {
				var r result
				for event := range stream {
					switch e := event.(type) {
					case *goai.ErrorEvent:
						r.errors++
						r.failure = e
					case *goai.DoneEvent:
						r.done++
					}
				}
				finished <- r
			}()
			select {
			case <-swapped:
			case r := <-finished:
				t.Fatalf("did not reach owned swap: %+v", r)
			case <-time.After(5 * time.Second):
				t.Fatal("did not reach owned swap")
			}
			var r result
			select {
			case r = <-finished:
			case <-time.After(time.Second):
				t.Error("credential open blocked on replaced FIFO")
				// Linux RDWR FIFO open never waits for a peer. Keep it until the blocked
				// reader has opened, then close it so that a pending Read also reaches EOF.
				peer, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
				if err != nil {
					t.Fatal(err)
				}
				select {
				case <-opened:
				case <-time.After(5 * time.Second):
					peer.Close()
					t.Fatal("owned FIFO reader did not open")
				}
				peer.Close()
				select {
				case r = <-finished:
				case <-time.After(5 * time.Second):
					t.Fatal("owned FIFO stream did not retire")
				}
			}
			if r.errors != 1 || r.done != 0 || r.failure == nil || r.failure.Error == nil || r.failure.Error.Model != "gemini" || inferenceCalls.Load() != 0 {
				t.Errorf("invalid terminal/inference: %+v requests=%d", r, inferenceCalls.Load())
			}
			if r.failure != nil {
				if cancelled && r.failure.Reason != goai.StopReasonAborted {
					t.Error("cancelled file load did not abort")
				}
				if !cancelled && (r.failure.Error == nil || !strings.Contains(r.failure.Error.ErrorMessage, "regular")) {
					t.Error("opened FIFO was not classified as non-regular")
				}
			}
		})
	}
}

func TestVertexCredentialRegularSymlink(t *testing.T) {
	root := t.TempDir()
	target, link := filepath.Join(root, "target.json"), filepath.Join(root, "link.json")
	if err := os.WriteFile(target, []byte(`{"type":"unsupported"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest("POST", "https://unused.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = applyVertexFileAuth(context.Background(), request, link, nil)
	if err == nil || !strings.Contains(err.Error(), "credential type is not supported") {
		t.Errorf("regular symlink did not pass file admission: %v", err)
	}
}
