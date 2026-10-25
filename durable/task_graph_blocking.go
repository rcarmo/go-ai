package durable

import (
	"context"
	"strconv"
)

// No checkpoints, inputs, memos or outcome payloads are retained for metadata
// refreshes. These subjects are owned by the graph's serial subscription drain.
type taskGraphSubject struct {
	id                                ID
	kind, status, abortReason         string
	version, restarts                 uint64
	native, managed, aborted, decided bool
}

func graphSubject(task Task) taskGraphSubject {
	subject := taskGraphSubject{id: task.ID, kind: task.Kind, status: task.Status, abortReason: task.AbortReason, restarts: task.Restarts, managed: taskManaged(task), aborted: taskAborted(task), decided: taskHasDecidedOutcome(task)}
	if task.Execution != nil && task.Execution.Native != nil {
		subject.native = true
		subject.version = task.Execution.Native.Version
	}
	return subject
}
func graphSubjects(state Snapshot) map[string]taskGraphSubject {
	subjects := make(map[string]taskGraphSubject, len(state.Tasks))
	for _, task := range state.Tasks {
		if !terminalStatus(task.Status) {
			subjects[strconv.FormatUint(uint64(task.ID), 10)] = graphSubject(task)
		}
	}
	return subjects
}
func (s *taskScheduler) graphBlocked(subject taskGraphSubject, registry TaskRegistrySnapshot) string {
	if subject.status == "running" || terminalStatus(subject.status) || subject.decided {
		return ""
	}
	if subject.aborted && subject.abortReason != "restart" {
		return ""
	}
	if subject.native {
		if reason := s.definitionBlockedReason(subject.id, subject.kind, subject.version, registry); reason != "" {
			return reason
		}
	}
	if subject.managed && !subject.aborted && subject.restarts >= s.h.RestartLimit() {
		return "restart_limit"
	}
	return ""
}

// Caller holds the Session line, which owns the scheduler's migration failures.
func (h *Harness) applyGraphBlocking(graph *TaskGraph, subjects map[string]taskGraphSubject) error {
	registry, err := h.options.Registry.taskSnapshot(h.session.limits)
	if err != nil {
		return err
	}
	for key, node := range graph.Tasks {
		node.Blocked = h.scheduler.graphBlocked(subjects[key], registry)
		graph.Tasks[key] = node
	}
	return nil
}
func (w *TaskGraphWatch) refreshBlocking(ctx context.Context, graph *TaskGraph) error {
	if err := w.harness.session.enter(ctx); err != nil {
		return err
	}
	defer w.harness.session.leave()
	return w.harness.applyGraphBlocking(graph, w.subjects)
}

var graphMetadataReady = func() <-chan struct{} { ch := make(chan struct{}); close(ch); return ch }()

// Metadata wakes share the existing observer reservation, queue and serial
// callback runner. At most one payload-free metadata frame is pending; overflow
// may drop it safely because every subsequent frame refreshes live metadata.
func (p *CommitSubscription) queueGraphRefresh() {
	p.mu.Lock()
	if p.ended || !p.graph {
		p.mu.Unlock()
		return
	}
	for _, frame := range p.pending {
		if frame.graphRefresh {
			p.mu.Unlock()
			return
		}
	}
	p.pending = append(p.pending, PublicationFrame{graphRefresh: true, Context: context.Background(), ready: graphMetadataReady})
	p.sizes = append(p.sizes, 0)
	p.mu.Unlock()
	p.kick()
}
func (s *Session) refreshTaskGraphObservers() {
	s.observerMu.Lock()
	defer s.observerMu.Unlock()
	for sub := range s.subscriptions {
		sub.queueGraphRefresh()
	}
}
