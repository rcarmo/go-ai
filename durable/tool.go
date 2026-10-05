package durable

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"math"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const MaxTools = 16
const MaxToolOutputBytes = 32 << 10

// ToolRegistration is copied at registration. Version/Implementation identify
// code selected for an intent. ReplaySafe defaults false; external idempotency
// remains the host's responsibility even when both replay policies permit it.
type ToolRegistration struct {
	Definition     goai.Tool
	Implementation string
	Version        uint64
	ReplaySafe     bool
	ExecutionMode  string
	// PrepareArguments repairs detached model arguments before validation. Its
	// result is persisted as intent; it never reruns for a started recovery.
	PrepareArguments func(context.Context, JSON) (JSON, error)
	// Nil preserves native overflow rejection. Configured limits retain a
	// bounded head/tail window and report the discarded progress counts.
	OutputLimits *ToolOutputLimits
	// Validator is required for schemas outside the enforced native subset. It
	// returns final rewritten arguments before intent and is NOT rerun on recovery.
	Validator func(context.Context, JSON) (JSON, error)
	Execute   func(context.Context, JSON, *ToolAPI) (ToolResult, error)
}
type toolOffer struct {
	Name                string            `json:"name"`
	Description         string            `json:"description"`
	Schema              JSON              `json:"schema"`
	Implementation      string            `json:"implementation"`
	Version             uint64            `json:"version"`
	ReplaySafe          bool              `json:"replaySafe"`
	HostValidation      bool              `json:"hostValidation,omitempty"`
	ExecutionMode       string            `json:"executionMode,omitempty"`
	ArgumentPreparation bool              `json:"argumentPreparation,omitempty"`
	OutputLimits        *ToolOutputLimits `json:"outputLimits,omitempty"`
}
type registeredTool struct {
	offer     toolOffer
	execute   func(context.Context, JSON, *ToolAPI) (ToolResult, error)
	validator func(context.Context, JSON) (JSON, error)
	prepare   func(context.Context, JSON) (JSON, error)
}
type Registry struct {
	mu                   sync.RWMutex
	tools                map[string]registeredTool
	tasks                map[string]*TaskDefinition
	taskRegistrations    map[string]uint64
	nextTaskRegistration uint64
	listeners            map[uint64]func()
	nextListener         uint64
	extensions           []installedExtension
}

