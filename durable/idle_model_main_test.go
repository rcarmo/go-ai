package durable

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source uses JavaScript Number multiplication, not integer LCG arithmetic.
func mainIdleRandom(seed float64) func() float64 {
	return func() float64 {
		seed = math.Mod(float64(float64(seed*1103515245)+12345), 2147483648)
		return seed / 2147483648
	}
}
func TestMainIdleModelSourceRandom(t *testing.T) {
	next := mainIdleRandom(42)
	for _, want := range []float64{1250496027, 1116302080, 1964818176, 1500480256, 1617229568, 1480547072, 1119088384, 326038272} {
		if got := next() * 2147483648; got != want {
			t.Fatalf("source random state=%v want=%v", got, want)
		}
	}
}

type mainIdleModelWork struct {
	id, conversation, read  ID
	gate, started, returned chan struct{}
	readErr                 error // published by closing started
}

// Independent downward graph traversal: production walks each candidate's
// owners upward. Ended owners remain graph connectors; background edges detach.
func mainIdleModelTargets(state Snapshot, hosts map[ID]*TaskRuntime, conversation ID) []ID {
	seenTasks, seenConversations := map[ID]bool{}, map[ID]bool{}
	var out []ID
	var visitTask, visitConversation func(ID)
	visitTask = func(id ID) {
		task := state.Tasks[id]
		background := task.Execution != nil && task.Execution.Native != nil && task.Execution.Native.Background
		if seenTasks[id] || background {
			return
		}
		seenTasks[id] = true
		if task.Status != "done" && task.Status != "failed" && task.Status != "aborted" || hosts[id] != nil {
			out = append(out, id)
		}
		for childID, child := range state.Tasks {
			if child.Owner == id {
				visitTask(childID)
			}
		}
		for cid, c := range state.Conversations {
			if c.Owner == id {
				visitConversation(cid)
			}
		}
	}
	visitConversation = func(id ID) {
		if seenConversations[id] {
			return
		}
		seenConversations[id] = true
		for tid, task := range state.Tasks {
			if task.Owner == 0 && task.Conversation == id {
				visitTask(tid)
			}
		}
	}
	if conversation == 0 {
		for id, c := range state.Conversations {
			if c.Owner == 0 {
				visitConversation(id)
			}
		}
	} else {
		visitConversation(conversation)
	}
	slices.Sort(out)
	return out
}

