package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestCompaction120FailureReopenNoCutAndSummaryClearing(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- terminal("summary")
			close(ch)
			return ch
		})
		resolve := options.Models
		options.Models = func(goai.Provider, string) *goai.Model { return nil }
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		failed, err := conv.Compact(bg, CompactionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if record := waitPublicTask(t, h, failed); record.State.Outcome.Status != "failed" {
			t.Fatal(record)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		options.Models = resolve
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		conv, err = next.Conversation(bg, conv.ID())
		if err != nil {
			t.Fatal(err)
		}
		failure := func() *CompactionFailure {
			state, err := next.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			view, err := conversationSnapshot(state, conv.ID(), next.session.limits)
			if err != nil {
				t.Fatal(err)
			}
			return view.CompactionFailure
		}
		if f := failure(); f == nil || f.Task != failed {
			t.Fatal("failure lost on reopen", f)
		}
		noCut, err := conv.Compact(bg, CompactionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if record := waitPublicTask(t, next, noCut); record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		}
		if f := failure(); f == nil || f.Task != failed {
			t.Fatal("no-cut cleared failure", f)
		}
		_, err = conv.Commit(bg, func(tx *Tx) error {
			for _, text := range []string{"old question", "old answer", "recent"} {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				value, err := dtoObject(userReceipt(text), tx.limits)
				if err != nil {
					return err
				}
				if err := tx.AppendEntry(Entry{ID: id, Conversation: conv.ID(), Kind: "message", Value: value}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		summary, err := conv.Compact(bg, CompactionOptions{KeepRecentTokens: 1})
		if err != nil {
			t.Fatal(err)
		}
		if record := waitPublicTask(t, next, summary); record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		}
		if f := failure(); f != nil {
			t.Fatal("summary did not clear failure", f)
		}
	})
}
