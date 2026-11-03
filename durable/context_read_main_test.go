package durable

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func captureMainContextRead(t *testing.T, s *Session, conversation, at ID) preparedContextRead {
	t.Helper()
	if err := s.enter(bg); err != nil {
		t.Fatal(err)
	}
	defer s.leave()
	state, native, err := s.taskSnapshotView(bg)
	if err != nil || !native {
		t.Fatal("native capture", native, err)
	}
	return s.captureContextRead(state, conversation, at)
}

func appendMainContextMessage(t *testing.T, s *Session, conversation ID, text string) ID {
	t.Helper()
	id, err := s.MintID(bg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit(bg, func(tx *Tx) error { return tx.AppendEntry(contextEntry(id, conversation, goai.RoleUser, text)) }); err != nil {
		t.Fatal(err)
	}
	return id
}

// Pinned harness-context-range.test.ts pauses a private off-line scan, commits
// "missed", returns the stale range, then commits "last". Native derivation
// holds an immutable published image instead of scanning mutable raw storage.
func TestMainContextReadPublicationWitness(t *testing.T) {
	for _, boundary := range []string{"before-derive", "before-install"} {
		t.Run(boundary, func(t *testing.T) {
			for _, warm := range []bool{false, true} {
				name := "cold"
				if warm {
					name = "warm"
				}
				t.Run(name, func(t *testing.T) {
					sqliteStore(t, func(t *testing.T, b sqliteFixture) {
						root := mint(t, b.store)
						apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: root}})
						s, err := OpenSession(b.store)
						if err != nil {
							t.Fatal(err)
						}
						defer s.Close(bg)
						first := appendMainContextMessage(t, s, root, "first")
						if warm {
							if _, err := s.ContextView(bg, root, 0); err != nil {
								t.Fatal(err)
							}
						}
						read := captureMainContextRead(t, s, root, 0)
						if warm && read.previous == read.retained {
							t.Fatal("mutable cache header escaped capture")
						}
						if boundary == "before-derive" {
							appendMainContextMessage(t, s, root, "missed")
						}
						view, derived, err := deriveContextRange(read.state, root, 0, read.previous, read.limits)
						if err != nil || len(view.Entries) != 1 || view.Entries[0].ID != first {
							t.Fatal("captured bounds drifted", view, err)
						}
						if boundary == "before-install" {
							appendMainContextMessage(t, s, root, "missed")
						}
						if err := s.installContextRead(bg, read, derived); err != nil {
							t.Fatal(err)
						}
						s.taskBookkeeping(func() {
							if s.contextRanges[root] != read.retained {
								t.Fatal("stale current range installed")
							}
						})
						last := appendMainContextMessage(t, s, root, "last")
						fresh, err := s.ContextView(bg, root, 0)
						if err != nil {
							t.Fatal(err)
						}
						var got []string
						for _, m := range fresh.Messages {
							got = append(got, m.Content[0].Text)
						}
						if !reflect.DeepEqual(got, []string{"first", "missed", "last"}) {
							t.Fatal(got)
						}
						s.taskBookkeeping(func() {
							if r := s.contextRanges[root]; r == nil || r.tail != last || !r.current {
								t.Fatal("fresh range not current")
							}
						})
					})
				})
			}
		})
	}
}

