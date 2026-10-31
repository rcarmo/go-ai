package google

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestVertexCredentialOpenedDescriptor(t *testing.T) {
	for _, name := range []string{"regular", "directory swap", "oversized swap", "cancel after open"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "credential.json")
			if err := os.WriteFile(path, []byte(`{"type":"unsupported"}`), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			savedOpen := openVertexCredentialFile
			var descriptor *os.File
			openVertexCredentialFile = func(path string) (*os.File, error) {
				if name == "directory swap" {
					if err := os.Remove(path); err != nil {
						return nil, err
					}
					if err := os.Mkdir(path, 0700); err != nil {
						return nil, err
					}
				}
				if name == "oversized swap" {
					if err := os.Truncate(path, vertexCredentialLimit+1); err != nil {
						return nil, err
					}
				}
				file, err := savedOpen(path)
				descriptor = file
				if name == "cancel after open" {
					cancel()
				}
				return file, err
			}
			t.Cleanup(func() { openVertexCredentialFile = savedOpen })
			savedClient := newVertexTokenClient
			var clients atomic.Int32
			newVertexTokenClient = func(*goai.StreamOptions) *http.Client {
				clients.Add(1)
				return &http.Client{Transport: vertexTokenWireFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("unexpected token I/O") })}
			}
			t.Cleanup(func() { newVertexTokenClient = savedClient })
			request, err := http.NewRequest("POST", "https://unused.invalid", nil)
			if err != nil {
				t.Fatal(err)
			}
			err = applyVertexFileAuth(ctx, request, path, nil)
			if name == "cancel after open" {
				if !errors.Is(err, context.Canceled) {
					t.Errorf("error=%v want cancellation", err)
				}
			} else {
				want := "regular credential file"
				if name == "regular" {
					want = "credential type is not supported"
				}
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Errorf("error=%v want %q", err, want)
				}
			}
			if descriptor == nil {
				t.Fatal("descriptor was not opened")
			}
			if _, err := descriptor.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Errorf("descriptor not closed: %v", err)
			}
			if clients.Load() != 0 {
				t.Error("file admission invoked token client")
			}
		})
	}
}
