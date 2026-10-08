package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sort"
	"strings"
	"sync"
)

// TaskPhase runs off the Session line. A handler must commit durable checkpoint
// progress, waiting or an outcome through its invocation-bound TaskRuntime.
type TaskPhase func(context.Context, TaskRecord, *TaskRuntime) error

type TaskDefinitionOptions struct {
	Kind    string
	Version uint64
	Initial func(any) (JSON, error)
	Phases  map[string]TaskPhase
	Abort   TaskPhase
	Migrate func(any, JSON, uint64) (any, JSON, error)
}

// TaskDefinition copies its phase map. Functions remain process-local host code;
// definition-token identity, not hashes of callbacks, controls reload retries.
type TaskDefinition struct {
	options     TaskDefinitionOptions
	constructed bool
}

func DefineTask(options TaskDefinitionOptions) (*TaskDefinition, error) {
	if !validKind(options.Kind) || strings.HasPrefix(options.Kind, "pi.") || options.Version == 0 || options.Version > MaxID || options.Initial == nil || options.Abort == nil || len(options.Phases) == 0 || len(options.Phases) > DefaultLimits().MaxMembers {
		return nil, reject("invalid task definition")
	}
	phases := make(map[string]TaskPhase, len(options.Phases))
	for name, handler := range options.Phases {
		if !validKind(name) || handler == nil {
			return nil, reject("invalid task phase")
		}
		phases[name] = handler
	}
	options.Phases = phases
	return &TaskDefinition{options: options, constructed: true}, nil
}

func (definition *TaskDefinition) Kind() string {
	if definition == nil {
		return ""
	}
	return definition.options.Kind
}

func (definition *TaskDefinition) Version() uint64 {
	if definition == nil {
		return 0
	}
	return definition.options.Version
}

func (definition *TaskDefinition) initial(input any, l Limits) (*NativeTaskExecution, error) {
	if definition == nil || !definition.constructed {
		return nil, reject("nil task definition")
	}
	// Initial receives its own placement, independent of the persisted input.
	ownedInput, err := copyTaskValue(&TaskValue{Present: true, Value: input}, l)
	if err != nil {
		return nil, err
	}
	callbackInput, err := copyTaskValue(ownedInput, l)
	if err != nil {
		return nil, err
	}
	checkpoint, err := callTaskInitial(definition, callbackInput.Value)
	if err != nil {
		return nil, err
	}
	checkpoint, err = copyObject(checkpoint, l)
	if err != nil {
		return nil, err
	}
	state := TaskState{Status: "pending", Checkpoint: checkpoint}
	if err = validateTaskState(state); err != nil {
		return nil, err
	}
	phase := checkpoint["phase"].(string)
	if definition.options.Phases[phase] == nil {
		return nil, reject("initial task phase not defined")
	}
	return &NativeTaskExecution{Version: definition.options.Version, Input: ownedInput, State: state}, nil
}

func callTaskInitial(def *TaskDefinition, input any) (checkpoint JSON, err error) {
	defer func() {
		if recover() != nil {
			err = reject("task initializer panic")
		}
	}()
	return def.options.Initial(input)
}

// CreateTask creates a definition-backed task but never dispatches host code.
// Owner references are judged again on the transaction's final candidate.
func (tx *Tx) CreateTask(definition *TaskDefinition, input any, options TaskOptions) (ID, error) {
	if err := tx.enter(); err != nil {
		return 0, err
	}
	defer tx.leave()
	if definition == nil || !definition.constructed {
		return 0, reject("invalid task definition token")
	}
	state := tx.state
	var err error
	if len(tx.writes) > 0 {
		state, err = tx.current()
		if err != nil {
			return 0, err
		}
	}
	conversation := options.Conversation
	var owner ID
	switch options.Ownership.Kind {
	case "conversation":
		if options.Ownership.Task != 0 {
			return 0, reject("conversation-owned task owner union")
		}
		if conversation == 0 {
			conversation = tx.taskConversation
		}
	case "task":
		owner = options.Ownership.Task
		parent, ok := state.Tasks[owner]
		if owner == 0 || !ok {
			return 0, reject("task owner missing")
		}
		if options.Background || conversation != 0 && conversation != parent.Conversation {
			return 0, reject("child task conversation/background")
		}
		if !taskCanOwnNewWork(parent) {
			return 0, reject("task owner is decided or abort-marked")
		}
		conversation = parent.Conversation
	default:
		return 0, reject("task ownership explicitly required")
	}
	if _, ok := state.Conversations[conversation]; !ok {
		return 0, reject("task conversation required")
	}
	native, err := definition.initial(input, tx.limits)
	if err != nil {
		return 0, err
	}
	native.Background = options.Background
	id, err := tx.store.mintID(tx.ctx, true)
	if err != nil {
		return 0, err
	}
	tx.state.HighWater = uint64(id)
	task := Task{ID: id, Conversation: conversation, Owner: owner, Kind: definition.options.Kind, Status: "pending", Checkpoint: JSON{}, Execution: &TaskExecution{Tag: nativeTaskTag, Native: native}}
	if err = tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
		return 0, err
	}
	return id, nil
}

