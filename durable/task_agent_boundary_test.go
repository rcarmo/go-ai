package durable

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskAgentBoundaryDrainingBlocksLateResolverUntilNewSnapshot(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Install(&Extension{Name: "base", Tools: []ToolRegistration{wrapRegistration("base")}}); err != nil {
		t.Fatal(err)
	}
	committed, returnHost := make(chan *TaskRuntime, 1), make(chan struct{})
	enteredB, releaseB := make(chan struct{}), make(chan struct{})
	definition, err := DefineTask(TaskDefinitionOptions{Kind: "task.agent-gap", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "a"}, nil }, Abort: func(context.Context, TaskRecord, *TaskRuntime) error { return nil }, Phases: map[string]TaskPhase{
		"a": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "running", Checkpoint: JSON{"phase": "b"}}, nil
			}); err != nil {
				return err
			}
			committed <- r
			<-returnHost
			return nil
		},
		"b": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(enteredB)
			<-releaseB
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, mustMemory(t), Options{Registry: registry})
	cleanupTaskGates(t, returnHost, releaseB)
	conv := root(t, h, ModelRef{Provider: "openai", ID: "model"})
	id := createPublicTask(t, h, definition, nil, TaskOptions{Conversation: conv.ID(), Ownership: TaskOwnership{Kind: "conversation"}})
	if err := h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	r := <-committed
	// Hold the real Session line so the phase return can close admission but
	// cannot publish its next registry. This deterministically creates F1's gap.
	blockEntered, blockRelease := make(chan struct{}), make(chan struct{})
	cleanupTaskGates(t, blockRelease)
	blocked := make(chan error, 1)
	go func() {
		_, err := h.session.taskCommit(bg, func(*Tx) error { close(blockEntered); <-blockRelease; return nil })
		blocked <- err
	}()
	awaitTaskSignal(t, blockEntered)
	close(returnHost)
	caller, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	for {
		r.phaseMu.RLock()
		draining := r.phaseDraining
		r.phaseMu.RUnlock()
		if draining {
			break
		}
		select {
		case <-caller.Done():
			t.Fatal("boundary never drained")
		default:
		}
	}
	cancelled, cancelWait := context.WithCancel(caller)
	cancelProbe := &phaseDrainingContext{Context: cancelled, reached: make(chan struct{})}
	cancelResult := make(chan error, 1)
	go func() { _, err := r.Agent(cancelProbe); cancelResult <- err }()
	awaitTaskSignal(t, cancelProbe.reached)
	cancelWait()
	if err := <-cancelResult; !errors.Is(err, context.Canceled) {
		t.Fatal("draining caller cancellation", err)
	}
	agentResult := make(chan TaskAgent, 1)
	agentErr := make(chan error, 1)
	probe := &phaseDrainingContext{Context: caller, reached: make(chan struct{})}
	go func() { value, err := r.Agent(probe); agentResult <- value; agentErr <- err }()
	// Done is first evaluated in phaseAgent's draining select (Err is not
	// called before admission). This witnesses gate entry, not goroutine launch.
	awaitTaskSignal(t, probe.reached)
	r.phaseMu.RLock()
	if r.agentResolution != nil {
		t.Error("late old resolver admitted")
	}
	r.phaseMu.RUnlock()
	if err := registry.Install(&Extension{Name: "late", Tools: []ToolRegistration{wrapRegistration("late")}}); err != nil {
		t.Fatal(err)
	}
	close(blockRelease)
	if err := <-blocked; err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, enteredB)
	value := <-agentResult
	if err := <-agentErr; err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tool := range value.Tools {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "base,late" {
		t.Fatal("late caller used old phase registry", names)
	}
	close(releaseB)
	waitPublicTask(t, h, id)
}

type phaseDrainingContext struct {
	context.Context
	once    sync.Once
	reached chan struct{}
}

func (c *phaseDrainingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.reached) })
	return c.Context.Done()
}

func TestTaskAgentBoundaryRetainsPreAdmittedBlockedWrapperAcrossClose(t *testing.T) {
	registry := NewRegistry()
	wrapperEntered, wrapperRelease := make(chan struct{}), make(chan struct{})
	var wrappers, nextPhases atomic.Int64
	if err := registry.Install(&Extension{Name: "base", Tools: []ToolRegistration{wrapRegistration("base")}, Wraps: []ExtensionWrap{{Tool: "base", WrapTool: func(reg ToolRegistration) (ToolRegistration, error) {
		wrappers.Add(1)
		close(wrapperEntered)
		<-wrapperRelease
		return reg, nil
	}}}}); err != nil {
		t.Fatal(err)
	}
	captured, returnHost := make(chan *TaskRuntime, 1), make(chan struct{})
	definition, err := DefineTask(TaskDefinitionOptions{Kind: "task.agent-owned-gap", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "a"}, nil }, Abort: func(context.Context, TaskRecord, *TaskRuntime) error { return nil }, Phases: map[string]TaskPhase{
		"a": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "running", Checkpoint: JSON{"phase": "b"}}, nil
			}); err != nil {
				return err
			}
			captured <- r
			<-returnHost
			return nil
		},
		"b": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			nextPhases.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, mustMemory(t), Options{Registry: registry})
	cleanupTaskGates(t, returnHost, wrapperRelease)
	conv := root(t, h, ModelRef{Provider: "openai", ID: "model"})
	createPublicTask(t, h, definition, nil, TaskOptions{Conversation: conv.ID(), Ownership: TaskOwnership{Kind: "conversation"}})
	if err := h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	r := <-captured
	callerDone := make(chan error, 1)
	go func() { _, err := r.Agent(bg); callerDone <- err }()
	awaitTaskSignal(t, wrapperEntered)
	close(returnHost)
	deadline, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	for {
		r.phaseMu.RLock()
		draining := r.phaseDraining
		r.phaseMu.RUnlock()
		if draining {
			break
		}
		select {
		case <-deadline.Done():
			t.Fatal("boundary failed to drain")
		default:
		}
	}
	lateProbe := &phaseDrainingContext{Context: bg, reached: make(chan struct{})}
	lateResult := make(chan error, 1)
	go func() { _, err := r.Agent(lateProbe); lateResult <- err }()
	awaitTaskSignal(t, lateProbe.reached)
	closed := make(chan error, 1)
	go func() { closed <- h.Close(bg) }()
	<-h.life.Done()
	if err := <-lateResult; !errors.Is(err, ErrSealed) && !errors.Is(err, context.Canceled) {
		t.Fatal("draining invocation cancellation", err)
	}
	select {
	case <-r.done:
		t.Fatal("resolver escaped invocation join")
	default:
	}
	select {
	case err := <-closed:
		t.Fatal("Close abandoned resolver", err)
	default:
	}
	if nextPhases.Load() != 0 {
		t.Fatal("next phase overlapped old wrapper")
	}
	close(wrapperRelease)
	<-callerDone
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, r.done)
	if wrappers.Load() != 1 || nextPhases.Load() != 0 {
		t.Fatal(wrappers.Load(), nextPhases.Load())
	}
}
