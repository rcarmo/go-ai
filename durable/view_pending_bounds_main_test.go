package durable

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestMainConversationPendingNativeBounds(t *testing.T) {
	l := DefaultLimits()
	before := ConversationViewCopy{Conversation: Conversation{ID: 1}, Entries: []Entry{}, Docs: map[string]JSON{"pi.live": {"n": JSON{"i": -1}}}}
	for _, count := range []int{maxPendingWatchOperations, maxPendingWatchOperations + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			frames := make([]PublicationFrame, 2)
			index := 0
			for i := range frames {
				size := count / 2
				if i == 1 {
					size = count - index
				}
				ops := make([]Operation, size)
				for j := range ops {
					ops[j] = Operation{"s", []any{"docs", "pi.live", "n"}, JSON{"i": index}}
					index++
				}
				owned, err := ownOperations(ops, l)
				if err != nil {
					t.Fatal(err)
				}
				value, _ := copyConversationView(before, l)
				value.Docs["pi.live"]["n"] = JSON{"i": index - 1}
				frames[i] = PublicationFrame{Context: bg, conversation: &conversationPublicationFrame{value: value, ops: owned}}
			}
			folded, err := coalesceConversationFrames(frames, before, l, l.MaxRetainedBytes)
			if err != nil {
				t.Fatal(err)
			}
			wantCount := count
			if count > maxPendingWatchOperations {
				wantCount = 1
			}
			if len(folded.conversation.ops) != wantCount || (count > maxPendingWatchOperations && folded.conversation.ops[0][0] != "r") {
				t.Fatal("native operation boundary", len(folded.conversation.ops))
			}
			// A bounded recovery root is also required when retained bytes cannot hold
			// the operation log; one byte cannot even hold that recovery root.
			bounded, err := coalesceConversationFrames(frames, before, l, 512)
			if err != nil || bounded.conversation.ops[0][0] != "r" {
				t.Fatal("missing byte-bounded recovery", err)
			}
			if _, err := coalesceConversationFrames(frames, before, l, 1); err == nil {
				t.Fatal("accepted oversized recovery")
			}
			if len(frames[0].conversation.ops) != count/2 || frames[0].conversation.ops[0][0] != "s" {
				t.Fatal("fold mutated received operations")
			}
		})
	}
}

func TestMainConversationPendingIndependentDTOBudgets(t *testing.T) {
	l := DefaultLimits()
	l.MaxNodes = 180
	items := make([]any, 150)
	for i := range items {
		items[i] = i
	}
	before := ConversationViewCopy{Conversation: Conversation{ID: 1}, Entries: []Entry{}, Docs: map[string]JSON{"pi.live": {"items": items}}}
	frames := make([]PublicationFrame, 2)
	value, err := copyConversationView(before, l)
	if err != nil {
		t.Fatal(err)
	}
	for i := range frames {
		key := fmt.Sprint(i)
		value.Docs["pi.live"][key] = i
		owned, err := copyConversationView(value, l)
		if err != nil {
			t.Fatal(err)
		}
		frames[i].conversation = &conversationPublicationFrame{value: owned, ops: []Operation{{"s", []any{"docs", "pi.live", key}, i}}}
	}
	folded, err := coalesceConversationFrames(frames, before, l, l.MaxRetainedBytes)
	if err != nil || len(folded.conversation.ops) != 2 {
		t.Fatal("legal independent DTOs rejected", err)
	}
	combinedValue, err := dtoObject(folded.conversation.value, l)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = encodeBounded(JSON{"value": combinedValue, "ops": folded.conversation.ops}, l, l.MaxFramePayloadBytes); err == nil {
		t.Fatal("control did not exceed a joint DTO node budget")
	}
	folded.conversation.ops[0][2] = "borrowed"
	if frames[0].conversation.ops[0][2] != 0 {
		t.Fatal("borrowed input tuple")
	}
}

