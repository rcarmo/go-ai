package goai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OpenRouterCatalogOptions keeps discovery/persistence scoped to the credential
// and endpoint. Stores contain models and an opaque scope ID, never the key.
// AllowNetwork=false loads only this scope's last known catalog or its baseline.
// Supply a durable ModelsStore to retain the catalog across process restarts.
type OpenRouterCatalogOptions struct {
	BaseURL      string
	Client       *http.Client
	Store        ModelsStore
	AllowNetwork bool
}

func openRouterCatalogScope(base, key string) (Provider, error) {
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("invalid OpenRouter catalog endpoint")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	sum := sha256.Sum256([]byte(u.String() + "\x00" + key))
	return Provider("openrouter-scope-" + hex.EncodeToString(sum[:])), nil
}

// ResolveOpenRouterCatalog loads and optionally refreshes a credential-scoped
// catalog without modifying the global registry. Failed refreshes leave the
// existing stored catalog intact and return an error, rather than broadening it.
func ResolveOpenRouterCatalog(ctx context.Context, key string, baseline []*Model, options OpenRouterCatalogOptions) ([]*Model, error) {
	if ctx == nil {
		return nil, fmt.Errorf("nil catalog context")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	base := options.BaseURL
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	scope, e := openRouterCatalogScope(base, key)
	if e != nil {
		return nil, e
	}
	if key == "" {
		return cloneModels(baseline), nil
	}
	current := cloneModels(baseline)
	if options.Store != nil {
		entry, e := options.Store.Read(scope)
		if e != nil {
			return nil, fmt.Errorf("OpenRouter catalog store read: %w", e)
		}
		if entry != nil {
			current = cloneModels(entry.Models)
		}
	}
	if !options.AllowNetwork {
		return current, nil
	}
	updated, e := PersonalizeOpenRouterCatalog(ctx, options.Client, base, key, baseline)
	if e != nil {
		return nil, e
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	if options.Store != nil {
		if e = options.Store.Write(scope, &ModelsStoreEntry{Models: cloneModels(updated), CheckedAt: time.Now().UnixMilli()}); e != nil {
			return nil, fmt.Errorf("OpenRouter catalog store write: %w", e)
		}
	}
	return updated, nil
}