func NewRegistry() *Registry { return &Registry{tools: map[string]registeredTool{}} }
func (r *Registry) Register(reg ToolRegistration) error {
	if r == nil {
		return reject("nil registry")
	}
	l := DefaultLimits()
	if reg.ExecutionMode != "" && reg.ExecutionMode != "parallel" && reg.ExecutionMode != "sequential" {
		return reject("invalid tool execution mode")
	}
	if !validKind(reg.Definition.Name) || !validKind(reg.Implementation) || reg.Version < 1 || reg.Version > MaxID || reg.Execute == nil || reg.Definition.ConstrainedSampling != nil {
		return reject("invalid tool registration")
	}
	var schema JSON
	if e := decodeStrict(reg.Definition.Parameters, l, l.MaxDocumentBytes, &schema); e != nil {
		return e
	}
	schema, e := copyObject(schema, l)
	if e != nil {
		return e
	}
	if schema["type"] != "object" {
		return reject("tool schema root must be object")
	}
	subsetErr := validateSchema(schema, 1)
	if subsetErr != nil && (!errors.Is(subsetErr, ErrUnsupported) || reg.Validator == nil) {
		return subsetErr
	}
	var output *ToolOutputLimits
	if reg.OutputLimits != nil {
		copy := *reg.OutputLimits
		if err := validateOutputLimits(copy); err != nil {
			return err
		}
		output = &copy
	}
	offer := toolOffer{reg.Definition.Name, reg.Definition.Description, schema, reg.Implementation, reg.Version, reg.ReplaySafe, subsetErr != nil, reg.ExecutionMode, reg.PrepareArguments != nil, output}
	if _, e = dtoObject(offer, l); e != nil {
		return e
	}
	r.mu.Lock()
	if r.tools == nil {
		r.tools = map[string]registeredTool{}
	}
	if _, exists := r.tools[offer.Name]; !exists && len(r.tools) >= MaxTools {
		r.mu.Unlock()
		return reject("tool registry limit")
	}
	r.tools[offer.Name] = registeredTool{offer, reg.Execute, reg.Validator, reg.PrepareArguments}
	wake := r.taskListenersLocked()
	r.mu.Unlock()
	for _, notify := range wake {
		notify()
	}
	return nil
}
func (r *Registry) Remove(name string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	_, existed := r.tools[name]
	delete(r.tools, name)
	wake := r.taskListenersLocked()
	r.mu.Unlock()
	if existed {
		for _, notify := range wake {
			notify()
		}
	}
}
func (r *Registry) current(name string) (registeredTool, bool) {
	if r == nil {
		return registeredTool{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.effectiveToolsLocked()[name]
	return v, ok
}
func protocolTools(offers []toolOffer, l Limits) ([]goai.Tool, error) {
	tools := make([]goai.Tool, 0, len(offers))
	for _, v := range offers {
		p, e := encodeBounded(v.Schema, l, l.MaxDocumentBytes)
		if e != nil {
			return nil, e
		}
		tools = append(tools, goai.Tool{Name: v.Name, Description: v.Description, Parameters: json.RawMessage(p)})
	}
	return tools, nil
}

func validateSchema(s JSON, depth int) error {
	if depth > 16 {
		return reject("schema depth limit")
	}
	typ, ok := s["type"].(string)
	if !ok {
		return reject("schema type required")
	}
	allowed := map[string]bool{"type": true, "description": true, "enum": true}
	switch typ {
	case "object":
		allowed["properties"] = true
		allowed["required"] = true
		allowed["additionalProperties"] = true
	case "array":
		allowed["items"] = true
		allowed["minItems"] = true
		allowed["maxItems"] = true
	case "string":
		allowed["minLength"] = true
		allowed["maxLength"] = true
		allowed["pattern"] = true
	case "number", "integer":
		allowed["minimum"] = true
		allowed["maximum"] = true
	case "boolean", "null":
	default:
		return reject("unsupported schema type")
	}
	unsupported := false
	for key := range s {
		if !allowed[key] {
			unsupported = true
		}
	}
	if v, ok := s["description"]; ok {
		if _, ok = v.(string); !ok {
			return reject("schema description")
		}
	}
	if v, ok := s["enum"]; ok {
		values, ok := v.([]any)
		if !ok || len(values) == 0 {
			return reject("schema enum")
		}
		for _, v := range values {
			if !schemaType(v, typ) {
				return reject("schema enum type")
			}
		}
	}
	number := func(key string, integer bool) error {
		if v, ok := s[key]; ok {
			n, ok := asNumber(v)
			rat, exact := exactNumber(v)
			if !ok || !exact || math.IsNaN(n) || math.IsInf(n, 0) || (integer && (!rat.IsInt() || n < 0 || n > 65536)) {
				return reject("schema numeric constraint")
			}
		}
		return nil
	}
	switch typ {
	case "object":
		props := map[string]any{}
		if v, ok := s["properties"]; ok {
			var valid bool
			props, valid = v.(map[string]any)
			if !valid {
				return reject("schema properties")
			}
		}
		for _, v := range props {
			obj, ok := v.(map[string]any)
			if !ok {
				return reject("schema property")
			}
			if e := validateSchema(JSON(obj), depth+1); e != nil {
				return e
			}
		}
		if v, ok := s["required"]; ok {
			required, ok := v.([]any)
			if !ok {
				return reject("schema required")
			}
			seen := map[string]bool{}
			for _, v := range required {
				name, ok := v.(string)
				if !ok || seen[name] {
					return reject("schema required key")
				}
				if _, ok = props[name]; !ok {
					return reject("schema required unknown property")
				}
				seen[name] = true
			}
		}
		if v, ok := s["additionalProperties"]; ok {
			if _, ok = v.(bool); !ok {
				return reject("schema additionalProperties")
			}
		}
	case "array":
		v, ok := s["items"].(map[string]any)
		if !ok {
			return reject("schema items required")
		}
		if e := validateSchema(JSON(v), depth+1); e != nil {
			return e
		}
		if e := number("minItems", true); e != nil {
			return e
		}
		if e := number("maxItems", true); e != nil {
			return e
		}
	case "string":
		if e := number("minLength", true); e != nil {
			return e
		}
		if e := number("maxLength", true); e != nil {
			return e
		}
		if v, ok := s["pattern"]; ok {
			p, ok := v.(string)
			if !ok || len(p) > 4096 {
				return reject("schema pattern")
			}
			if _, e := regexp.Compile(p); e != nil {
				return reject("schema pattern")
			}
		}
	case "number", "integer":
		if e := number("minimum", false); e != nil {
			return e
		}
		if e := number("maximum", false); e != nil {
			return e
		}
	}
	for _, pair := range [][2]string{{"minimum", "maximum"}, {"minLength", "maxLength"}, {"minItems", "maxItems"}} {
		ar, aok := exactNumber(s[pair[0]])
		br, bok := exactNumber(s[pair[1]])
		if aok && bok && ar.Cmp(br) > 0 {
			return reject("incoherent schema bounds")
		}
	}
	if unsupported {
		return ErrUnsupported
	}
	return nil
}
func asNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		x, e := n.Float64()
		return x, e == nil && math.Abs(x) <= float64(MaxID)
	case float64:
		return n, math.Abs(n) <= float64(MaxID)
	case int:
		return float64(n), n >= -int(MaxID) && n <= int(MaxID)
	}
	return 0, false
}
func schemaType(v any, typ string) bool {
	switch typ {
	case "object":
		switch v.(type) {
		case JSON, map[string]any:
			return true
		}
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "null":
		return v == nil
	case "number", "integer":
		n, ok := asNumber(v)
		rat, exact := exactNumber(v)
		return ok && !math.IsNaN(n) && !math.IsInf(n, 0) && exact && (typ != "integer" || rat.IsInt())
	}
	return false
}
func checkSchema(s JSON, v any, depth int) error {
	if depth > 16 || !schemaType(v, s["type"].(string)) {
		return reject("tool arguments schema type")
	}
	if enum, ok := s["enum"].([]any); ok {
		found := false
		for _, candidate := range enum {
			if equalJSONValue(candidate, v) {
				found = true
				break
			}
		}
		if !found {
			return reject("tool arguments enum")
		}
	}
	bound := func(key string, n float64, lower bool) error {
		if limit, ok := asNumber(s[key]); ok && ((lower && n < limit) || (!lower && n > limit)) {
			return reject("tool arguments bounds")
		}
		return nil
	}
	switch s["type"] {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			obj = map[string]any(v.(JSON))
		}
		props, _ := s["properties"].(map[string]any)
		if required, ok := s["required"].([]any); ok {
			for _, name := range required {
				if _, ok = obj[name.(string)]; !ok {
					return reject("tool arguments required")
				}
			}
		}
		for key, value := range obj {
			schema, known := props[key]
			if !known {
				if allow, ok := s["additionalProperties"].(bool); ok && !allow {
					return reject("tool arguments extra property")
				}
				continue
			}
			if e := checkSchema(JSON(schema.(map[string]any)), value, depth+1); e != nil {
				return e
			}
		}
	case "array":
		items := v.([]any)
		if e := bound("minItems", float64(len(items)), true); e != nil {
			return e
		}
		if e := bound("maxItems", float64(len(items)), false); e != nil {
			return e
		}
		for _, value := range items {
			if e := checkSchema(JSON(s["items"].(map[string]any)), value, depth+1); e != nil {
				return e
			}
		}
	case "string":
		text := v.(string)
		n := float64(len([]rune(text)))
		if e := bound("minLength", n, true); e != nil {
			return e
		}
		if e := bound("maxLength", n, false); e != nil {
			return e
		}
		if pattern, ok := s["pattern"].(string); ok {
			matched, _ := regexp.MatchString(pattern, text)
			if !matched {
				return reject("tool arguments pattern")
			}
		}
	case "number", "integer":
		n, ok := exactNumber(v)
		if !ok {
			return reject("tool arguments number")
		}
		for _, constraint := range []string{"minimum", "maximum"} {
			if raw, exists := s[constraint]; exists {
				limit, ok := exactNumber(raw)
				if !ok {
					return reject("schema number")
				}
				cmp := n.Cmp(limit)
				if constraint == "minimum" && cmp < 0 || constraint == "maximum" && cmp > 0 {
					return reject("tool arguments bounds")
				}
			}
		}
	}
	return nil
}