func TestMainContextReadInstallationFences(t *testing.T) {
	for _, mode := range []string{"install", "newer-read", "expiry", "settings", "settings-empty", "cancel", "close", "raw-close", "poison", "failure", "receipt", "stored"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				var clock atomic.Int64
				clock.Store(1000)
				retention := int64(1000)
				h := mainContextScopeHarness(t, b.store, &clock)
				c, err := h.Root(bg, AgentChange{})
				if err != nil {
					t.Fatal(err)
				}
				s := h.session
				appendMainContextMessage(t, s, c.ID(), "first")
				if mode != "settings-empty" {
					if _, err := c.Context(bg); err != nil {
						t.Fatal(err)
					}
				}
				read := captureMainContextRead(t, s, c.ID(), 0)
				_, derived, err := deriveContextRange(read.state, c.ID(), 0, read.previous, read.limits)
				if err != nil {
					t.Fatal(err)
				}
				ctx := bg
				var expected error
				switch mode {
				case "install":
					clock.Store(1500)
				case "receipt", "stored":
					id, err := s.MintID(bg)
					if err != nil {
						t.Fatal(err)
					}
					entry := contextEntry(id, c.ID(), goai.RoleUser, "private receipt")
					var receipt commitReceipt
					if _, err := s.core.commitParts(bg, []Batch{{Writes: []Write{{Op: "append-entry", Entry: &entry}}}}, func(r commitReceipt) error { receipt = r; return nil }, true); err != nil {
						t.Fatal(err)
					}
					if mode == "receipt" {
						s.taskBookkeeping(func() {
							if err := s.adoptReceipt(receipt, nil); err != nil {
								t.Fatal(err)
							}
						})
					}
				case "newer-read":
					if _, err := c.ContextView(bg, 0); err != nil {
						t.Fatal(err)
					}
				case "expiry":
					clock.Store(2000)
				case "settings", "settings-empty":
					zero := int64(0)
					if err := h.SetSettings(bg, HarnessSettings{ContextRetentionMs: &zero}); err != nil {
						t.Fatal(err)
					}
					// Changing back to the captured value must not resurrect the old read.
					if err := h.SetSettings(bg, HarnessSettings{ContextRetentionMs: &retention}); err != nil {
						t.Fatal(err)
					}
				case "cancel":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(bg)
					cancel()
					expected = context.Canceled
				case "close":
					if err := h.Close(bg); err != nil {
						t.Fatal(err)
					}
					expected = ErrClosed
				case "raw-close":
					if err := s.core.close(bg, true); err != nil {
						t.Fatal(err)
					}
					expected = ErrClosed
				case "failure":
					expected = errors.New("off-line context first failure")
					s.taskBookkeeping(func() { s.backendResult(bg, expected, true) })
				case "poison":
					if err := s.core.enter(bg); err != nil {
						t.Fatal(err)
					}
					s.core.poison()
					s.core.leave()
					expected = ErrPoisoned
				}
				var kept *contextRange
				s.taskBookkeeping(func() { kept = s.contextRanges[c.ID()] })
				err = s.installContextRead(ctx, read, derived)
				if !errors.Is(err, expected) {
					t.Fatal("install", err, "want", expected)
				}
				s.taskBookkeeping(func() {
					got := s.contextRanges[c.ID()]
					switch mode {
					case "install", "stored":
						if got != derived || got.idleSince == nil || *got.idleSince != 1000 {
							t.Fatal("valid range not installed with original idle stamp")
						}
					case "expiry", "settings", "settings-empty", "close":
						if got != nil {
							t.Fatal("retired range resurrected")
						}
					case "newer-read", "cancel", "receipt":
						if got != kept {
							t.Fatal("newer/cancelled cache changed")
						}
					}
				})
			})
		})
	}
}

type mainContextProjection struct {
	nativeStorage
	text    string
	failure error
	reads   int
}

func (a *mainContextProjection) Snapshot(ctx context.Context) (Snapshot, error) {
	a.reads++
	if a.failure != nil {
		return Snapshot{}, a.failure
	}
	state, err := a.nativeStorage.Snapshot(ctx)
	if err == nil {
		for id, entry := range state.Entries {
			if len(entry.Model) > 0 {
				entry.Model[0].Content[0].Text = a.text
				state.Entries[id] = entry
			}
		}
	}
	return state, err
}

func TestMainContextReadWrapperAuthority(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		root := mint(t, b.store)
		apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: root}})
		id := mint(t, b.store)
		entry := contextEntry(id, root, goai.RoleUser, "raw")
		apply(t, b.store, Write{Op: "append-entry", Entry: &entry})
		adapter := &mainContextProjection{nativeStorage: b.store.(nativeStorage), text: "first projection"}
		h := openHarness(t, adapter, Options{})
		c, err := h.Conversation(bg, root)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"first projection", "changed at equal sequence"} {
			h.session.taskBookkeeping(func() { adapter.text = text })
			view, err := c.ContextView(bg, 0)
			if err != nil || view.Messages[0].Content[0].Text != text {
				t.Fatal("wrapper bypass", view, err)
			}
			model, err := c.Context(bg)
			if err != nil || model.Messages[0].Content[0].Text != text {
				t.Fatal("model wrapper bypass", model, err)
			}
		}
		want := reject("context adapter failure")
		h.session.taskBookkeeping(func() { adapter.failure = want })
		if _, err := c.ContextView(bg, 0); err == nil {
			t.Fatal("adapter error lost")
		} else {
			var request *StorageRequestError
			if !errors.As(err, &request) || request.Reason != "context adapter failure" {
				t.Fatal("adapter projection changed", err)
			}
		}
		h.session.taskBookkeeping(func() {
			if len(h.session.contextRanges) != 0 || adapter.reads < 5 {
				t.Fatal("wrapper used native cache", adapter.reads)
			}
		})
	})
}

func TestMainContextReadConcurrentHeaders(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		root := mint(t, b.store)
		apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: root}})
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		appendMainContextMessage(t, s, root, "first")
		var wg sync.WaitGroup
		defer wg.Wait()
		errs := make(chan error, 4)
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 30 {
					view, err := s.ContextView(bg, root, 0)
					if err != nil {
						errs <- err
						return
					}
					view.Messages[0].Content[0].Text = "detached"
				}
			}()
		}
		for range 20 {
			appendMainContextMessage(t, s, root, "next")
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		view, err := s.ContextView(bg, root, 0)
		if err != nil || len(view.Messages) != 21 || view.Messages[0].Content[0].Text != "first" {
			t.Fatal(view, err)
		}
	})
}

