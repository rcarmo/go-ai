package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"path/filepath"
	"testing"
)

func TestIdentity140DurableDefaultHostAndPinnedOverrides(t *testing.T) {
	for _, tc := range []struct {
		name   string
		host   *goai.ClientIdentity
		pinned JSON
		want   string
	}{
		{name: "pi-default", want: "pi"},
		{name: "host", host: &goai.ClientIdentity{Attribution: &goai.ClientAttribution{Name: "host", URL: "https://host.example"}}, want: "host"},
		{name: "host-null", host: &goai.ClientIdentity{AttributionSet: true}},
		{name: "pinned-null", host: &goai.ClientIdentity{Attribution: &goai.ClientAttribution{Name: "host", URL: "https://host.example"}}, pinned: JSON{"attribution": nil, "userAgent": "short"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, _ *goai.Context, o *goai.StreamOptions) <-chan goai.Event {
				calls++
				name := ""
				if o.Identity != nil && o.Identity.Attribution != nil {
					name = o.Identity.Attribution.Name
				}
				if name != tc.want {
					t.Errorf("identity=%+v want attribution=%s", o.Identity, tc.want)
				}
				if tc.pinned != nil && (o.Identity.UserAgent == nil || o.Identity.UserAgent.Mode != "short") {
					t.Error("pinned request identity missing")
				}
				ch := make(chan goai.Event, 1)
				ch <- terminal("done")
				close(ch)
				return ch
			})
			options.Identity = tc.host
			store, err := OpenJournal(filepath.Join(t.TempDir(), "journal"), JournalOptions{})
			if err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, options)
			if tc.host != nil && tc.host.Attribution != nil {
				tc.host.Attribution.Name = "mutated after Open"
			}
			conversation, err := h.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{Identity: tc.pinned}})
			if err != nil {
				t.Fatal(err)
			}
			submission, err := conversation.Submit(bg, Input{Content: "hello", RequestID: "request"})
			if err != nil {
				t.Fatal(err)
			}
			result := waitSubmission(t, submission)
			if result.Message == nil || result.Message.Content[0].Text != "done" || calls != 1 {
				t.Fatalf("%+v calls=%d", result, calls)
			}
		})
	}
}

func TestIdentity140DurableAuthorityAndBoundedJSON(t *testing.T) {
	limits := DefaultLimits()
	if _, err := cloneOptions(&goai.StreamOptions{Identity: &goai.ClientIdentity{}}, limits); err == nil {
		t.Fatal("accepted unpinned callback identity")
	}
	for _, value := range []JSON{{"unknown": true}, {"userAgent": "bad"}, {"userAgent": JSON{"custom": "x", "extra": true}}, {"attribution": JSON{"name": "x"}}, {"version": nil}, {"attribution": JSON{"name": "x", "url": "y", "extra": "bad"}}} {
		if _, err := cloneSettings(RequestSettings{Identity: value}, limits); err == nil {
			t.Fatalf("accepted invalid identity %#v", value)
		}
	}
	settings := RequestSettings{Identity: JSON{"userAgent": JSON{"custom": "owned"}, "attribution": nil}}
	detached, err := cloneSettings(settings, limits)
	if err != nil {
		t.Fatal(err)
	}
	settings.Identity["userAgent"].(JSON)["custom"] = "changed"
	options := &goai.StreamOptions{}
	if err = applyRequestSettings(options, detached, limits); err != nil {
		t.Fatal(err)
	}
	if options.Identity == nil || *options.Identity.UserAgent.Custom != "owned" || !options.Identity.AttributionSet {
		t.Fatal(options.Identity)
	}
}
