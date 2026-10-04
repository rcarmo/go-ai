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
	"sort"
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
	// Validator is required for schemas outside the enforced native subset. It
	// returns final rewritten arguments before intent and is NOT rerun on recovery.
	Validator func(context.Context, JSON) (JSON, error)
	Execute   func(context.Context, JSON, *ToolAPI) (ToolResult, error)
}
type toolOffer struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Schema         JSON   `json:"schema"`
	Implementation string `json:"implementation"`
	Version        uint64 `json:"version"`
	ReplaySafe     bool   `json:"replaySafe"`
	HostValidation bool   `json:"hostValidation,omitempty"`
}
type registeredTool struct {
	offer     toolOffer
	execute   func(context.Context, JSON, *ToolAPI) (ToolResult, error)
	validator func(context.Context, JSON) (JSON, error)
}
type Registry struct {
	mu    sync.RWMutex
	tools map[string]registeredTool
}

func NewRegistry() *Registry { return &Registry{tools: map[string]registeredTool{}} }
func (r *Registry) Register(reg ToolRegistration) error {
	if r == nil {
		return reject("nil registry")
	}
	l := DefaultLimits()
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
	offer := toolOffer{reg.Definition.Name, reg.Definition.Description, schema, reg.Implementation, reg.Version, reg.ReplaySafe, subsetErr != nil}
	if _, e = dtoObject(offer, l); e != nil {
		return e
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tools == nil {
		r.tools = map[string]registeredTool{}
	}
	if _, exists := r.tools[offer.Name]; !exists && len(r.tools) >= MaxTools {
		return reject("tool registry limit")
	}
	r.tools[offer.Name] = registeredTool{offer, reg.Execute, reg.Validator}
	return nil
}
func (r *Registry) Remove(name string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.tools, name)
	r.mu.Unlock()
}
func (r *Registry) snapshot(l Limits) ([]toolOffer, map[string]registeredTool, error) {
	offers := []toolOffer{}
	pins := map[string]registeredTool{}
	if r == nil {
		return offers, pins, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		v := r.tools[name]
		schema, e := copyObject(v.offer.Schema, l)
		if e != nil {
			return nil, nil, e
		}
		v.offer.Schema = schema
		offers = append(offers, v.offer)
		pins[name] = v
	}
	return offers, pins, nil
}
func (r *Registry) current(name string) (registeredTool, bool) {
	if r == nil {
		return registeredTool{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.tools[name]
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
	Details JSON
	Usage   *goai.Usage
	Commit  func(*Tx) error
}
type toolCheckpoint struct {
	Offer     toolOffer       `json:"offer"`
	CallID    string          `json:"callId"`
	Arguments JSON            `json:"arguments"`
	Started   bool            `json:"started"`
	Abort     bool            `json:"abort,omitempty"`
	ErrorCode string          `json:"errorCode,omitempty"`
	Output    string          `json:"output"`
	Result    *MessageReceipt `json:"result,omitempty"`
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
}

func (a *ToolAPI) Output(text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.active {
		return ErrSealed
	}
	if e := a.ctx.Err(); e != nil {
		return e
	}
	if len(text) > MaxToolOutputBytes-len(a.checkpoint.Output) {
		return reject("tool output limit")
	}
	candidate := a.checkpoint
	candidate.Output += text
	p, e := dtoObject(candidate, a.h.session.limits)
	if e != nil {
		return e
	}
	task := a.task
	task.Checkpoint = p
	_, e = a.h.session.Commit(context.Background(), func(tx *Tx) error {
		current := tx.state.Tasks[task.ID]
		if terminalStatus(current.Status) {
			return ErrSealed
		}
		var cp toolCheckpoint
		if e := fromObject(current.Checkpoint, &cp, a.h.session.limits); e != nil {
			return e
		}
		if cp.Abort {
			return reject("tool aborted")
		}
		return tx.PutTask(task)
	})
	if e == nil {
		a.checkpoint = candidate
		a.task = task
	}
	return e
}
func (a *ToolAPI) seal()      { a.mu.Lock(); a.active = false; a.mu.Unlock() }
func (a *ToolAPI) TaskID() ID { a.mu.Lock(); defer a.mu.Unlock(); return a.task.ID }

func sameImplementation(a, b toolOffer) bool {
	return a.Name == b.Name && a.Implementation == b.Implementation && a.Version == b.Version && reflect.DeepEqual(a.Schema, b.Schema) && a.HostValidation == b.HostValidation
}
func validateArguments(ctx context.Context, reg registeredTool, args JSON, l Limits) (JSON, error) {
	owned, e := copyObject(args, l)
	if e != nil {
		return nil, e
	}
	if reg.validator != nil {
		owned, e = reg.validator(ctx, owned)
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
	for _, call := range calls {
		var offer toolOffer
		found := false
		for _, candidate := range cp.Offered {
			if candidate.Name == call.Name {
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
	_, e = h.session.Commit(context.Background(), func(tx *Tx) error {
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
		if e = tx.AppendEntry(Entry{ID: entryID, Conversation: parent.Conversation, Kind: "message", Value: value}); e != nil {
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
		parent.Checkpoint = checkpoint
		if e = tx.PutTask(parent); e != nil {
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
func (h *Harness) runOwnedTools(parent *Task, cp *generationCheckpoint) error {
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
		if e = h.executeTool(child); e != nil {
			return e
		}
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
	messages, e := contextReceipts(s, parent.Conversation, h.session.limits)
	if e != nil {
		return e
	}
	cp.Messages = messages
	cp.Phase = "intent"
	cp.Children = nil
	parent.Status = "pending"
	checkpoint, e := dtoObject(cp, h.session.limits)
	if e != nil {
		return e
	}
	parent.Checkpoint = checkpoint
	_, e = h.session.Commit(context.Background(), func(tx *Tx) error {
		var latest generationCheckpoint
		if e := fromObject(tx.state.Tasks[parent.ID].Checkpoint, &latest, h.session.limits); e != nil {
			return e
		}
		if latest.Abort {
			return reject("parent aborted")
		}
		for _, child := range tx.state.Tasks {
			if child.Owner == parent.ID && !terminalStatus(child.Status) {
				return reject("tool drain incomplete")
			}
		}
		return tx.PutTask(*parent)
	})
	return e
}
func (h *Harness) executeTool(task Task) error {
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
	// Stored args are already validated. No validator/hook runs on recovery.
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
	ctx, cancel := context.WithCancel(h.life)
	h.invocations[task.ID] = cancel
	h.mu.Unlock()
	defer func() { cancel(); h.mu.Lock(); delete(h.invocations, task.ID); h.mu.Unlock() }()
	cp.Started = true
	task.Status = "running"
	value, e := dtoObject(cp, h.session.limits)
	if e != nil {
		return e
	}
	task.Checkpoint = value
	_, e = h.session.Commit(context.Background(), func(tx *Tx) error {
		var latest toolCheckpoint
		if e := fromObject(tx.state.Tasks[task.ID].Checkpoint, &latest, h.session.limits); e != nil {
			return e
		}
		if latest.Abort {
			return reject("tool aborted before dispatch")
		}
		return tx.PutTask(task)
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
	api := &ToolAPI{h: h, task: task, checkpoint: cp, active: true, ctx: ctx}
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
	if len(result.Content) > MaxToolOutputBytes-len(text) {
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
	receipt := messageReceipt{Role: goai.RoleToolResult, Content: []goai.ContentBlock{{Type: "text", Text: text}}, ToolCallID: cp.CallID, ToolName: cp.Offer.Name, IsError: code != "", ErrorCode: code, Details: owned, Usage: result.Usage}
	value, e := dtoObject(receipt, h.session.limits)
	if e != nil {
		return e
	}
	cp.Result = &receipt
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
	_, e = h.session.Commit(context.Background(), func(tx *Tx) error {
		existing := tx.state.Tasks[task.ID]
		if terminalStatus(existing.Status) {
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
			if e := result.Commit(tx); e != nil {
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
		if e = tx.AppendEntry(Entry{ID: id, Conversation: task.Conversation, Kind: "message", Value: value}); e != nil {
			return e
		}
		if e = tx.PutTask(task); e != nil {
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
			return h.finishTool(task, cp, ToolResult{Usage: result.Usage}, "tool_outcome_rejected")
		}
	}
	if e == nil {
		h.notify()
	}
	return e
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
	h := c.h
	h.mu.Lock()
	if h.closing.Load() {
		h.mu.Unlock()
		return ErrClosed
	}
	var affected []ID
	_, e := h.session.Commit(ctx, func(tx *Tx) error {
		for _, task := range tx.state.Tasks {
			if task.Conversation != c.id || terminalStatus(task.Status) {
				continue
			}
			switch task.Kind {
			case "pi.generation":
				var cp generationCheckpoint
				if e := fromObject(task.Checkpoint, &cp, h.session.limits); e != nil {
					return e
				}
				cp.Abort = true
				task.Status = "completing"
				value, e := dtoObject(cp, h.session.limits)
				if e != nil {
					return e
				}
				task.Checkpoint = value
			case "pi.tool":
				var cp toolCheckpoint
				if e := fromObject(task.Checkpoint, &cp, h.session.limits); e != nil {
					return e
				}
				cp.Abort = true
				value, e := dtoObject(cp, h.session.limits)
				if e != nil {
					return e
				}
				task.Checkpoint = value
			default:
				continue
			}
			if e := tx.PutTask(task); e != nil {
				return e
			}
			affected = append(affected, task.ID)
		}
		return nil
	})
	if e == nil {
		for _, id := range affected {
			if cancel := h.invocations[id]; cancel != nil {
				cancel()
			}
		}
		h.scheduleLocked(c.id)
	}
	h.mu.Unlock()
	if e != nil {
		return e
	}
	for {
		signal := h.signal()
		s, e := h.Snapshot(ctx)
		if e != nil {
			return e
		}
		pending := false
		for _, id := range affected {
			if !terminalStatus(s.Tasks[id].Status) {
				pending = true
			}
		}
		if !pending {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-signal:
		case <-h.life.Done():
			return ErrClosed
		}
	}
}
func (h *Harness) drainAborted(parent Task, cp generationCheckpoint, observed ...messageReceipt) error {
	s, e := h.session.Snapshot(context.Background())
	if e != nil {
		return e
	}
	children := []Task{}
	for _, task := range s.Tasks {
		if task.Owner == parent.ID && !terminalStatus(task.Status) {
			children = append(children, task)
		}
	}
	sort.Slice(children, func(i, j int) bool { return children[i].ID < children[j].ID })
	for _, task := range children {
		var tool toolCheckpoint
		if e = fromObject(task.Checkpoint, &tool, h.session.limits); e != nil {
			return e
		}
		tool.Abort = true
		if e = h.finishTool(task, tool, ToolResult{}, "aborted"); e != nil {
			return e
		}
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