// Err checkpoints are request-owned barriers, without a production test hook.
// The first two checks are Session/core capture admission. Checks three/four
// run off line, before derivation and after detachment respectively.
type mainContextReadGate struct {
	context.Context
	calls            atomic.Int64
	at               int64
	entered, release chan struct{}
}

func (c *mainContextReadGate) Err() error {
	if c.calls.Add(1) == c.at {
		close(c.entered)
		<-c.release
	}
	return c.Context.Err()
}

func TestMainContextReadPublicOffLine(t *testing.T) {
	for _, api := range []string{"session", "conversation-view", "conversation-model"} {
		t.Run(api, func(t *testing.T) {
			for _, at := range []int64{3, 4} {
				name := "before-derive"
				if at == 4 {
					name = "before-install"
				}
				t.Run(name, func(t *testing.T) {
					sqliteStore(t, func(t *testing.T, b sqliteFixture) {
						var clock atomic.Int64
						clock.Store(1000)
						h := mainContextScopeHarness(t, b.store, &clock)
						c, err := h.Root(bg, AgentChange{})
						if err != nil {
							t.Fatal(err)
						}
						appendMainContextMessage(t, h.session, c.ID(), "first")
						gate := &mainContextReadGate{Context: bg, at: at, entered: make(chan struct{}), release: make(chan struct{})}
						done := make(chan error, 1)
						var texts []string
						go func() {
							var err error
							if api == "conversation-model" {
								var model *goai.Context
								model, err = c.Context(gate)
								if err == nil {
									for _, m := range model.Messages {
										texts = append(texts, m.Content[0].Text)
									}
								}
							} else {
								var view ContextView
								if api == "session" {
									view, err = h.session.ContextView(gate, c.ID(), 0)
								} else {
									view, err = c.ContextView(gate, 0)
								}
								if err == nil {
									for _, m := range view.Messages {
										texts = append(texts, m.Content[0].Text)
									}
								}
							}
							done <- err
						}()
						var once sync.Once
						release := func() { once.Do(func() { close(gate.release) }) }
						joined := false
						defer func() {
							release()
							if !joined {
								<-done
							}
						}()
						select {
						case <-gate.entered:
						case err := <-done:
							joined = true
							t.Fatal("read returned without off-line checkpoint", err)
						case <-time.After(5 * time.Second):
							t.Fatal("read never reached off-line checkpoint")
						}
						ctx, cancel := context.WithTimeout(bg, time.Second)
						_, err = h.session.Commit(ctx, func(tx *Tx) error {
							id, err := tx.MintID()
							if err != nil {
								return err
							}
							return tx.AppendEntry(contextEntry(id, c.ID(), goai.RoleUser, "missed"))
						})
						cancel()
						if err != nil {
							t.Fatal("off-line reader held Session line", err)
						}
						release()
						err = <-done
						joined = true
						if err != nil || !reflect.DeepEqual(texts, []string{"first"}) {
							t.Fatal("captured result changed", texts, err)
						}
						h.session.taskBookkeeping(func() {
							if h.session.contextRanges[c.ID()] != nil {
								t.Fatal("stale public read installed")
							}
						})
						appendMainContextMessage(t, h.session, c.ID(), "last")
						fresh, err := c.ContextView(bg, 0)
						if err != nil {
							t.Fatal(err)
						}
						var got []string
						for _, m := range fresh.Messages {
							got = append(got, m.Content[0].Text)
						}
						if !reflect.DeepEqual(got, []string{"first", "missed", "last"}) {
							t.Fatal(got)
						}
					})
				})
			}
		})
	}
}

func TestMainContextReadEmptyAndInvalidBounds(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		root := mint(t, b.store)
		apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: root}})
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		view, err := s.ContextView(bg, root, 0)
		if err != nil || !reflect.DeepEqual(view, ContextView{}) {
			t.Fatal("empty context changed", view, err)
		}
		if _, err := s.ContextView(bg, root, ID(MaxID)); err == nil {
			t.Fatal("invisible cutoff accepted")
		}
		if _, err := s.ContextView(bg, ID(MaxID), 0); err == nil {
			t.Fatal("unknown conversation accepted")
		}
		//lint:ignore SA1012 Deliberate nil-context admission rejection test.
		if _, err := s.ContextView(nil, root, 0); err == nil {
			t.Fatal("nil context accepted")
		}
		if s.failure() != nil {
			t.Fatal("request rejection failed session")
		}
	})
}
