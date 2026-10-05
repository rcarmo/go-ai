package durable

import (
	"context"
	"strconv"
	"sync"
)

// TaskGraph contains live tasks only, without inputs, memos or outcome payloads.
// Decimal keys match persisted task IDs. Returned graphs are detached.
type TaskGraph struct {
	Tasks map[string]TaskGraphNode `json:"tasks"`
}
type TaskGraphNode struct {
	ID             ID             `json:"id"`
	Kind           string         `json:"kind"`
	Conversation   ID             `json:"conversationId"`
	Owner          ID             `json:"owner,omitempty"`
	Background     bool           `json:"background"`
	AbortRequested bool           `json:"abortRequested"`
	State          TaskGraphState `json:"state"`
	Conversations  []ID           `json:"conversations"`
}
type TaskGraphState struct {
	Status  string `json:"status"`
	Phase   string `json:"phase,omitempty"`
	On      []ID   `json:"on,omitempty"`
	Policy  string `json:"policy,omitempty"`
	Outcome string `json:"outcome,omitempty"`
}

func graphNode(task Task, conversations []ID, limits Limits) (TaskGraphNode, error) {
	record, err := CanonicalTask(task, limits)
	if err != nil {
		return TaskGraphNode{}, err
	}
	state := TaskGraphState{Status: record.State.Status}
	if record.State.Outcome != nil {
		state.Outcome = record.State.Outcome.Status
	} else {
		state.Phase, _ = record.State.Checkpoint["phase"].(string)
		state.On = append([]ID(nil), record.State.On...)
		state.Policy = record.State.Policy
	}
	return TaskGraphNode{ID: task.ID, Kind: task.Kind, Conversation: task.Conversation, Owner: task.Owner, Background: record.Background, AbortRequested: record.AbortRequested, State: state, Conversations: conversations}, nil
}
func buildTaskGraph(state Snapshot, limits Limits) (TaskGraph, error) {
	graph := TaskGraph{Tasks: map[string]TaskGraphNode{}}
	for _, id := range ids(state.Tasks) {
		task := state.Tasks[id]
		if terminalStatus(task.Status) {
			continue
		}
		conversations := []ID{}
		for _, cid := range ids(state.Conversations) {
			if state.Conversations[cid].Owner == id {
				conversations = append(conversations, cid)
			}
		}
		node, err := graphNode(task, conversations, limits)
		if err != nil {
			return TaskGraph{}, err
		}
		graph.Tasks[strconv.FormatUint(uint64(id), 10)] = node
	}
	return graph, nil
}
func copyTaskGraph(graph TaskGraph) TaskGraph {
	result := TaskGraph{Tasks: make(map[string]TaskGraphNode, len(graph.Tasks))}
	for key, node := range graph.Tasks {
		node.Conversations = append([]ID{}, node.Conversations...)
		node.State.On = append([]ID(nil), node.State.On...)
		result.Tasks[key] = node
	}
	return result
}

// TaskGraph reads the committed graph without enabling the scheduler.
func (h *Harness) TaskGraph(ctx context.Context) (TaskGraph, error) {
	var graph TaskGraph
	err := h.session.readTasks(ctx, func(state Snapshot) error {
		if h.closing.Load() {
			return ErrClosed
		}
		var err error
		graph, err = buildTaskGraph(state, h.session.limits)
		return err
	})
	return graph, err
}

// TaskGraphWatch delivers graph-changing committed revisions serially off-line.
// It uses the Session's atomic baseline/registration and bounded resnapshot queue.
// Checkpoint payload-only changes generate no graph callback.
type TaskGraphWatch struct {
	mu      sync.Mutex
	current TaskGraph
	sub     *CommitSubscription
	limits  Limits
}

func (h *Harness) WatchTaskGraph(ctx context.Context) (*TaskGraphWatch, error) {
	if h.closing.Load() {
		return nil, ErrClosed
	}
	state, sub, err := h.session.SubscribeCommits(ctx)
	if err != nil {
		return nil, err
	}
	graph, err := buildTaskGraph(state, h.session.limits)
	if err != nil || h.closing.Load() {
		sub.Stop()
		if err == nil {
			err = ErrClosed
		}
		return nil, err
	}
	return &TaskGraphWatch{current: graph, sub: sub, limits: h.session.limits}, nil
}
func (w *TaskGraphWatch) Value() TaskGraph {
	w.mu.Lock()
	defer w.mu.Unlock()
	return copyTaskGraph(w.current)
}
func (w *TaskGraphWatch) Closed() <-chan struct{} { return w.sub.Closed() }
func (w *TaskGraphWatch) End() (WatchEnd, bool)   { return w.sub.End() }
func (w *TaskGraphWatch) Stop() WatchEnd          { return w.sub.Stop() }
func (w *TaskGraphWatch) Start(listener func(context.Context, TaskGraph, []Operation) error) error {
	if listener == nil {
		return reject("nil task graph listener")
	}
	return w.sub.Start(func(ctx context.Context, frame PublicationFrame) error {
		w.mu.Lock()
		before := w.current
		next := copyTaskGraph(before)
		var err error
		if frame.Snapshot != nil {
			next, err = buildTaskGraph(*frame.Snapshot, w.limits)
		} else {
			for _, write := range frame.Publication.Tables {
				if write.Task == nil {
					continue
				}
				task := *write.Task
				key := strconv.FormatUint(uint64(task.ID), 10)
				if terminalStatus(task.Status) {
					delete(next.Tasks, key)
					continue
				}
				owned := next.Tasks[key].Conversations
				if owned == nil {
					owned = []ID{}
				}
				node, e := graphNode(task, owned, w.limits)
				if e != nil {
					err = e
					break
				}
				next.Tasks[key] = node
			}
			if err == nil {
				for _, write := range frame.Publication.Tables {
					if write.Conversation == nil || write.Conversation.Owner == 0 {
						continue
					}
					key := strconv.FormatUint(uint64(write.Conversation.Owner), 10)
					node, ok := next.Tasks[key]
					if !ok {
						continue
					}
					found := false
					for _, id := range node.Conversations {
						if id == write.Conversation.ID {
							found = true
						}
					}
					if !found {
						node.Conversations = append(node.Conversations, write.Conversation.ID)
					}
					node.Conversations = idsFromSlice(node.Conversations)
					next.Tasks[key] = node
				}
			}
		}
		var ops []Operation
		if err == nil {
			a, e := dtoObject(before, w.limits)
			err = e
			if err == nil {
				b, e := dtoObject(next, w.limits)
				err = e
				if err == nil {
					ops, err = diffOperations(a, b, w.limits)
				}
			}
		}
		if err == nil {
			w.current = next
		}
		value := copyTaskGraph(next)
		w.mu.Unlock()
		if err != nil {
			return err
		}
		if len(ops) == 0 {
			return nil
		}
		return listener(ctx, value, ops)
	})
}
func idsFromSlice(values []ID) []ID {
	set := map[ID]bool{}
	for _, id := range values {
		set[id] = true
	}
	return ids(set)
}