// ToolResult application updates and usage are adopted with the tool outcome.
// Commit callbacks do not escape invocation scope. Error text is a fixed code.
type ToolResult struct {
	Content string
	// Blocks adds strict text/image content to the tool receipt. Content remains
	// the backwards-compatible text field; streamed prefixes precede Blocks.
	Blocks  []goai.ContentBlock
	Details JSON
	// Diagnostics are model/UI remarks, separate from application details.
	Diagnostics []ToolDiagnostic
	IsError     bool
	Usage       *goai.Usage
	Commit      func(*Tx) error
}
type toolCheckpoint struct {
	Offer            toolOffer        `json:"offer"`
	CallID           string           `json:"callId"`
	Arguments        JSON             `json:"arguments"`
	Started          bool             `json:"started"`
	Abort            bool             `json:"abort,omitempty"`
	ErrorCode        string           `json:"errorCode,omitempty"`
	ReportedError    bool             `json:"reportedError,omitempty"`
	Output           string           `json:"output"`
	Details          any              `json:"details,omitempty"`
	HasDetails       bool             `json:"hasDetails,omitempty"`
	DroppedBytes     uint64           `json:"droppedBytes,omitempty"`
	DroppedLines     uint64           `json:"droppedLines,omitempty"`
	OutputRaw        string           `json:"outputRaw,omitempty"`
	OutputFull       bool             `json:"outputFull,omitempty"`
	OutputBytes      uint64           `json:"outputBytes,omitempty"`
	OutputNewlines   uint64           `json:"outputNewlines,omitempty"`
	OutputTerminated bool             `json:"outputTerminated,omitempty"`
	Diagnostics      []ToolDiagnostic `json:"diagnostics,omitempty"`
	Result           *MessageReceipt  `json:"result,omitempty"`
}

// ToolAPI publishes only bounded committed prefixes while the invocation lives.
// Output is fenced after return/abort; application mutation belongs to Commit.
type ToolAPI struct {
	mu         sync.Mutex
	h          *Harness
	task       Task
	checkpoint toolCheckpoint
	active     bool
	ctx        context.Context
	runtime    *TaskRuntime
}

func (a *ToolAPI) Output(text string) error {
	return a.updateProgress(func(cp *toolCheckpoint, l Limits) error {
		if cp.Offer.OutputLimits != nil {
			return retainToolOutput(cp, text, *cp.Offer.OutputLimits, false)
		}
		if len(text) > MaxToolOutputBytes-len(cp.Output) {
			return reject("tool output limit")
		}
		cp.Output += text
		return nil
	})
}

// SetOutput replaces a retained output window; it is bounded and fenced like
// Output, and does not change the final ToolResult contract.
func (a *ToolAPI) SetOutput(text string) error {
	return a.updateProgress(func(cp *toolCheckpoint, l Limits) error {
		if cp.Offer.OutputLimits != nil {
			return retainToolOutput(cp, text, *cp.Offer.OutputLimits, true)
		}
		if len(text) > MaxToolOutputBytes {
			return reject("tool output limit")
		}
		cp.Output = text
		return nil
	})
}

// Details commits detached UI metadata. Nil explicitly clears earlier details.
func (a *ToolAPI) Details(value any) error {
	return a.updateProgress(func(cp *toolCheckpoint, l Limits) error {
		owned, err := ownJSONValue(value, l)
		if err != nil {
			return err
		}
		cp.Details, cp.HasDetails = owned, true
		return nil
	})
}

// Diagnostic records a detached model/UI remark in committed progress.
func (a *ToolAPI) Diagnostic(diagnostic ToolDiagnostic) error {
	return a.updateProgress(func(cp *toolCheckpoint, l Limits) error {
		if err := validateToolDiagnostics([]ToolDiagnostic{diagnostic}, l); err != nil {
			return err
		}
		if len(cp.Diagnostics) >= l.MaxMembers {
			return reject("tool diagnostic limit")
		}
		cp.Diagnostics = append(cp.Diagnostics, diagnostic)
		return nil
	})
}
func (a *ToolAPI) updateProgress(change func(*toolCheckpoint, Limits) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.active {
		return ErrSealed
	}
	if e := a.ctx.Err(); e != nil {
		return e
	}
	var task Task
	var candidate toolCheckpoint
	_, e := a.h.session.invocationCommit(context.Background(), a.task.ID, func(tx *Tx) error {
		if err := a.runtime.check(); err != nil {
			return err
		}
		if a.h.closing.Load() {
			return ErrClosed
		}
		current, ok := tx.state.Tasks[a.task.ID]
		if !ok || current.Status != "running" || taskHasDecidedOutcome(current) {
			return ErrSealed
		}
		if taskAborted(current) {
			return reject("tool aborted")
		}
		var err error
		task, err = copyTask(current, tx.limits)
		if err != nil {
			return err
		}
		if err := fromObject(task.Checkpoint, &candidate, tx.limits); err != nil {
			return err
		}
		if err := change(&candidate, tx.limits); err != nil {
			return err
		}
		task.Checkpoint, err = dtoObject(candidate, tx.limits)
		if err != nil {
			return err
		}
		return tx.stage(Write{Op: "put-task", Task: &task})
	})
	if e == nil {
		a.checkpoint = candidate
		a.task = task
	}
	return e
}
func (a *ToolAPI) seal()      { a.mu.Lock(); a.active = false; a.mu.Unlock() }
func (a *ToolAPI) TaskID() ID { a.mu.Lock(); defer a.mu.Unlock(); return a.task.ID }

// Additive runtime forwarding never changes the restricted atomic ToolResult
// Commit callback. Execution replacement stays private even inside these Txs.
func (a *ToolAPI) Commit(ctx context.Context, callback func(*Tx) error) error {
	return a.runtime.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) { return nil, callback(tx) })
}
func (a *ToolAPI) CreateTask(ctx context.Context, def *TaskDefinition, input any, options TaskOptions) (ID, error) {
	var id ID
	err := a.Commit(ctx, func(tx *Tx) error { var err error; id, err = tx.CreateTask(def, input, options); return err })
	return id, err
}
func (a *ToolAPI) WaitForTask(ctx context.Context, id ID) (TaskRecord, error) {
	return a.runtime.WaitForTask(ctx, id)
}
func (a *ToolAPI) Task(ctx context.Context, id ID) (TaskRecord, bool, error) {
	return a.runtime.Task(ctx, id)
}
func (a *ToolAPI) Memo(ctx context.Context, name string) (any, bool, error) {
	return a.runtime.Memo(ctx, name)
}
func (a *ToolAPI) MemoCandidate(ctx context.Context, name string, value any) (any, error) {
	return a.runtime.MemoCandidate(ctx, name, value)
}
func (a *ToolAPI) Conversation(ctx context.Context, id ID) (*InvocationConversation, error) {
	return a.runtime.Conversation(ctx, id)
}
func (a *ToolAPI) WatchDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (*DocumentWatch, error) {
	return a.runtime.WatchDefinition(ctx, def, owner, key)
}
func (a *ToolAPI) SnapshotDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (JSON, bool, error) {
	return a.runtime.SnapshotDefinition(ctx, def, owner, key)
}
func (a *ToolAPI) SnapshotDefinitionAsOf(ctx context.Context, def *DocumentDefinition, owner ID, key *string, at ID) (JSON, bool, error) {
	return a.runtime.SnapshotDefinitionAsOf(ctx, def, owner, key, at)
}