func taskCanOwnNewWork(task Task) bool {
	if terminalStatus(task.Status) || taskHasDecidedOutcome(task) {
		return false
	}
	if task.Execution != nil {
		if task.Execution.Native != nil && task.Execution.Native.AbortRequested {
			return false
		}
		if task.Execution.Builtin != nil && task.Execution.Builtin.AbortRequested {
			return false
		}
	}
	// cp.Abort remains authority even without new metadata.
	if aborted, _ := task.Checkpoint["abort"].(bool); aborted {
		return false
	}
	return true
}

// TaskRegistrySnapshot is an immutable phase selection with its captured Session
// limits. Each tool schema read is detached; function references and task tokens
// remain process-local. A zero snapshot has no tools and uses default limits.
type TaskRegistrySnapshot struct {
	definitions map[string]*TaskDefinition
	offers      []toolOffer
	pins        map[string]registeredTool
	limits      Limits
	selection   *Registry // immutable captured registration data for phase agents
}

func (s TaskRegistrySnapshot) Task(kind string) *TaskDefinition { return s.definitions[kind] }
func (s TaskRegistrySnapshot) Tools() ([]goai.Tool, error) {
	l := s.limits
	if l == (Limits{}) {
		l = DefaultLimits()
	}
	return protocolTools(s.offers, l)
}

func (r *Registry) taskListenersLocked() []func() {
	wake := make([]func(), 0, len(r.listeners))
	for _, notify := range r.listeners {
		wake = append(wake, notify)
	}
	return wake
}

func (r *Registry) registerBuiltinTask(definition *TaskDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tasks == nil {
		r.tasks = map[string]*TaskDefinition{}
	}
	// Builtin phases resolve their current Harness from TaskRuntime, so this
	// immutable definition can be reused across Harness reopen.
	if r.tasks[definition.Kind()] == nil {
		r.tasks[definition.Kind()] = definition
	}
	return nil
}

// RegisterTask atomically replaces one immutable token. Disposal removes only
// that registration, never a newer replacement of the same kind.
func (r *Registry) RegisterTask(definition *TaskDefinition) (func(), error) {
	if r == nil || definition == nil || !definition.constructed {
		return nil, reject("nil task registration")
	}
	if definition.options.Kind == "task.pi.compaction" {
		return nil, reject("builtin task cannot be replaced")
	}
	r.mu.Lock()
	for _, extension := range r.extensions {
		if extension.tasks[definition.options.Kind] != nil {
			r.mu.Unlock()
			return nil, reject("task collides with installed extension")
		}
	}
	if r.tasks == nil {
		r.tasks = map[string]*TaskDefinition{}
	}
	if _, exists := r.tasks[definition.options.Kind]; !exists && len(r.tasks) >= DefaultLimits().MaxPage {
		r.mu.Unlock()
		return nil, reject("task registry capacity")
	}
	if r.nextTaskRegistration >= MaxID {
		r.mu.Unlock()
		return nil, reject("task registration identity exhausted")
	}
	if r.taskRegistrations == nil {
		r.taskRegistrations = map[string]uint64{}
	}
	r.nextTaskRegistration++
	registration := r.nextTaskRegistration
	r.taskRegistrations[definition.options.Kind] = registration
	r.tasks[definition.options.Kind] = definition
	wake := r.taskListenersLocked()
	r.mu.Unlock()
	for _, notify := range wake {
		notify()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			if r.taskRegistrations[definition.options.Kind] != registration {
				r.mu.Unlock()
				return
			}
			delete(r.tasks, definition.options.Kind)
			delete(r.taskRegistrations, definition.options.Kind)
			wake := r.taskListenersLocked()
			r.mu.Unlock()
			for _, notify := range wake {
				notify()
			}
		})
	}, nil
}