// Pinned harness-idle-scopes.test.ts:436-511, seed 42 and 200 operations.
// Actual registered hosts and public creation/completion APIs preserve native
// owner fences. This checks values, not Source's fullIdleChecks==0 work bound.
func TestMainIdleNestedWorkModel(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		var nodes sync.Map
		def := taskDefinition(t, "idle.model", func(ctx context.Context, record TaskRecord, r *TaskRuntime) error {
			name, ok := record.Input.Value.(string)
			if !ok {
				return fmt.Errorf("model input is not a name")
			}
			value, ok := nodes.Load(name)
			if !ok {
				return fmt.Errorf("unknown model gate %s", name)
			}
			n := value.(*mainIdleModelWork)
			defer close(n.returned)
			if n.read != 0 {
				_, n.readErr = r.ContextView(ctx, n.read, 0)
			}
			close(n.started)
			if n.readErr != nil {
				return n.readErr
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-n.gate:
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{Now: func() int64 { return 1000 }, SetTimer: func(func(), time.Duration, TimerOptions) func() { return nil }}, def)
		t.Cleanup(func() {
			nodes.Range(func(_, value any) bool { releaseTaskGate(value.(*mainIdleModelWork).gate); return true })
		})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		note := func(tx *Tx, cid ID) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			e := contextEntry(id, cid, goai.RoleUser, "note")
			return tx.AppendEntry(e)
		}
		commit := func(f func(*Tx) error) {
			t.Helper()
			if _, err := h.CommitTasks(bg, 1, f); err != nil {
				t.Fatal(err)
			}
		}
		commit(func(tx *Tx) error { return note(tx, 1) })
		conversations := []ID{1}
		var live []*mainIdleModelWork
		next := mainIdleRandom(42)
		pick := func(n int) int { return int(next() * float64(n)) }
		await := func(ch <-chan struct{}) {
			t.Helper()
			select {
			case <-ch:
			case <-time.After(5 * time.Second):
				t.Fatal("model host barrier timed out")
			}
		}
		spawn := func(name string, read ID, create func(*Tx, *mainIdleModelWork) error) {
			t.Helper()
			n := &mainIdleModelWork{read: read, gate: make(chan struct{}), started: make(chan struct{}), returned: make(chan struct{})}
			nodes.Store(name, n)
			commit(func(tx *Tx) error { return create(tx, n) })
			await(n.started)
			if n.readErr != nil {
				t.Fatal(n.readErr)
			}
			live = append(live, n)
		}
		operations := [5]int{}
		checked := 0
		for step := 0; step < 200; step++ {
			action := next()
			name := fmt.Sprintf("t%d", step)
			switch {
			case action < 0.1:
				var cid ID
				commit(func(tx *Tx) error {
					var err error
					cid, err = tx.MintID()
					if err != nil {
						return err
					}
					if err = tx.CreateConversation(Conversation{ID: cid}); err != nil {
						return err
					}
					return note(tx, cid)
				})
				conversations = append(conversations, cid)
				operations[0]++
			case action < 0.45 || len(live) == 0:
				into := conversations[pick(len(conversations))]
				var read ID
				if next() < 0.6 {
					read = conversations[pick(len(conversations))]
				}
				background := next() < 0.15
				spawn(name, read, func(tx *Tx, n *mainIdleModelWork) error {
					var err error
					n.conversation = into
					n.id, err = tx.CreateTask(def, name, TaskOptions{Conversation: into, Ownership: TaskOwnership{Kind: "conversation"}, Background: background})
					return err
				})
				operations[1]++
			case action < 0.6:
				owner := live[pick(len(live))]
				record, found, err := h.Task(bg, owner.id)
				if err != nil {
					t.Fatal(err)
				}
				if !found || record.State.Status == "terminal" || record.State.Status == "completing" {
					operations[4]++
					continue
				}
				child := next() < 0.5
				spawn(name, 0, func(tx *Tx, n *mainIdleModelWork) error {
					into := owner.conversation
					if child {
						var err error
						n.conversation = into
						n.id, err = tx.CreateTask(def, name, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: owner.id}})
						return err
					}
					var err error
					into, err = tx.MintID()
					if err != nil {
						return err
					}
					if err = tx.CreateConversation(Conversation{ID: into, Owner: owner.id}); err != nil {
						return err
					}
					if err = note(tx, into); err != nil {
						return err
					}
					n.conversation, n.read = into, into
					n.id, err = tx.CreateTask(def, name, TaskOptions{Conversation: into, Ownership: TaskOwnership{Kind: "conversation"}})
					return err
				})
				if !child {
					conversations = append(conversations, live[len(live)-1].conversation)
				}
				operations[2]++
			default:
				i := pick(len(live))
				ended := live[i]
				live = append(live[:i], live[i+1:]...)
				releaseTaskGate(ended.gate)
				await(ended.returned)
				operations[3]++
			}
			h.session.taskBookkeeping(func() {
				state := h.session.readImage
				for _, cid := range append([]ID{0}, conversations...) {
					expected := mainIdleModelTargets(state, h.scheduler.invocations, cid)
					actual := h.scheduler.idleTargets(state, cid)
					slices.Sort(actual)
					if !slices.Equal(actual, expected) {
						t.Fatalf("step%d conversation%d targets=%v want=%v", step, cid, actual, expected)
					}
					if r := h.session.contextRanges[cid]; r != nil && (r.idleSince == nil) != (len(expected) > 0) {
						t.Fatalf("step%d conversation%d cached idle disagrees with recompute", step, cid)
					}
				}
			})
			checked++
		}
		h.session.taskBookkeeping(func() {
			idle := 0
			for _, r := range h.session.contextRanges {
				if r.idleSince != nil {
					idle++
				}
			}
			if len(h.session.contextRanges) <= 5 || idle == 0 || idle >= len(h.session.contextRanges) {
				t.Fatalf("model needs mixed busy/idle contexts: total=%d idle=%d", len(h.session.contextRanges), idle)
			}
			t.Logf("model steps=200 checked=%d operations(ownerless,work,owned,end,skip)=%v contexts=%d idle=%d", checked, operations, len(h.session.contextRanges), idle)
		})
	})
}