func sameImplementation(a, b toolOffer) bool {
	return a.Name == b.Name && a.Implementation == b.Implementation && a.Version == b.Version && reflect.DeepEqual(a.Schema, b.Schema) && a.HostValidation == b.HostValidation && a.ExecutionMode == b.ExecutionMode && a.ArgumentPreparation == b.ArgumentPreparation && reflect.DeepEqual(a.OutputLimits, b.OutputLimits)
}

// Recover only application validation code; argument detachment, schema checks
// and durable admissions execute outside this host callback fence.
func callToolValidator(validate func(context.Context, JSON) (JSON, error), ctx context.Context, args JSON) (owned JSON, err error) {
	defer func() {
		if recover() != nil {
			owned = nil
			err = reject("tool validator callback panic")
		}
	}()
	return validate(ctx, args)
}

func validateArguments(ctx context.Context, reg registeredTool, args JSON, l Limits) (JSON, error) {
	owned, e := copyObject(args, l)
	if e != nil {
		return nil, e
	}
	if reg.prepare != nil {
		owned, e = callToolValidator(reg.prepare, ctx, owned)
		if e != nil {
			return nil, reject("tool argument preparation failed")
		}
		owned, e = copyObject(owned, l)
		if e != nil {
			return nil, e
		}
	}
	if reg.validator != nil {
		owned, e = callToolValidator(reg.validator, ctx, owned)
		if e != nil {
			return nil, reject("tool validation failed")
		}
		owned, e = copyObject(owned, l)
		if e != nil {
			return nil, e
		}
	}
	if !reg.offer.HostValidation {
		if e = checkSchema(reg.offer.Schema, owned, 1); e != nil {
			return nil, e
		}
	}
	return owned, nil
}