func TestMainConversationPendingReadiness(t *testing.T) {
	for _, first := range []int{0, 1} {
		t.Run(fmt.Sprint(first), func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h, c := transcriptFixture(t, b)
				watch, err := detachedWatch(c, bg)
				if err != nil {
					t.Fatal(err)
				}
				defer watch.Stop()
				ready := []chan struct{}{make(chan struct{}), make(chan struct{})}
				defer func() {
					for _, r := range ready {
						select {
						case <-r:
						default:
							close(r)
						}
					}
				}()
				ids := make([]ID, 2)
				for i := range ids {
					ids[i], err = h.session.MintID(bg)
					if err != nil {
						t.Fatal(err)
					}
				}
				h.session.taskBookkeeping(func() {
					for i, id := range ids {
						prepared, receipt, candidate := mainPreparedPublication(t, h.session, bg, id, ready[i])
						if _, err := h.session.publishSessionPublication(&prepared, receipt, &candidate); err != nil {
							t.Fatal(err)
						}
					}
				})
				delivered := make(chan ConversationViewCopy, 2)
				if err := watch.Start(func(_ context.Context, value ConversationViewCopy, _ []Operation) error {
					delivered <- value
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(3 * time.Second)
				for {
					watch.sub.mu.Lock()
					detached := watch.sub.running && len(watch.sub.pending) == 0
					watch.sub.mu.Unlock()
					if detached {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("worker did not detach the pending pair")
					}
					time.Sleep(time.Millisecond)
				}
				close(ready[first])
				select {
				case <-delivered:
					t.Fatal("callback crossed another publication's fence")
				case <-time.After(25 * time.Millisecond):
				}
				close(ready[1-first])
				transcriptIDs(t, transcriptFrame(t, delivered), ids, 0)
			})
		})
	}
}

// Moving a merged append earlier can exceed an intermediate full-view budget,
// even though each publication and the folded operation DTO independently fit.
func TestMainConversationPendingIntermediateBudget(t *testing.T) {
	l := DefaultLimits()
	before := ConversationViewCopy{Conversation: Conversation{ID: 1}, Entries: []Entry{}, Docs: map[string]JSON{"pi.live": {"a": "", "b": strings.Repeat("x", 64)}}}
	baseline, err := dtoObject(before, l)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeBounded(baseline, l, l.MaxDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	l.MaxDocumentBytes = len(encoded) + 10
	ops := []Operation{{"a", []any{"docs", "pi.live", "a"}, "1"}, {"s", []any{"docs", "pi.live", "b"}, ""}, {"a", []any{"docs", "pi.live", "a"}, strings.Repeat("y", 64)}}
	var current any = baseline
	frames := make([]PublicationFrame, len(ops))
	value, _ := copyConversationView(before, l)
	for i, op := range ops {
		current, err = ApplyOperations(current, []Operation{op}, l)
		if err != nil {
			t.Fatal("individual publication did not fit", i, err)
		}
		switch i {
		case 0:
			value.Docs["pi.live"]["a"] = "1"
		case 1:
			value.Docs["pi.live"]["b"] = ""
		case 2:
			value.Docs["pi.live"]["a"] = "1" + strings.Repeat("y", 64)
		}
		owned, err := copyConversationView(value, l)
		if err != nil {
			t.Fatal(err)
		}
		frames[i].conversation = &conversationPublicationFrame{value: owned, ops: []Operation{op}}
	}
	merged, overflow, err := foldWatchOperations([][]Operation{{ops[0]}, {ops[1]}, {ops[2]}}, l)
	if err != nil || overflow {
		t.Fatal("unexpected fold failure", err)
	}
	if _, err = ownOperations(merged, l); err != nil {
		t.Fatal("operation DTO control did not fit", err)
	}
	if _, err = ApplyOperations(baseline, merged, l); err == nil {
		t.Fatal("intermediate budget control did not reject")
	}
	folded, err := coalesceConversationFrames(frames, before, l, l.MaxRetainedBytes)
	if err != nil || len(folded.conversation.ops) != 1 || folded.conversation.ops[0][0] != "r" {
		t.Fatal("missing intermediate-budget recovery", err)
	}
	applied, err := ApplyOperations(baseline, folded.conversation.ops, l)
	if err != nil || !equalJSONValue(applied, current) {
		t.Fatal("invalid bounded recovery", err)
	}
}
