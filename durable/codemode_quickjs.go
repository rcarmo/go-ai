package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"sync"
	"sync/atomic"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

// QuickJSSandbox implements the serial codemode protocol in WASM. Close the
// sandbox after all executions finish. Parallel scripts are not supported yet.
type QuickJSSandbox struct{ engine *quickjs.Engine }

func (*QuickJSSandbox) RuntimeIdentity() string { return quickjs.Identity() }
func NewQuickJSSandbox(ctx context.Context) (*QuickJSSandbox, error) {
	engine, err := quickjs.NewEngine(ctx)
	if err != nil {
		return nil, err
	}
	return &QuickJSSandbox{engine}, nil
}
func (s *QuickJSSandbox) Close(ctx context.Context) error { return s.engine.Close(ctx) }

type quickjsExecution struct {
	ctx        context.Context
	cancel     context.CancelFunc
	events     chan CodemodeEvent
	deliveries chan CodemodeDelivery
	done       chan struct{}
	pending    atomic.Bool
	sequence   atomic.Uint64
	deliveryMu sync.Mutex
	awaiting   *CodemodeCall
}

func (s *QuickJSSandbox) Start(ctx context.Context, code string, start CodemodeStart) (CodemodeExecution, error) {
	if start.RuntimeIdentity != "" && start.RuntimeIdentity != s.RuntimeIdentity() {
		return nil, reject("QuickJS runtime identity mismatch")
	}
	if s == nil || s.engine == nil {
		return nil, reject("nil QuickJS engine")
	}
	linked, cancel := context.WithCancel(ctx)
	execution := &quickjsExecution{ctx: linked, cancel: cancel, events: make(chan CodemodeEvent, 1), deliveries: make(chan CodemodeDelivery), done: make(chan struct{})}
	var clock atomic.Int64
	clock.Store(start.StartedAt)
	opts := quickjs.Options{InterruptBudget: start.InterruptBudget, SerialCalls: true, Clock: clock.Load, StartedAt: start.StartedAt, Seed: start.Seed, MemoryLimitBytes: 32 << 20, Store: map[string]json.RawMessage{}}
	for key, value := range start.Store {
		raw, err := json.Marshal(value)
		if err != nil {
			cancel()
			return nil, err
		}
		opts.Store[key] = raw
	}
	callHost := func(name string, global bool) func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
			if !execution.pending.CompareAndSwap(false, true) {
				return nil, errors.New("parallel codemode calls unsupported")
			}
			defer execution.pending.Store(false)
			var arguments JSON
			if global {
				var values any
				if err := decodeQuickJSValue(args, &values); err != nil {
					return nil, err
				}
				arguments = JSON{"args": values}
			} else {
				var value any
				if err := decodeQuickJSValue(args, &value); err != nil {
					return nil, errors.New("tool arguments must be an object")
				}
				object, ok := value.(map[string]any)
				if !ok || object == nil {
					return nil, errors.New("tool arguments must be an object")
				}
				arguments = JSON(object)
			}
			id := execution.sequence.Add(1)
			call := CodemodeCall{ID: int(id), Global: global, Name: name, Arguments: arguments}
			// Keep admission identity separate from caller-visible event maps.
			raw, err := json.Marshal(arguments)
			if err != nil {
				return nil, err
			}
			var expectedArgs JSON
			if err = decodeQuickJSValue(raw, &expectedArgs); err != nil {
				return nil, err
			}
			expected := call
			expected.Arguments = expectedArgs
			execution.deliveryMu.Lock()
			execution.awaiting = &expected
			execution.deliveryMu.Unlock()
			select {
			case execution.events <- CodemodeEvent{Call: &call}:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			var delivery CodemodeDelivery
			select {
			case delivery = <-execution.deliveries:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			if delivery.Call.ID != int(id) {
				return nil, errors.New("QuickJS delivery identity mismatch")
			}
			// Replayed deliveries cannot rewind the deterministic clock.
			if delivery.Now > clock.Load() {
				clock.Store(delivery.Now)
			}
			if delivery.Error != "" {
				return nil, errors.New(delivery.Error)
			}
			return json.Marshal(delivery.Value)
		}
	}
	for _, tool := range start.Tools {
		opts.Tools = append(opts.Tools, quickjs.Tool{Name: tool.Name, JSName: tool.Identifier, Description: tool.Description, Execute: callHost(tool.Name, false)})
	}
	globals := []string{"searchTools", "describeTool", "describeNamespace"}
	if start.PaidModels {
		globals = append(globals, "models.classify", "models.generateImages", "models.getModelsOfType", "models.getAvailableOfType", "models.getModelOfType")
	}
	for _, name := range globals {
		opts.Globals = append(opts.Globals, quickjs.Global{Name: name, Spread: true, Execute: callHost(name, true)})
	}
	for _, global := range start.Globals {
		opts.Globals = append(opts.Globals, quickjs.Global{Name: global.Name, Spread: global.Spread, Execute: callHost(global.Name, true)})
	}
	go func() {
		defer close(execution.done)
		result := s.engine.Execute(linked, code, opts)
		out := CodemodeResult{StoreWrites: JSON{}}
		for _, item := range result.Output {
			if item.Type == "image" {
				out.Images = append(out.Images, goai.ImageContent{Type: "image", Data: item.Data, MimeType: item.MimeType})
			}
		}
		var formatErr error
		out.Output, formatErr = formatQuickJSText(result)
		if formatErr != nil {
			out.Error = formatErr.Error()
		}
		if result.Error != nil {
			out.Error = result.Error.Error()
		}
		out.StoreDeletes = append([]string(nil), result.StoreWrites.Delete...)
		if result.Value != nil {
			out.HasValue = true
			if err := decodeQuickJSValue(result.Value, &out.Value); err != nil {
				out.Error = "invalid QuickJS return value"
			}
		}
		for key, raw := range result.StoreWrites.Set {
			var value any
			if err := decodeQuickJSValue(raw, &value); err != nil {
				out.Error = "invalid QuickJS store value"
				break
			}
			out.StoreWrites[key] = value
		}
		if out.Error != "" {
			out.Value = nil
			out.HasValue = false
		}
		select {
		case execution.events <- CodemodeEvent{Result: &out}:
		case <-linked.Done():
		}
	}()
	return execution, nil
}
func (e *quickjsExecution) Next(ctx context.Context) (CodemodeEvent, error) {
	select {
	case event := <-e.events:
		return event, nil
	case <-ctx.Done():
		return CodemodeEvent{}, ctx.Err()
	case <-e.ctx.Done():
		return CodemodeEvent{}, e.ctx.Err()
	}
}
func (e *quickjsExecution) Deliver(ctx context.Context, delivery CodemodeDelivery) error {
	if ctx == nil {
		return reject("nil delivery context")
	}
	// Detach JSON before handoff: the callback marshals the value after this
	// method returns, and callers retain ownership of their original maps.
	raw, err := json.Marshal(delivery)
	if err != nil || len(raw) > MaxCodemodeFullOutputBytes {
		return reject("invalid or oversized delivery JSON")
	}
	var owned CodemodeDelivery
	if err = decodeQuickJSValue(raw, &owned); err != nil {
		return reject("invalid delivery JSON")
	}
	delivery = owned
	e.deliveryMu.Lock()
	defer e.deliveryMu.Unlock()
	select {
	case <-e.done:
		return nil
	default:
	}
	call := e.awaiting
	if call == nil || delivery.Seq != call.ID || delivery.Call.ID != call.ID || delivery.Call.Name != call.Name || delivery.Call.Global != call.Global || !equalJSONValue(delivery.Call.Arguments, call.Arguments) || delivery.Now < 0 || !delivery.HasValue && delivery.Value != nil || delivery.Error != "" && (delivery.HasValue || delivery.Value != nil) {
		return reject("invalid QuickJS delivery")
	}
	select {
	case e.deliveries <- delivery:
		e.awaiting = nil
		return nil
	case <-e.done:
		// A fatal VM boundary may end before an admitted host call settles.
		// The caller still journals/joins that call; no live promise remains.
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-e.ctx.Done():
		return e.ctx.Err()
	}
}
func (e *quickjsExecution) Close() error { e.cancel(); <-e.done; return nil }

func decodeQuickJSValue(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(value)
}