func (h *Harness) acceptTools(parent Task, cp generationCheckpoint, message messageReceipt) error {
	if cp.Round >= 16 {
		error := errorReceipt("tool_round_limit")
		error.Usage = message.Usage
		return h.finish(parent, cp, error, false)
	}
	h.mu.Lock()
	pins := h.pins[parent.ID]
	h.mu.Unlock()
	calls := []goai.ContentBlock{}
	seen := map[string]bool{}
	for _, call := range message.Content {
		if call.Type != "toolCall" {
			continue
		}
		if seen[call.ID] {
			return h.finish(parent, cp, messageReceipt{Role: goai.RoleAssistant, Content: []goai.ContentBlock{}, Usage: message.Usage, StopReason: goai.StopReasonError, ErrorCode: "duplicate_tool_call"}, false)
		}
		seen[call.ID] = true
		calls = append(calls, call)
	}
	if len(calls) == 0 || len(calls) > MaxTools {
		error := errorReceipt("invalid_tool_round")
		error.Usage = message.Usage
		return h.finish(parent, cp, error, false)
	}
	prepared := make([]toolCheckpoint, 0, len(calls))
	cp.Sequential = cp.Agent.Settings.ToolExecution == "sequential"
	cp.ToolExecution = cp.Agent.Settings.ToolExecution
	if cp.ToolExecution == "" {
		cp.ToolExecution = "parallel"
	}
	for _, call := range calls {
		var offer toolOffer
		found := false
		for _, candidate := range cp.Offered {
			if candidate.Name == call.Name {
				if candidate.ExecutionMode == "sequential" {
					cp.Sequential = true
				}
				offer = candidate
				found = true
				break
			}
		}
		tc := toolCheckpoint{Offer: offer, CallID: call.ID, Arguments: JSON(call.Arguments)}
		// ContentBlock omits empty arguments on JSON round-trip; the validated
		// empty object remains an object, never an absent argument authority.
		if tc.Arguments == nil {
			tc.Arguments = JSON{}
		}
		if !found {
			tc.Offer.Name = call.Name
			tc.ErrorCode = "tool_not_offered"
		} else {
			reg, ok := pins[call.Name]
			if !ok {
				reg, ok = h.options.Registry.current(call.Name)
			}
			if !ok || !sameImplementation(offer, reg.offer) {
				tc.ErrorCode = "tool_unavailable"
			} else {
				args, e := validateArguments(h.life, reg, tc.Arguments, h.session.limits)
				if e != nil {
					tc.ErrorCode = "invalid_tool_arguments"
				} else {
					tc.Arguments = args
				}
			}
		}
		prepared = append(prepared, tc)
	}
	// Preserve the model's original bounded call arguments in the transcript.
	// Final validated/repaired execution arguments belong only to child intent;
	// they are independently detached by validateArguments and never overwrite
	// model provenance or change the call seen by the successor request.
	value, e := dtoObject(message, h.session.limits)
	if e != nil {
		return e
	}
	cp.Phase = "tools"
	cp.Round++
	cp.Children = nil
	parent.Status = "completing"
	_, e = h.session.invocationCommit(context.Background(), parent.ID, func(tx *Tx) error {
		current := tx.state.Tasks[parent.ID]
		var latest generationCheckpoint
		if e := fromObject(current.Checkpoint, &latest, h.session.limits); e != nil {
			return e
		}
		if latest.Abort {
			return reject("generation abort admitted")
		}
		entryID, e := tx.MintID()
		if e != nil {
			return e
		}
		if e = tx.AppendEntry(Entry{ID: entryID, Conversation: parent.Conversation, Kind: "message", Value: value, ByTask: parent.ID}); e != nil {
			return e
		}
		for _, tool := range prepared {
			id, e := tx.MintID()
			if e != nil {
				return e
			}
			checkpoint, e := dtoObject(tool, h.session.limits)
			if e != nil {
				return e
			}
			if e = tx.PutTask(Task{ID: id, Conversation: parent.Conversation, Owner: parent.ID, Kind: "pi.tool", Status: "pending", Checkpoint: checkpoint}); e != nil {
				return e
			}
			cp.Children = append(cp.Children, id)
		}
		checkpoint, e := dtoObject(cp, h.session.limits)
		if e != nil {
			return e
		}
		owned, err := copyTask(current, tx.limits)
		if err != nil {
			return err
		}
		owned.Status = parent.Status
		parent = owned
		parent.Checkpoint = checkpoint
		if e = tx.stage(Write{Op: "put-task", Task: &parent}); e != nil {
			return e
		}
		usage, e := builtin(tx, parent.Conversation, "pi.usage")
		if e != nil {
			return e
		}
		return usage.Update(func(v JSON) error { return addModelUsage(v, message, h.session.limits) })
	})
	return e
}
func (h *Harness) runOwnedTools(runtime *TaskRuntime, parent *Task, cp *generationCheckpoint) error {
	for _, id := range cp.Children {
		s, e := h.session.Snapshot(context.Background())
		if e != nil {
			return e
		}
		current := s.Tasks[parent.ID]
		var latest generationCheckpoint
		if e = fromObject(current.Checkpoint, &latest, h.session.limits); e != nil {
			return e
		}
		if latest.Abort {
			return h.drainAborted(current, latest)
		}
		child, ok := s.Tasks[id]
		if !ok || child.Owner != parent.ID {
			return reject("owned child missing")
		}
		if terminalStatus(child.Status) {
			continue
		}
		if h.life.Err() != nil {
			return context.Canceled
		}
		// Independent shared reservations own execution. This adapter only
		// continues once every ordered child and its real host return drained.
		return reject("owned tool still live")
	}
	if h.life.Err() != nil {
		return context.Canceled
	}
	s, e := h.session.Snapshot(context.Background())
	if e != nil {
		return e
	}
	current := s.Tasks[parent.ID]
	var latest generationCheckpoint
	if e = fromObject(current.Checkpoint, &latest, h.session.limits); e != nil {
		return e
	}
	if latest.Abort {
		return h.drainAborted(current, latest)
	}
	hooks := h.options.Registry.selectedHooks(latest.Agent)
	results := []MessageReceipt{}
	for _, id := range latest.Children {
		var tool toolCheckpoint
		if err := fromObject(s.Tasks[id].Checkpoint, &tool, h.session.limits); err != nil {
			return err
		}
		if tool.Result != nil {
			results = append(results, *tool.Result)
		}
	}
	for _, hook := range hooks {
		if hook.AfterTools == nil {
			continue
		}
		copy, err := detachReceipts(results, h.session.limits)
		if err != nil {
			return err
		}
		if err = callAfterTools(runtime.context, hook.AfterTools, copy, runtime); err != nil {
			if runtime.context.Err() != nil {
				return runtime.context.Err()
			}
			h.scheduler.report(err)
		}
	}
	_, e = h.session.invocationCommit(context.Background(), parent.ID, func(tx *Tx) error {
		// This is a NEW continuation, never the received-result Close exception.
		// Recheck after actual line acquisition, then use the latest full record
		// and ordered round; off-line snapshots cannot authorise this transition.
		if h.closing.Load() || h.scheduler.sealed.Load() {
			return ErrClosed
		}
		if err := runtime.check(); err != nil {
			return err
		}
		if err := runtime.context.Err(); err != nil {
			return err
		}
		current, exists := tx.state.Tasks[parent.ID]
		if !exists || current.Status != "running" || taskHasDecidedOutcome(current) || h.scheduler.invocations[parent.ID] != runtime {
			return ErrSealed
		}
		if taskAborted(current) || taskBelowCancelled(tx.state, current) || taskSelectedFailFastCancellation(tx.state, current.ID) {
			h.scheduler.endOnLine(runtime)
			return ErrSealed
		}
		var latest generationCheckpoint
		if err := fromObject(current.Checkpoint, &latest, tx.limits); err != nil {
			return err
		}
		if latest.Phase != "tools" || latest.Abort {
			return reject("generation continuation phase changed")
		}
		children, err := h.scheduler.toolRoundChildren(tx.state, current, latest)
		if err != nil {
			return err
		}
		for _, child := range children {
			if !terminalStatus(child.Status) || h.scheduler.invocations[child.ID] != nil {
				return reject("ordered tool round still live")
			}
		}
		if len(h.scheduler.ownedLive(tx.state, current.ID)) != 0 {
			return reject("tool drain incomplete")
		}
		if h.hasQueuedReset(tx.state, current.Conversation) {
			return h.resetToolsRun(tx, current, latest)
		}
		if err := h.applySteeringBoundary(tx, current, &latest); err != nil {
			return err
		}
		state := tx.state
		if len(tx.writes) > 0 {
			state, err = tx.current()
			if err != nil {
				return err
			}
		}
		messages, err := contextReceipts(state, current.Conversation, tx.limits)
		if err != nil {
			return err
		}
		latest.Messages, latest.Phase, latest.Children = messages, "prepare-next", nil
		owned, err := copyTask(current, tx.limits)
		if err != nil {
			return err
		}
		owned.Status = "pending"
		owned.Checkpoint, err = dtoObject(latest, tx.limits)
		if err != nil {
			return err
		}
		if err := tx.stage(Write{Op: "put-task", Task: &owned}); err != nil {
			return err
		}
		*parent, *cp = owned, latest
		return nil
	})
	return e
}
func (h *Harness) executeTool(task Task, runtime *TaskRuntime) error {
	var cp toolCheckpoint
	if e := fromObject(task.Checkpoint, &cp, h.session.limits); e != nil {
		return e
	}
	reg, current := h.options.Registry.current(cp.Offer.Name)
	h.mu.Lock()
	pins := h.pins[task.Owner]
	pin, pinned := pins[cp.Offer.Name]
	h.mu.Unlock()
	if pinned && !cp.Started {
		reg = pin
		current = true
	}
	code := cp.ErrorCode
	if cp.Abort {
		code = "aborted"
	} else if code == "" {
		if !current || !sameImplementation(cp.Offer, reg.offer) {
			code = "tool_unavailable"
		} else if cp.Started && (!cp.Offer.ReplaySafe || !reg.offer.ReplaySafe) {
			code = "interrupted"
		}
	}
	if code != "" {
		return h.finishTool(task, cp, ToolResult{}, code)
	}
	if !pinned {
		if !reg.offer.HostValidation && checkSchema(reg.offer.Schema, cp.Arguments, 1) != nil {
			return h.finishTool(task, cp, ToolResult{}, "invalid_stored_arguments")
		}
	}
	var parentCheckpoint generationCheckpoint
	if err := h.session.readTasks(context.Background(), func(state Snapshot) error {
		if err := runtime.check(); err != nil {
			return err
		}
		parent, ok := state.Tasks[task.Owner]
		if !ok {
			return reject("tool parent missing")
		}
		return fromObject(parent.Checkpoint, &parentCheckpoint, h.session.limits)
	}); err != nil {
		return err
	}
	hooks := h.options.Registry.selectedToolHooks(parentCheckpoint.Agent)
	if !cp.Started {
		for _, hook := range hooks {
			if hook.BeforeTool == nil {
				continue
			}
			args, err := copyObject(cp.Arguments, h.session.limits)
			if err != nil {
				return err
			}
			decision, err := callBeforeTool(runtime.context, hook.BeforeTool, goai.ToolCall{Type: "toolCall", ID: cp.CallID, Name: cp.Offer.Name, Arguments: args}, runtime)
			if err != nil {
				if runtime.context.Err() != nil {
					return runtime.context.Err()
				}
				code = "tool_blocked"
				break
			}
			if decision == nil {
				continue
			}
			if decision.Block != "" {
				code = "tool_blocked"
				break
			}
			if decision.Arguments != nil {
				cp.Arguments, err = copyObject(decision.Arguments, h.session.limits)
				if err != nil {
					code = "invalid_tool_arguments"
					break
				}
			}
		}
		if code == "" && !reg.offer.HostValidation {
			if err := checkSchema(reg.offer.Schema, cp.Arguments, 0); err != nil {
				code = "invalid_tool_arguments"
			}
		}
		if code != "" {
			return h.finishTool(task, cp, ToolResult{}, code)
		}
	}
	// Final args are recorded before execution; beforeTool never reruns on recovery.
	replaying := cp.Started
	h.mu.Lock()
	admission, admissionErr := h.session.Snapshot(context.Background())
	if admissionErr != nil {
		h.mu.Unlock()
		return admissionErr
	}
	var admitted toolCheckpoint
	if admissionErr = fromObject(admission.Tasks[task.ID].Checkpoint, &admitted, h.session.limits); admissionErr != nil {
		h.mu.Unlock()
		return admissionErr
	}
	if admitted.Abort {
		h.mu.Unlock()
		return h.finishTool(task, admitted, ToolResult{}, "aborted")
	}
	if h.closing.Load() {
		h.mu.Unlock()
		return context.Canceled
	}
	ctx := runtime.context
	h.mu.Unlock()
	cp.Started = true
	task.Status = "running"
	value, e := dtoObject(cp, h.session.limits)
	if e != nil {
		return e
	}
	task.Checkpoint = value
	_, e = h.session.invocationCommit(context.Background(), task.ID, func(tx *Tx) error {
		current := tx.state.Tasks[task.ID]
		if err := runtime.check(); err != nil {
			return err
		}
		if current.Status != "running" || taskHasDecidedOutcome(current) {
			return ErrSealed
		}
		var latest toolCheckpoint
		if e := fromObject(current.Checkpoint, &latest, h.session.limits); e != nil {
			return e
		}
		if h.closing.Load() || taskAborted(current) || taskBelowCancelled(tx.state, current) || taskSelectedFailFastCancellation(tx.state, current.ID) {
			h.scheduler.endOnLine(runtime)
			return ErrSealed
		}
		owned, err := copyTask(current, tx.limits)
		if err != nil {
			return err
		}
		if replaying {
			latest.Output = ""
			latest.Details = nil
			latest.HasDetails = false
			latest.DroppedBytes, latest.DroppedLines = 0, 0
			latest.OutputRaw = ""
			latest.OutputFull = false
			latest.OutputBytes, latest.OutputNewlines = 0, 0
			latest.OutputTerminated = false
			latest.Diagnostics = nil
		}
		latest.Started = true
		latest.Arguments = cp.Arguments
		owned.Checkpoint, err = dtoObject(latest, tx.limits)
		if err != nil {
			return err
		}
		owned.Status = "running"
		task, cp = owned, latest
		return tx.stage(Write{Op: "put-task", Task: &task})
	})
	if e != nil {
		return e
	}
	if ctx.Err() != nil {
		return nil
	}
	currentState, e := h.session.Snapshot(context.Background())
	if e != nil {
		return e
	}
	var dispatchState toolCheckpoint
	if e = fromObject(currentState.Tasks[task.ID].Checkpoint, &dispatchState, h.session.limits); e != nil {
		return e
	}
	if dispatchState.Abort {
		return h.finishTool(task, dispatchState, ToolResult{}, "aborted")
	}
	if err := h.session.readTasks(context.Background(), func(state Snapshot) error {
		current := state.Tasks[task.ID]
		if h.closing.Load() || taskAborted(current) || taskBelowCancelled(state, current) || taskSelectedFailFastCancellation(state, current.ID) {
			h.scheduler.endOnLine(runtime)
			return ErrSealed
		}
		return runtime.check()
	}); err != nil {
		return err
	}
	api := &ToolAPI{h: h, task: task, checkpoint: cp, active: true, ctx: ctx, runtime: runtime}
	var result ToolResult
	var executionError error
	func() {
		defer func() {
			if recover() != nil {
				executionError = errors.New("tool panic")
			}
		}()
		args, err := copyObject(cp.Arguments, h.session.limits)
		if err != nil {
			executionError = err
			return
		}
		result, executionError = reg.execute(ctx, args, api)
	}()
	api.seal()
	cp = api.checkpoint
	if executionError == nil {
		for _, hook := range hooks {
			if hook.AfterTool == nil {
				continue
			}
			copy, err := detachToolResult(result, h.session.limits)
			if err != nil {
				executionError = err
				break
			}
			callArgs, err := copyObject(cp.Arguments, h.session.limits)
			if err != nil {
				executionError = err
				break
			}
			next, err := callAfterTool(ctx, hook.AfterTool, goai.ToolCall{Type: "toolCall", ID: cp.CallID, Name: cp.Offer.Name, Arguments: callArgs}, copy, runtime)
			if err != nil {
				if ctx.Err() != nil {
					executionError = ctx.Err()
					break
				}
				h.scheduler.report(err)
				continue
			}
			if next != nil {
				result, err = detachToolResult(*next, h.session.limits)
				if err != nil {
					executionError = err
					break
				}
			}
		}
	}
	snapshot, e := h.session.Snapshot(context.Background())
	if e != nil {
		return e
	}
	var latest toolCheckpoint
	if e = fromObject(snapshot.Tasks[task.ID].Checkpoint, &latest, h.session.limits); e != nil {
		return e
	}
	if latest.Abort {
		cp.Abort = true
		return h.finishTool(task, cp, ToolResult{Usage: result.Usage}, "aborted")
	}
	// Close is non-aborting. An unfinished canceled executor leaves its persisted
	// intent; a returned result is settled before storage ownership is released.
	if h.life.Err() != nil && executionError != nil {
		return nil
	}
	if executionError != nil {
		return h.finishTool(task, cp, ToolResult{Usage: result.Usage}, "tool_error")
	}
	return h.finishTool(task, cp, result, "")
}
func (h *Harness) finishTool(task Task, cp toolCheckpoint, result ToolResult, code string) error {
	// Detach and validate known usage before any app callback/outcome staging.
	// Invalid receipts settle once with no usage/app mutation; fallback never
	// recycles the invalid pointer into another failing outcome.
	if result.Usage != nil {
		if !validUsage(result.Usage) {
			code = "invalid_usage"
			result.Usage = nil
			result.Commit = nil
		} else {
			value, e := dtoObject(result.Usage, h.session.limits)
			if e != nil {
				code = "invalid_usage"
				result.Usage = nil
				result.Commit = nil
			} else {
				var owned goai.Usage
				if e = fromObject(value, &owned, h.session.limits); e != nil {
					code = "invalid_usage"
					result.Usage = nil
					result.Commit = nil
				} else {
					result.Usage = &owned
				}
			}
		}
	}
	text := cp.Output
	if cp.Offer.OutputLimits != nil {
		// Bound once after all text blocks are assembled, as pinned
		// boundContent does. An early whole-line cut can otherwise allow a
		// later block to refill bytes beyond the original head cutoff.
		text += result.Content
	} else if len(result.Content) > MaxToolOutputBytes-len(text) {
		code = "output_limit"
	} else {
		text += result.Content
	}
	if code != "" {
		text = cp.Output
		if text != "" {
			text += "\n"
		}
		text += code
		if len(text) > MaxToolOutputBytes {
			text = code
		}
	}
	details := result.Details
	if details == nil && cp.HasDetails && cp.Details != nil {
		switch value := cp.Details.(type) {
		case map[string]any:
			details = JSON(value)
		case JSON:
			details = value
		default:
			code = "invalid_details"
			text = code
			result.Commit = nil
		}
	}
	if details == nil {
		details = JSON{}
	}
	owned, e := copyObject(details, h.session.limits)
	if e != nil {
		code = "invalid_details"
		text = code
		owned = JSON{}
		result.Commit = nil
	}
	if !validUsage(result.Usage) {
		code = "invalid_usage"
		text = code
		result.Usage = nil
		result.Commit = nil
	}
	content := []goai.ContentBlock{{Type: "text", Text: text}}
	if code == "" && len(result.Blocks) > 0 {
		blocks, err := contributionReceipt(goai.Message{Role: goai.RoleToolResult, Content: result.Blocks}, h.session.limits)
		if err != nil {
			code, text = "invalid_content", "invalid_content"
			result.Commit = nil
			content = []goai.ContentBlock{{Type: "text", Text: text}}
		} else {
			if text == "" {
				content = nil
			}
			content = append(content, blocks.Content...)
		}
	}
	if code == "" && cp.Offer.OutputLimits != nil {
		content = boundToolBlocks(content, *cp.Offer.OutputLimits)
	}
	diagnostics := append(append([]ToolDiagnostic{}, cp.Diagnostics...), result.Diagnostics...)
	if err := validateToolDiagnostics(diagnostics, h.session.limits); err != nil {
		code = "invalid_diagnostics"
		result.Commit = nil
		diagnostics = nil
		content = []goai.ContentBlock{{Type: "text", Text: code}}
	}
	if len(diagnostics) > 0 {
		content = append(content, goai.ContentBlock{Type: "text", Text: renderToolDiagnostics(diagnostics)})
	}
	receipt := messageReceipt{Role: goai.RoleToolResult, Content: content, ToolCallID: cp.CallID, ToolName: cp.Offer.Name, IsError: code != "" || result.IsError, ErrorCode: code, Details: owned, Usage: result.Usage}
	value, e := dtoObject(receipt, h.session.limits)
	if e != nil {
		return e
	}
	cp.Result = &receipt
	cp.ReportedError = result.IsError
	cp.Diagnostics = diagnostics
	cp.ErrorCode = code
	task.Status = "done"
	if code == "aborted" {
		task.Status = "aborted"
	}
	checkpoint, e := dtoObject(cp, h.session.limits)
	if e != nil {
		return e
	}
	task.Checkpoint = checkpoint
	_, e = h.session.invocationCommit(context.Background(), task.ID, func(tx *Tx) error {
		existing := tx.state.Tasks[task.ID]
		if terminalStatus(existing.Status) || taskHasDecidedOutcome(existing) {
			return nil
		}
		var latest toolCheckpoint
		if e := fromObject(existing.Checkpoint, &latest, h.session.limits); e != nil {
			return e
		}
		if latest.Abort && code != "aborted" {
			return reject("tool abort precedes outcome")
		}
		if code == "" && result.Commit != nil {
			if e := callToolOutcome(result.Commit, tx); e != nil {
				return reject("application outcome callback failed")
			}
			// Invocation outcome callbacks may mutate application documents only,
			// never task authority/transcript/built-in lifecycle state.
			for _, write := range tx.writes {
				if (write.Op != "put-document" && write.Op != "retire-document") || write.Document == nil || strings.HasPrefix(write.Document.Kind, "pi.") || write.Document.Scope != "conversation" || write.Document.Owner != task.Conversation {
					return reject("tool application mutation outside scope")
				}
			}
		}
		id, e := tx.MintID()
		if e != nil {
			return e
		}
		if e = tx.AppendEntry(Entry{ID: id, Conversation: task.Conversation, Kind: "message", Value: value, ByTask: task.ID}); e != nil {
			return e
		}
		outcome := TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"entryId": id}}}
		if code != "" {
			outcome = TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: code}, Result: &TaskValue{Present: true, Value: JSON{"entryId": id}}}
		}
		if code == "aborted" {
			outcome = TaskOutcome{Status: "aborted", Reason: "aborted", Result: &TaskValue{Present: true, Value: JSON{"entryId": id}}}
		}
		hold := &BuiltinTaskHold{Stage: "final", Action: "tool-receipt", Outcome: outcome, FinalStatus: task.Status, Entry: id, Conversation: task.Conversation, Owner: task.Owner, CallID: cp.CallID}
		metadata := &BuiltinTaskExecution{}
		if existing.Execution != nil {
			*metadata = *existing.Execution.Builtin
		}
		metadata.AbortRequested = cp.Abort || taskAborted(existing)
		metadata.Memos = nil
		metadata.Hold = hold
		task.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: metadata}
		candidate, err := tx.current()
		if err != nil {
			return err
		}
		if len(h.scheduler.ownedLive(candidate, task.ID)) > 0 {
			hold.Stage = "held"
			task.Status = "completing"
		}
		if e = tx.stage(Write{Op: "put-task", Task: &task}); e != nil {
			return e
		}
		if result.Usage != nil {
			usage, e := builtin(tx, task.Conversation, "pi.usage")
			if e != nil {
				return e
			}
			return usage.Update(func(v JSON) error { return addToolUsage(v, cp.Offer.Name, result.Usage, h.session.limits) })
		}
		return nil
	})
	if e != nil && code == "" {
		var rejected *StorageRejected
		if errors.As(e, &rejected) {
			// One bounded deciding/fallback sequence retains a genuine external
			// wake arriving during either preparation/storage attempt.
			h.session.taskBookkeeping(func() {
				if r := h.scheduler.invocations[task.ID]; r != nil {
					epoch := r.admissionEpoch
					r.fallbackEpoch = &epoch
				}
			})
			fallbackErr := h.finishTool(task, cp, ToolResult{Usage: result.Usage}, "tool_outcome_rejected")
			h.session.taskBookkeeping(func() {
				if r := h.scheduler.invocations[task.ID]; r != nil {
					r.fallbackEpoch = nil
				}
			})
			return fallbackErr
		}
	}
	if e == nil {
		h.notify()
	}
	return e
}
func callToolOutcome(callback func(*Tx) error, tx *Tx) (err error) {
	defer func() {
		if recover() != nil {
			err = reject("application outcome callback panic")
		}
	}()
	return callback(tx)
}
func addToolUsage(state JSON, name string, usage *goai.Usage, l Limits) error {
	tools, ok := state["tools"].(map[string]any)
	if !ok {
		return reject("usage document shape")
	}
	// Share arithmetic checks without changing the public tools[name] address.
	models := map[string]any{}
	if prior, ok := tools[name]; ok {
		models["/"+name] = prior
	}
	tmp := JSON{"models": models}
	if e := addModelUsage(tmp, messageReceipt{Model: name, Usage: usage}, l); e != nil {
		return e
	}
	tools[name] = models["/"+name]
	return nil
}

