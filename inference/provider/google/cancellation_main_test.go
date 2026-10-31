package google

// Source-derived at 81f3cd41: both adapters classify caught errors as aborted
// whenever the caller signal is aborted, and check it before successful done.
// Native HTTP/body-lifetime probes use a parked response and no live credentials.
import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestMainSourceGoogleCancellation(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		for _, mode := range []string{"before request", "after partial", "cancel and reject", "after finish", "reject without cancel"} {
			t.Run(string(api)+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				var requests atomic.Int32
				stopped := make(chan struct{})
				var stopOnce sync.Once
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					w.Header().Set("Content-Type", "text/event-stream")
					first := `{"responseId":"first","candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`
					if mode == "after finish" {
						first = `{"responseId":"first","candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"hello"}]}}]}`
					}
					fmt.Fprintf(w, "data: %s\n\n", first)
					if mode == "cancel and reject" || mode == "reject without cancel" {
						fmt.Fprint(w, "data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n")
					}
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					stopOnce.Do(func() { close(stopped) })
				}))
				t.Cleanup(server.Close)
				t.Cleanup(cancel)
				model := upstreamGoogleThinkingModel(api, "gemini-2.5-flash", nil)
				model.BaseURL = server.URL
				calls := 0
				opts := &goai.StreamOptions{APIKey: "test", Project: "test-project", Location: "us-central1"}
				opts.OnProviderStreamEvent = func(any, *goai.Model) error {
					calls++
					switch mode {
					case "after partial", "after finish":
						cancel()
					case "cancel and reject":
						if calls == 2 {
							cancel()
							return errors.New("hook rejected")
						}
					case "reject without cancel":
						if calls == 2 {
							return errors.New("hook rejected")
						}
					}
					return nil
				}
				if mode == "before request" {
					cancel()
				}
				var message *goai.Message
				done, failures := 0, 0
				var reason goai.StopReason
				var diagnostic error
				for event := range goai.GetApiProvider(api).Stream(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts) {
					switch e := event.(type) {
					case *goai.DoneEvent:
						done++
						message = e.Message
					case *goai.ErrorEvent:
						failures++
						message = e.Error
						reason = e.Reason
						diagnostic = e.Err
					}
				}
				want := goai.StopReasonAborted
				if mode == "reject without cancel" {
					want = goai.StopReasonError
					if ctx.Err() != nil {
						t.Fatal("negative control unexpectedly cancelled")
					}
				}
				if failures != 1 || done != 0 || reason != want {
					t.Errorf("errors=%d done=%d reason=%s want %s", failures, done, reason, want)
				}
				if diagnostic == nil {
					t.Error("missing terminal diagnostic")
				}
				if message == nil {
					t.Error("missing assistant error payload")
				} else {
					if message.StopReason != want || message.Role != goai.RoleAssistant || message.Model != model.ID || message.Api != api || message.Provider != model.Provider || message.Usage == nil || message.ErrorMessage == "" {
						t.Errorf("invalid assistant failure payload: reason=%s role=%s model=%s diagnostic=%q usage=%v", message.StopReason, message.Role, message.Model, message.ErrorMessage, message.Usage)
					}
					if mode != "before request" && (len(message.Content) != 1 || message.Content[0].Text != "hello" || message.ResponseID != "first") {
						t.Errorf("partial content/id lost: %#v id=%s", message.Content, message.ResponseID)
					}
					if (mode == "cancel and reject" || mode == "reject without cancel") && message.ErrorMessage != "hook rejected" {
						t.Errorf("hook diagnostic %q", message.ErrorMessage)
					}
				}
				if mode == "before request" {
					if requests.Load() != 0 || calls != 0 {
						t.Errorf("cancelled request reached HTTP=%d hook=%d", requests.Load(), calls)
					}
				} else {
					select {
					case <-stopped:
					case <-time.After(time.Second):
						t.Error("parked response not released before caller cleanup")
					}
					if requests.Load() != 1 {
						t.Errorf("HTTP requests %d", requests.Load())
					}
				}
			})
		}
	}
}
