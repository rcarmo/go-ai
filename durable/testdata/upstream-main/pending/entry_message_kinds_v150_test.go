package durable

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Original harness entries: pi.user / pi.assistant carry `model:[message]`
// and never a native `value` payload (src/harness/generation.ts:880,1154).
func TestPiDurableV150HarnessMessageEntriesUseOriginalKindsAndModel(t *testing.T) {
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		ch <- terminal("done")
		close(ch)
		return ch
	})
	store, _ := newSQLiteTestStore(t)
	h := openHarness(t, store, options)
	conv := root(t, h, ref)
	sub, err := conv.Submit(bg, Input{Content: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	waitSubmission(t, sub)
	check := func(label string, h *Harness) {
		snapshot, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		kinds := map[string]goai.Role{}
		for _, entry := range snapshot.Entries {
			if entry.Conversation != conv.ID() || (entry.Kind != "message" && entry.Kind != "pi.user" && entry.Kind != "pi.assistant") {
				continue
			}
			encoded, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), `"value"`) || len(entry.Model) != 1 {
				t.Errorf("%s entry %d not original-shaped: %s", label, entry.ID, encoded)
				continue
			}
			kinds[entry.Kind] = entry.Model[0].Role
		}
		if kinds["pi.user"] != goai.RoleUser || kinds["pi.assistant"] != goai.RoleAssistant || len(kinds) != 2 {
			t.Errorf("%s kinds = %v", label, kinds)
		}
	}
	check("live", h)
	if err := h.Close(bg); err != nil {
		t.Fatal(err)
	}
	check("reopened", openHarness(t, reopenStoreAfterHarnessClose(t, store), options))
}