func (c *ConversationHandle) Abort(ctx context.Context) error {
	return c.AbortWithOptions(ctx, ConversationAbortOptions{})
}
func (c *ConversationHandle) AbortWithOptions(ctx context.Context, options ConversationAbortOptions) error {
	return c.abortBound(ctx, options, nil)
}
func (c *ConversationHandle) abortBound(ctx context.Context, options ConversationAbortOptions, binding *TaskRuntime) error {
	if binding != nil {
		if err := binding.check(); err != nil {
			return err
		}
	}
	return c.abortAdmission(ctx, options, binding)
}

func (c *ConversationHandle) abortAdmission(ctx context.Context, options ConversationAbortOptions, binding *TaskRuntime) error {
	h := c.h
	if binding == nil {
		if err := h.Resume(ctx); err != nil {
			return err
		}
	}
	var reached []ID
	var cancels []context.CancelFunc
	_, err := h.session.taskCommit(ctx, func(tx *Tx) error {
		if h.closing.Load() {
			return ErrClosed
		}
		if binding != nil {
			if err := binding.check(); err != nil {
				return err
			}
			own := tx.state.Tasks[binding.taskID]
			if taskInScope(tx.state, own, c.id, options.Background) {
				return &InvocationWaitRequiresYield{Task: binding.taskID}
			}
			// Enabling belongs to the lifetime/self-scope admission; a sealed
			// or self-dependent capability cannot donate an external retry epoch.
			h.scheduler.enable()
		}
		for _, id := range ids(tx.state.Tasks) {
			task := tx.state.Tasks[id]
			if terminalStatus(task.Status) || !options.Background && taskBackground(task) || !taskInScope(tx.state, task, c.id, options.Background) {
				continue
			}
			reached = append(reached, id)
			if r := h.scheduler.invocations[id]; r != nil && !r.abortMode {
				cancels = append(cancels, r.cancel)
			}
			task = markTask(task)
			if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
				return err
			}
		}
		return h.withdrawScopedInputs(tx, c.id, options.Background, nil)
	})
	if err != nil {
		return err
	}
	for _, cancel := range cancels {
		cancel()
	}
	h.scheduler.kick()
	for _, id := range reached {
		var err error
		if binding != nil {
			_, err = binding.WaitForTask(ctx, id)
		} else {
			_, err = h.WaitForTask(ctx, id)
		}
		if err != nil {
			return err
		}
	}
	return h.waitIdleScope(ctx, c.id, binding)
}
func (h *Harness) drainAborted(parent Task, cp generationCheckpoint, observed ...messageReceipt) error {
	s, e := h.session.Snapshot(context.Background())
	if e != nil {
		return e
	}
	// Children (generic or built-in) are never decoded/dispatched here. Shared
	// reservations settle them in cp.Children order and bottom-up ownership.
	if len(observed) == 0 && len(taskOwnedLive(s, parent.ID)) > 0 {
		return reject("abort descendants still live")
	}
	cp.Abort = true
	r := errorReceipt("aborted")
	if len(observed) > 0 {
		r.Usage = observed[0].Usage
	}
	return h.finish(parent, cp, r, false)
}

