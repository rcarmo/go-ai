package oauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// CredentialStore is a host-owned atomic storage boundary. Modify acquires the
// provider lock with ctx, rereads current credentials, calls update while locked,
// and persists a non-nil replacement before unlocking. Nil replacement keeps
// current state. Once update starts, persistence must not be cancelled by ctx:
// providers can rotate refresh tokens before caller cancellation is observed.
// Process-shared stores must supply a process-shared lock; no global in-memory
// lock can make an arbitrary host's external store atomic.
type CredentialStore interface {
	Modify(context.Context, string, func(*Credentials) (*Credentials, error)) (*Credentials, error)
}

// RefreshStoredCredential mirrors the pinned stored-refresh contract: callers
// may cancel lock admission, but admitted refresh+save is bounded by its own
// timeout and cannot lose a rotated refresh token on caller cancellation.
func RefreshStoredCredential(ctx context.Context, store CredentialStore, id string, needsRefresh func(*Credentials) bool) (*Credentials, error) {
	if ctx == nil || store == nil || needsRefresh == nil {
		return nil, goai.NewModelsError(goai.ModelsErrorAuth, "invalid stored OAuth refresh", nil)
	}
	provider := GetProvider(id)
	if provider == nil {
		return nil, goai.NewModelsError(goai.ModelsErrorAuth, fmt.Sprintf("OAuth provider %q not registered", id), nil)
	}
	result, err := store.Modify(ctx, id, func(current *Credentials) (*Credentials, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if current == nil || !needsRefresh(current) {
			return nil, nil
		}
		refreshContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		refreshed, err := refreshTokenWithContext(refreshContext, provider, current)
		if err != nil {
			return nil, goai.NewModelsError(goai.ModelsErrorOAuth, fmt.Sprintf("OAuth refresh failed for %s", id), err)
		}
		return refreshed, nil
	})
	if err != nil {
		var modelsError *goai.ModelsError
		if errors.As(err, &modelsError) {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, goai.NewModelsError(goai.ModelsErrorAuth, fmt.Sprintf("Credential store modify failed for %s", id), err)
	}
	return result, nil
}