// subscribeTasks is private scheduler wake delivery. No host code or commit
// runs in these callbacks, and Registry never holds its lock while waking.
func (r *Registry) subscribeTasks(notify func()) (func(), error) {
	if r == nil || notify == nil {
		return nil, reject("nil task registry listener")
	}
	r.mu.Lock()
	if len(r.listeners) >= DefaultLimits().MaxPage || r.nextListener >= MaxID {
		r.mu.Unlock()
		return nil, reject("task registry listener capacity")
	}
	if r.listeners == nil {
		r.listeners = map[uint64]func(){}
	}
	r.nextListener++
	id := r.nextListener
	r.listeners[id] = notify
	r.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); delete(r.listeners, id); r.mu.Unlock() }) }, nil
}
func (r *Registry) taskSnapshot(l Limits) (TaskRegistrySnapshot, error) {
	result := TaskRegistrySnapshot{definitions: map[string]*TaskDefinition{}, pins: map[string]registeredTool{}, limits: l}
	if r == nil {
		return result, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name, definition := range r.effectiveTasksLocked() {
		result.definitions[name] = definition
	}
	result.selection = &Registry{tools: make(map[string]registeredTool, len(r.tools)), toolOrder: append([]string{}, r.toolOrder...), extensions: append([]installedExtension(nil), r.extensions...)}
	for name, tool := range r.tools {
		result.selection.tools[name] = tool
	}
	tools := r.effectiveToolsLocked()
	for _, name := range sortedToolNames(tools) {
		tool := tools[name]
		schema, err := copyObject(tool.offer.Schema, l)
		if err != nil {
			return TaskRegistrySnapshot{}, err
		}
		tool.offer.Schema = schema
		result.offers = append(result.offers, tool.offer)
		result.pins[name] = tool
	}
	return result, nil
}
func sortedToolNames(tools map[string]registeredTool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CanonicalTask is the new runtime view of one raw task. Unknown legacy kinds
// have no executable definition/version/result inferred from object payloads.
func CanonicalTask(task Task, l Limits) (TaskRecord, error) {
	owned, err := copyTask(task, l)
	if err != nil {
		return TaskRecord{}, err
	}
	view := TaskRecord{StartedAt: owned.StartedAt, EndedAt: owned.EndedAt, ID: owned.ID, Conversation: owned.Conversation, Owner: owned.Owner, Kind: owned.Kind, Background: taskBackground(owned)}
	if owned.Execution != nil && owned.Execution.Native != nil {
		native := owned.Execution.Native
		view.Version = native.Version
		view.Input = native.Input
		view.AbortRequested = native.AbortRequested
		view.Memos = native.Memos
		view.State = native.State
		return view, nil
	}
	view.State = TaskState{Status: owned.Status, Checkpoint: owned.Checkpoint}
	if owned.Execution != nil && owned.Execution.Builtin != nil {
		builtin := owned.Execution.Builtin
		view.AbortRequested = builtin.AbortRequested
		view.Memos = builtin.Memos
		if builtin.Hold != nil {
			state := "completing"
			if builtin.Hold.Stage == "final" {
				state = "terminal"
			}
			view.State = TaskState{Status: state, Outcome: &builtin.Hold.Outcome}
		}
	}
	// Built-in version/phase/abort data come only from their original checkpoint;
	// it remains the sole authority for started effects and their replay policy.
	switch owned.Kind {
	case "pi.generation":
		var cp generationCheckpoint
		if err = fromObject(owned.Checkpoint, &cp, l); err != nil {
			return TaskRecord{}, err
		}
		view.Version = 1
		view.AbortRequested = view.AbortRequested || cp.Abort
		if !taskHasDecidedOutcome(owned) && !terminalStatus(owned.Status) && cp.Phase == "tools" {
			view.State = TaskState{Status: "waiting", Checkpoint: owned.Checkpoint, On: append([]ID(nil), cp.Children...), Policy: "allSettled"}
		}
	case "pi.tool":
		var cp toolCheckpoint
		if err = fromObject(owned.Checkpoint, &cp, l); err != nil {
			return TaskRecord{}, err
		}
		view.Version = 1
		view.AbortRequested = view.AbortRequested || cp.Abort
	}
	if terminalStatus(owned.Status) && !taskHasDecidedOutcome(owned) {
		// Old raw receipts remain accessible in Snapshot/Settlement. Do not
		// fabricate a generic completed result from absent execution metadata.
		view.State = TaskState{Status: "terminal"}
		view.Memos = nil
	}
	return view, nil
}