func exactNumber(v any) (*big.Rat, bool) {
	var spelling string
	switch n := v.(type) {
	case json.Number:
		spelling = string(n)
	case float64:
		spelling = strconv.FormatFloat(n, 'g', -1, 64)
	case int:
		spelling = strconv.Itoa(n)
	default:
		return nil, false
	}
	if len(spelling) > 128 {
		return nil, false
	}
	if i := strings.IndexAny(spelling, "eE"); i >= 0 {
		e, err := strconv.Atoi(spelling[i+1:])
		if err != nil || e > 64 || e < -64 {
			return nil, false
		}
	}
	n, ok := new(big.Rat).SetString(spelling)
	if !ok {
		return nil, false
	}
	limit := new(big.Rat).SetInt(new(big.Int).SetUint64(MaxID))
	if new(big.Rat).Abs(n).Cmp(limit) > 0 {
		return nil, false
	}
	return n, true
}
func equalJSONValue(a, b any) bool {
	if an, ok := exactNumber(a); ok {
		bn, ok := exactNumber(b)
		return ok && an.Cmp(bn) == 0
	}
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			if m, yes := b.(JSON); yes {
				bv = map[string]any(m)
				ok = true
			}
		}
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			other, ok := bv[k]
			if !ok || !equalJSONValue(v, other) {
				return false
			}
		}
		return true
	case JSON:
		return equalJSONValue(map[string]any(av), b)
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i, v := range av {
			if !equalJSONValue(v, bv[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}
