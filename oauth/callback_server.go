package oauth

import (
	"context"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type oauthCallbackOptions struct {
	ProviderName string
	Host         string
	Port         int
	Path         string
	State        string
	// Complete runs once after validation, before rendering success. Providers
	// can use it to exchange the code; failure returns HTTP 502.
	Complete func(context.Context, *url.URL) error
	Listen   func(network, address string) (net.Listener, error)
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

	listen := opts.Listen
	if listen == nil {
		listen = net.Listen
	}
	listener, err := listen("tcp", net.JoinHostPort(host, strconv.Itoa(opts.Port)))
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
	var completionMu sync.Mutex
	completed := false
	var completionErr error

	mux := http.NewServeMux()
	server := &http.Server{Handler: mux}
	closed := make(chan struct{})
	var closeOnce sync.Once
	closeServer := func() error {
		var closeErr error
		closeOnce.Do(func() {
			close(closed)
			shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			closeErr = server.Shutdown(shutdownContext)
			if closeErr != nil {
				_ = server.Close()
			}
		})
		return closeErr
	}
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
			_ = writeOAuthCallbackResponse(w, http.StatusBadRequest, "text/plain; charset=utf-8", providerName+" authorization failed: "+desc+"\n")
			finishErr(fmt.Errorf("%s authorization failed: %s", providerName, desc))
			return
		}
		if r.URL.Query().Get("code") == "" {
			http.Error(w, "missing authorization code", http.StatusBadRequest)
			return
		}
		copyURL := *r.URL
		copyURL.Scheme = "http"
		copyURL.Host = r.Host
		completionMu.Lock()
		defer completionMu.Unlock()
		if !completed {
			completed = true
			if opts.Complete != nil {
				completionErr = opts.Complete(r.Context(), &copyURL)
			}
		}
		if completionErr != nil {
			// Never echo endpoint errors or authorization credentials into HTML.
			_ = writeOAuthCallbackResponse(w, http.StatusBadGateway, "text/plain; charset=utf-8", "Authentication failed: token exchange did not complete.\n")
			finishErr(completionErr)
			return
		}
		if err := writeOAuthCallbackResponse(w, http.StatusOK, "text/html; charset=utf-8", oauthCallbackSuccessHTML(providerName)); err != nil {
			finishErr(fmt.Errorf("could not deliver OAuth callback response"))
			return
		}
		finishURL(&copyURL)
	})
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			finishErr(err)
			closeOnce.Do(func() { close(closed) })
		}
	}()
	if done := ctx.Done(); done != nil {
		go func() {
			select {
			case <-done:
				finishErr(fmt.Errorf("login cancelled: %w", ctx.Err()))
				_ = closeServer()
			case <-closed:
			}
		}()
	}

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
		Close: closeServer,
	}, nil
}

// Write and flush the full, length-delimited response before publishing login
// completion. A browser launches asynchronously; publishing first lets Login's
// deferred shutdown race the HTTP server's buffered response or chunk terminator.
func writeOAuthCallbackResponse(w http.ResponseWriter, status int, contentType, body string) error {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if _, err := w.Write([]byte(body)); err != nil {
		return err
	}
	return http.NewResponseController(w).Flush()
}

const oauthCallbackLogoSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 800" aria-hidden="true"><path fill="#F09082" d="M165.29 165.29H517.36V400H400V282.65H165.29Z"/><path fill="#4D9ABF" d="M165.29 282.65H282.65V400H400V517.36H282.65V634.72H165.29Z"/><path fill="#F1BE58" d="M517.36 400H634.72V634.72H517.36Z"/></svg>`

func oauthCallbackSuccessHTML(providerName string) string {
	if providerName == "" {
		providerName = "OAuth"
	}
	escapedProviderName := html.EscapeString(providerName)
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Authentication successful</title>
  <style>
    :root {
      color-scheme: dark;
      --page-bg: #09090b;
      --text: #fafafa;
      --text-dim: #a1a1aa;
      --font-sans: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 24px;
      background: var(--page-bg);
      color: var(--text);
      font-family: var(--font-sans);
      text-align: center;
    }
    main {
      width: 100%;
      max-width: 32rem;
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 12px;
    }
    .logo {
      width: 72px;
      height: 72px;
      display: block;
      margin-bottom: 12px;
    }
    h1 {
      margin: 0;
      font-size: 28px;
      line-height: 1.15;
      font-weight: 650;
    }
    p {
      margin: 0;
      color: var(--text-dim);
      font-size: 15px;
      line-height: 1.7;
    }
  </style>
</head>
<body>
  <main>
    <div class="logo">` + oauthCallbackLogoSVG + `</div>
    <h1>Authentication successful</h1>
    <p>Signed in to ` + escapedProviderName + `.</p>
  </main>
</body>
</html>`
}
