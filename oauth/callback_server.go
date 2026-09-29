package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
)

type oauthCallbackOptions struct {
	ProviderName string
	Host         string
	Port         int
	Path         string
	State        string
}

type oauthCallbackServer struct {
	RedirectURI string
	Wait        func() (*url.URL, error)
	Close       func() error
}

func startOAuthCallbackServer(ctx context.Context, opts oauthCallbackOptions) (*oauthCallbackServer, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("login cancelled: %w", err)
	}
	host := opts.Host
	if host == "" {
		host = "127.0.0.1"
	}
	path := opts.Path
	if path == "" {
		path = "/auth/callback"
	}
	providerName := opts.ProviderName
	if providerName == "" {
		providerName = "OAuth"
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(opts.Port)))
	if err != nil {
		return nil, err
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		return nil, fmt.Errorf("OAuth callback server did not bind to TCP")
	}
	redirectHost := host
	if redirectHost == "" || redirectHost == "0.0.0.0" || redirectHost == "::" {
		redirectHost = "127.0.0.1"
	}
	redirectURI := (&url.URL{Scheme: "http", Host: net.JoinHostPort(redirectHost, strconv.Itoa(addr.Port)), Path: path}).String()

	result := make(chan *url.URL, 1)
	errs := make(chan error, 1)
	var once sync.Once
	finishErr := func(err error) { once.Do(func() { errs <- err }) }
	finishURL := func(u *url.URL) { once.Do(func() { result <- u }) }

	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "callback method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if opts.State != "" && r.URL.Query().Get("state") != opts.State {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
			desc := r.URL.Query().Get("error_description")
			if desc == "" {
				desc = oauthErr
			}
			http.Error(w, providerName+" authorization failed: "+desc, http.StatusBadRequest)
			finishErr(fmt.Errorf("%s authorization failed: %s", providerName, desc))
			return
		}
		if r.URL.Query().Get("code") == "" {
			http.Error(w, "missing authorization code", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("Authentication completed. You can close this window."))
		copyURL := *r.URL
		copyURL.Scheme = "http"
		copyURL.Host = r.Host
		finishURL(&copyURL)
	})
	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			finishErr(err)
		}
	}()
	go func() {
		<-ctx.Done()
		finishErr(fmt.Errorf("login cancelled: %w", ctx.Err()))
		_ = server.Close()
	}()

	return &oauthCallbackServer{
		RedirectURI: redirectURI,
		Wait: func() (*url.URL, error) {
			select {
			case u := <-result:
				return u, nil
			case err := <-errs:
				return nil, err
			case <-ctx.Done():
				return nil, fmt.Errorf("login cancelled: %w", ctx.Err())
			}
		},
		Close: server.Close,
	}, nil
}
