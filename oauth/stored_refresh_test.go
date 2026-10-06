package oauth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type testCredentialStore struct {
	line  chan struct{}
	value *Credentials
}

func (s *testCredentialStore) Modify(ctx context.Context, _ string, update func(*Credentials) (*Credentials, error)) (*Credentials, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.line:
	}
	defer func() { s.line <- struct{}{} }()
	next, err := update(s.value)
	if err != nil {
		return nil, err
	}
	if next != nil {
		s.value = next
	}
	return s.value, nil
}
func TestStoredOAuth104RefreshPersistsAfterCallerCancelAndDoubleChecks(t *testing.T) {
	provider := &contextOAuthProvider{id: "stored-refresh-104", started: make(chan struct{}), release: make(chan struct{})}
	RegisterProvider(provider)
	store := &testCredentialStore{line: make(chan struct{}, 1), value: &Credentials{Access: "old", Refresh: "rotating", Expires: 1}}
	store.line <- struct{}{}
	caller, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := RefreshStoredCredential(caller, store, provider.ID(), func(c *Credentials) bool { return c.Expires == 1 })
		result <- err
	}()
	select {
	case <-provider.started:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not enter")
	}
	cancel()
	close(provider.release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal("admitted refresh discarded rotated token", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not finish")
	}
	if store.value.Access != "new-token" {
		t.Fatal("refresh did not persist", store.value)
	}
	var wait sync.WaitGroup
	for i := 0; i < 5; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := RefreshStoredCredential(context.Background(), store, provider.ID(), func(c *Credentials) bool { return c.Expires == 1 })
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	if provider.calls() != 1 {
		t.Fatal("double-checked refresh repeated", provider.calls())
	}
	<-store.line
	blocked, cancelBlocked := context.WithCancel(context.Background())
	cancelBlocked()
	if _, err := RefreshStoredCredential(blocked, store, provider.ID(), func(*Credentials) bool { return true }); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled lock admission", err)
	}
	store.line <- struct{}{}
}
