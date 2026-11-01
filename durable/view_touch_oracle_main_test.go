package durable

import (
	"context"
	"sort"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func upstreamCompletedChat(t *testing.T, b backend, text string) (*Harness, *ConversationHandle) {
	t.Helper()
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		ch <- terminal(text)
		close(ch)
		return ch
	})
	h := openHarness(t, b.store, options)
	return h, root(t, h, ref)
}

// Pinned harness-view.test.ts:113-126,160-176. The real provider/submission
// path uses the native fake terminal provider instead of Source faux text.
// Shared JS reference assertions from line160 remain explicitly unported.
func TestMainViewUpstreamTouchOracles(t *testing.T) {
	for _, mode := range []string{"completed-hydration", "unmounted-silence"} {
		t.Run(mode, func(t *testing.T) {
			utf16Backends(t, func(t *testing.T, b backend) {
				h, c := upstreamCompletedChat(t, b, "hello")
				if mode == "completed-hydration" {
					sub, err := c.Submit(bg, Input{Content: "hi"})
					if err != nil {
						t.Fatal(err)
					}
					settled := waitSubmission(t, sub)
					if settled.Message == nil || settled.Message.Content[0].Text != "hello" {
						t.Fatal("source answer", settled)
					}
					fresh, err := c.Watch(bg)
					if err != nil {
						t.Fatal(err)
					}
					value, err := fresh.Value()
					fresh.Stop()
					if err != nil {
						t.Fatal(err)
					}
					if value.Conversation != (Conversation{ID: c.ID()}) {
						t.Fatal("source root record", value.Conversation)
					}
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					var all []Entry
					for _, e := range state.Entries {
						if e.Conversation == c.ID() {
							all = append(all, e)
						}
					}
					sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
					if len(value.Entries) != len(all) {
						t.Fatal("source all entries count", len(value.Entries), len(all))
					}
					for i, e := range all {
						got, err := dtoObject(value.Entries[i], h.session.limits)
						if err != nil {
							t.Fatal(err)
						}
						want, err := dtoObject(e, h.session.limits)
						if err != nil {
							t.Fatal(err)
						}
						if !equalDeltaJSON(map[string]any(got), map[string]any(want)) {
							t.Fatal("source all entries record", i)
						}
					}
					if value.Before != 0 {
						t.Fatal("source absent before", value.Before)
					}
					var kinds []string
					for k := range value.Docs {
						kinds = append(kinds, k)
					}
					sort.Strings(kinds)
					want := []string{"pi.agent", "pi.inbox", "pi.live", "pi.provider", "pi.usage"}
					if !equalJSONValue(kinds, want) {
						t.Fatal("source mounted keys", kinds)
					}
					if !equalJSONValue(value.Docs["pi.live"], JSON{}) || !equalJSONValue(value.Docs["pi.inbox"], JSON{"items": []any{}}) {
						t.Fatal("source settled live/inbox", value.Docs)
					}
					return
				}
				other, err := h.CreateConversation(bg, AgentChange{})
				if err != nil {
					t.Fatal(err)
				}
				_, ch := recordUpstreamWindow(t, c)
				appendUpstreamHistory(t, h, other, "note", 0)
				level := goai.ModelThinkingLevel("high")
				if err = c.ConfigurePatch(bg, AgentPatch{ThinkingLevel: &level}); err != nil {
					t.Fatal(err)
				}
				note := appendUpstreamHistory(t, h, c, "note", 0)
				frames := awaitUpstreamWindow(t, ch, note)
				if len(frames) != 2 {
					t.Fatal("source touching frame count", len(frames))
				}
				assertConversationOperations(t, frames[0].ops, []Operation{{"s", []any{"docs", "pi.agent", "thinkingLevel"}, "high"}}, h.session.limits)
			})
		})
	}
}
