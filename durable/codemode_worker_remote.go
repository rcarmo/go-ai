package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/quickjs"
)

// CodemodeWorkerRequest is pi-codemode 1.2.0's portable remote request.
// Tools and globals contain declarations only; implementations stay on the host.
type CodemodeWorkerRequest struct {
	Data            CodemodeWorkerStart `json:"data"`
	InterruptBudget int                 `json:"interruptBudget,omitempty"`
}
type CodemodeWorkerStart struct {
	Code             string                     `json:"code"`
	Tools            []CodemodeWorkerTool       `json:"tools"`
	Globals          []CodemodeWorkerGlobal     `json:"globals"`
	MemoryLimitBytes int                        `json:"memoryLimitBytes,omitempty"`
	Store            map[string]string          `json:"store"`
	Deterministic    *CodemodeWorkerDeterminism `json:"deterministic,omitempty"`
}
type CodemodeWorkerTool struct {
	Name        string `json:"name"`
	JSName      string `json:"jsName"`
	Description string `json:"description"`
}
type CodemodeWorkerGlobal struct {
	Name   string `json:"name"`
	Spread bool   `json:"spread"`
}
type CodemodeWorkerDeterminism struct {
	StartedAt int64  `json:"startedAt"`
	Seed      uint32 `json:"seed"`
}
type CodemodeWorkerOutput struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Console  bool   `json:"console,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}
type CodemodeWorkerDuring struct {
	Seq    int `json:"seq"`
	Issued int `json:"issued"`
}

// RawMessage retains the wire distinction between JSON text (script errors) and
// structured remote failure objects. Args/value/payload are JSON-encoded strings.
type CodemodeWorkerMessage struct {
	Type        string                `json:"type"`
	ID          int                   `json:"id,omitempty"`
	Target      string                `json:"target,omitempty"`
	Name        string                `json:"name,omitempty"`
	Args        *string               `json:"args,omitempty"`
	Item        *CodemodeWorkerOutput `json:"item,omitempty"`
	OK          bool                  `json:"ok"`
	Value       *string               `json:"value,omitempty"`
	Writes      *string               `json:"writes,omitempty"`
	Error       json.RawMessage       `json:"error,omitempty"`
	During      *CodemodeWorkerDuring `json:"during,omitempty"`
	Message     string                `json:"message,omitempty"`
	Fingerprint string                `json:"fingerprint,omitempty"`
	Seq         int                   `json:"seq,omitempty"`
	Issued      int                   `json:"issued,omitempty"`
}
type CodemodeWorkerReply struct {
	Type       string  `json:"type"`
	ID         int     `json:"id,omitempty"`
	OK         bool    `json:"ok"`
	Payload    *string `json:"payload,omitempty"`
	Seq        int     `json:"seq,omitempty"`
	Now        int64   `json:"now,omitempty"`
	Properties *string `json:"properties,omitempty"`
}

// CodemodeWorkerTransport implements the official execute(request, exchange)
// boundary. Exchange calls may overlap, but must enter in worker message order.
// Execute must join all callbacks before returning and honour ctx cancellation.
type CodemodeWorkerTransport interface {
	Execute(context.Context, CodemodeWorkerRequest, func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error
}

// CodemodeWorkerRemote adapts official worker-message batches to the durable
// native drain protocol. Fingerprint must match the deployed deterministic VM;
// it is deliberately not inferred from the first untrusted message.
type CodemodeWorkerRemote struct {
	Transport   CodemodeWorkerTransport
	Fingerprint string
}

// SupportsWorkerArgumentPresence advertises the decoder for new runs only.
// Execute honours the recorded start flag even when called during replay.
func (*CodemodeWorkerRemote) SupportsWorkerArgumentPresence() bool { return true }

// SupportsWorkerDiscovery advertises catalogue-only globals for new starts.
func (*CodemodeWorkerRemote) SupportsWorkerDiscovery() bool { return true }

func (r *CodemodeWorkerRemote) Execute(ctx context.Context, code string, start CodemodeStart, exchange func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error), receive func(context.Context) (CodemodeDelivery, error)) error {
	if ctx == nil || r == nil || r.Transport == nil || r.Fingerprint == "" || exchange == nil || receive == nil {
		return reject("invalid worker remote adapter")
	}
	request := CodemodeWorkerRequest{Data: CodemodeWorkerStart{Code: code, MemoryLimitBytes: start.MemoryLimitBytes, Tools: []CodemodeWorkerTool{}, Globals: []CodemodeWorkerGlobal{}, Store: map[string]string{}, Deterministic: &CodemodeWorkerDeterminism{StartedAt: start.StartedAt, Seed: uint32(start.Seed)}}, InterruptBudget: start.InterruptBudget}
	for _, tool := range start.Tools {
		request.Data.Tools = append(request.Data.Tools, CodemodeWorkerTool{tool.Name, tool.Identifier, tool.Description})
	}
	if start.WorkerDiscovery {
		for _, name := range codemodeDiscoveryNames {
			request.Data.Globals = append(request.Data.Globals, CodemodeWorkerGlobal{Name: name, Spread: true})
		}
	}
	for _, global := range start.Globals {
		request.Data.Globals = append(request.Data.Globals, CodemodeWorkerGlobal{global.Name, global.Spread})
	}
	limits := DefaultLimits()
	for key, value := range start.Store {
		b, err := encodeBounded(value, limits, limits.MaxDocumentBytes)
		if err != nil {
			return err
		}
		request.Data.Store[key] = string(b)
	}
	linked, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	waiting := map[int]chan CodemodeDelivery{}
	deliveredCalls := map[int]int{0: 0}
	// Keep tool rows in issue order for a script failure. A global isn't a
	// tool call, and a pending tool has no successfully delivered outcome.
	var calls []quickjs.Call
	callRows := map[int]int{}
	started, terminal := false, false
	terminalDone := make(chan struct{})
	// Let RPCs for abandoned calls return after the guest ends. The
	// transport can join them without waiting for the host to receive the
	// terminal event, which is deliberately held until the transport returns.
	finish := func(ctx context.Context, result *CodemodeResult, scriptEnd *CodemodeWorkerDuring, workerStopped bool) error {
		if _, err := exchange(ctx, CodemodeEvent{Result: result, scriptEnd: scriptEnd, workerStopped: workerStopped}); err != nil {
			return err
		}
		terminal = true
		close(terminalDone)
		return nil
	}
	issued, lastDrain, lastIssued := 0, -1, 0
	outputBytes := 0
	outputItems := []quickjs.OutputItem{}
	images := []goai.ImageContent{}
	var protocolErr error
	fail := func(err error) error {
		if protocolErr == nil {
			protocolErr = err
		}
		cancel()
		return err
	}
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		for {
			delivery, err := receive(linked)
			mu.Lock()
			if err != nil {
				if linked.Err() == nil {
					fail(err)
				}
				mu.Unlock()
				return
			}
			ch, ok := waiting[delivery.Call.ID]
			if !ok {
				fail(reject("worker delivery has no pending call"))
				mu.Unlock()
				return
			}
			delete(waiting, delivery.Call.ID)
			deliveredCalls[delivery.Seq] = delivery.Call.ID
			if row, ok := callRows[delivery.Call.ID]; ok {
				calls[row].Status = "ok"
				if delivery.Error != "" {
					calls[row].Status = "error"
				}
			}
			ch <- delivery
			mu.Unlock()
		}
	}()
	defer func() { cancel(); <-joined }()
	drain := func(ctx context.Context, seq, count int, final bool) error {
		if seq == lastDrain {
			if count != lastIssued {
				return reject("worker drain changed issued count")
			}
			return nil
		}
		if seq != lastDrain+1 || count != issued {
			return reject("invalid worker drain sequence")
		}
		callID, ok := deliveredCalls[seq]
		if !ok {
			return reject("worker drain before delivery")
		}
		d := CodemodeDrain{DeliveryID: callID, Issued: count, Final: final}
		reply, err := exchange(ctx, CodemodeEvent{Drain: &d})
		if err != nil {
			return err
		}
		if reply.Stop {
			return context.Canceled
		}
		if reply.Acknowledged == nil || *reply.Acknowledged != d {
			return reject("worker drain not acknowledged")
		}
		lastDrain, lastIssued = seq, count
		return nil
	}
	began := time.Now()
	err := r.Transport.Execute(linked, request, func(callCtx context.Context, batch []CodemodeWorkerMessage) (*CodemodeWorkerReply, error) {
		mu.Lock()
		if protocolErr != nil {
			err := protocolErr
			mu.Unlock()
			return nil, err
		}
		if terminal || linked.Err() != nil {
			mu.Unlock()
			return &CodemodeWorkerReply{Type: "stop"}, nil
		}
		if len(batch) == 0 || len(batch) > 1001 {
			err := fail(reject("invalid worker batch size"))
			mu.Unlock()
			return nil, err
		}
		var pending chan CodemodeDelivery
		var callID int
		for index, message := range batch {
			if message.Type != "output" && message.Type != "started" && index != len(batch)-1 {
				err := fail(reject("worker control message must end batch"))
				mu.Unlock()
				return nil, err
			}
			var err error
			switch message.Type {
			case "started":
				if started || message.Fingerprint != r.Fingerprint {
					err = reject("worker fingerprint mismatch")
				} else {
					started = true
				}
			case "output":
				if !started || message.Item == nil {
					err = reject("invalid worker output")
					break
				}
				item := message.Item
				switch item.Type {
				case "text":
					if len(outputItems) >= 100000 || outputBytes+len(item.Text) > MaxCodemodeFullOutputBytes {
						err = reject("worker output exceeds limit")
					} else {
						outputBytes += len(item.Text)
						kind := "text"
						if item.Console {
							kind = "console"
						}
						outputItems = append(outputItems, quickjs.OutputItem{Type: kind, Text: item.Text})
					}
				case "image":
					image := goai.ImageContent{Type: "image", Data: item.Data, MimeType: item.MimeType}
					err = validateCodemodeImages([]goai.ImageContent{image}, limits)
					if err == nil {
						if len(images) >= limits.MaxPage {
							err = reject("worker image count exceeds limit")
						} else {
							images = append(images, image)
						}
					}
				default:
					err = reject("unknown worker output type")
				}
			case "call":
				if !started || message.ID != issued+1 || (message.Target != "tool" && message.Target != "global") {
					err = reject("invalid worker call")
					break
				}
				call := CodemodeCall{ID: message.ID, Name: message.Name, Global: message.Target == "global"}
				if call.Global {
					call.Arguments, call.UndefinedArgument, err = decodeWorkerGlobalArguments(start, message, limits)
				} else if message.Args != nil {
					err = decodeWorkerJSON([]byte(*message.Args), limits, limits.MaxRecordBytes, &call.Arguments)
				}
				if err != nil {
					break
				}
				if call.Arguments == nil {
					call.Arguments = JSON{}
				}
				pending = make(chan CodemodeDelivery, 1)
				waiting[call.ID] = pending
				if !call.Global {
					callRows[call.ID] = len(calls)
					calls = append(calls, quickjs.Call{Name: call.Name, Status: "cancelled"})
				}
				callID = call.ID
				issued++
				var reply CodemodeRemoteReply
				reply, err = exchange(callCtx, CodemodeEvent{Call: &call})
				if err == nil && reply.Stop {
					err = context.Canceled
				}
			case "idle":
				if !started {
					err = reject("worker idle before started")
				} else {
					err = drain(callCtx, message.Seq, message.Issued, false)
				}
			case "done":
				if !started {
					err = reject("worker done lacks deterministic position")
					break
				}
				var scriptError *quickjs.Error
				if !message.OK {
					scriptError, err = decodeWorkerScriptError(message.Error, limits)
					if err != nil {
						break
					}
				}
				if message.During == nil {
					// The published VM reports a compile failure after the
					// fingerprint but before it has a drain position. Accept
					// only this effect-free error, never a successful result.
					if scriptError == nil || scriptError.Name != "SyntaxError" || scriptError.Stack == "" ||
						issued != 0 || lastDrain != -1 || len(outputItems) != 0 || len(images) != 0 || message.Value != nil || message.Writes != nil {
						err = reject("worker done lacks deterministic position")
						break
					}
				} else if message.During.Issued != issued || message.During.Seq < lastDrain || message.During.Seq > lastDrain+1 {
					err = reject("worker done lacks deterministic position")
					break
				}
				result := CodemodeResult{Images: images, WallMs: time.Since(began).Milliseconds()}
				if message.OK {
					err = drain(callCtx, message.During.Seq, message.During.Issued, true)
					if err != nil {
						break
					}
					if message.Value != nil {
						result.HasValue = true
						err = decodeWorkerReturnValue([]byte(*message.Value), limits, &result.Value)
						if err != nil {
							break
						}
					}
					if message.Writes == nil {
						err = reject("worker done lacks writes")
						break
					}
					result.StoreWrites, result.StoreDeletes, err = decodeCodemodeWorkerStorePatch(*message.Writes, limits)
				} else {
					result.Error = scriptError.Message
				}
				if err == nil {
					formatted := quickjs.Result{OK: message.OK, Output: outputItems}
					if message.OK && message.Value != nil {
						formatted.Value = json.RawMessage(*message.Value)
					}
					if scriptError != nil {
						formatted.Error = scriptError
						formatted.Calls = calls
					}
					result.Output, err = formatQuickJSText(formatted)
				}
				if err == nil {
					var end *CodemodeWorkerDuring
					if scriptError != nil && start.JournalVersion == codemodeGroupedJournalVersion {
						// A no-position done frame reached here only for the
						// verified effect-free syntax-error case above.
						end = new(CodemodeWorkerDuring)
						if message.During != nil {
							*end = *message.During
						}
					}
					err = finish(callCtx, &result, end, false)
				}
			case "crash", "failure":
				result := CodemodeResult{Images: images, Error: message.Message, WallMs: time.Since(began).Milliseconds()}
				result.Output, err = formatQuickJSText(quickjs.Result{OK: true, Output: outputItems})
				if err != nil {
					break
				}
				if message.Type == "failure" {
					var failure struct {
						Message string `json:"message"`
					}
					err = json.Unmarshal(message.Error, &failure)
					result.Error = failure.Message
				}
				if err == nil {
					if result.Error == "" {
						result.Error = "Remote sandbox failed"
					}
					err = finish(callCtx, &result, nil, start.JournalVersion == codemodeGroupedJournalVersion)
				}
			default:
				err = reject("unknown worker message type")
			}
			if err != nil {
				fail(err)
				mu.Unlock()
				return nil, err
			}
		}
		mu.Unlock()
		if pending == nil {
			return nil, nil
		}
		select {
		case delivery := <-pending:
			select {
			case <-terminalDone:
				return &CodemodeWorkerReply{Type: "stop"}, nil
			default:
			}
			reply := &CodemodeWorkerReply{Type: "result", ID: callID, OK: delivery.Error == "", Seq: delivery.Seq, Now: delivery.Now}
			if delivery.Error != "" {
				reply.Payload = &delivery.Error
				if delivery.Properties != nil {
					raw, err := encodeBounded(delivery.Properties, limits, limits.MaxRecordBytes)
					if err != nil {
						return nil, err
					}
					text := string(raw)
					reply.Properties = &text
				}
			} else if delivery.HasValue {
				b, err := encodeBounded(delivery.Value, limits, limits.MaxRecordBytes)
				if err != nil {
					return nil, err
				}
				value := string(b)
				reply.Payload = &value
			}
			return reply, nil
		case <-terminalDone:
			return &CodemodeWorkerReply{Type: "stop"}, nil
		case <-linked.Done():
			return &CodemodeWorkerReply{Type: "stop"}, nil
		}
	})
	mu.Lock()
	defer mu.Unlock()
	if protocolErr != nil {
		return protocolErr
	}
	if err != nil {
		return err
	}
	if !terminal {
		return reject("worker remote returned without terminal message")
	}
	return nil
}

// Reuse the bounded scanner and strict decoder without imposing its journal
// object-root contract on worker JSON values (which may be arrays or scalars).
func decodeWorkerGlobalArguments(start CodemodeStart, message CodemodeWorkerMessage, limits Limits) (JSON, bool, error) {
	global := codemodeCustomGlobal(start, message.Name)
	if global == nil || global.Spread {
		// A spread guest always serializes its argument list, even when it is
		// empty. null inside that list is valid, but bare null is not a list.
		var values []any
		if message.Args != nil {
			if err := decodeWorkerJSON([]byte(*message.Args), limits, limits.MaxRecordBytes, &values); err != nil {
				return nil, false, err
			}
		}
		if values == nil {
			return nil, false, reject("spread worker global arguments must be an array")
		}
		return JSON{"args": values}, false, nil
	}
	var value any
	if message.Args == nil {
		if start.WorkerArgumentPresence {
			return JSON{}, true, nil
		}
	} else if err := decodeWorkerJSON([]byte(*message.Args), limits, limits.MaxRecordBytes, &value); err != nil {
		return nil, false, err
	}
	if value == nil && !start.WorkerArgumentPresence {
		// Keep the old omitted/null projection verbatim for pre-flag journals.
		value = []any{}
	}
	return JSON{"args": value}, false, nil
}

// A returned string can be spilled and stored in chunks, like the inline
// sandbox's full output. Only its wire decoding gets the larger text limit;
// other JSON values, calls and store writes still use record limits.
func decodeWorkerReturnValue(data []byte, limits Limits, target *any) error {
	maximum := limits.MaxRecordBytes
	if value := bytes.TrimSpace(data); len(value) > 0 && value[0] == '"' {
		limits.MaxStringBytes = MaxCodemodeFullOutputBytes
		// Each byte can take six bytes in a JSON escape, plus the quotes.
		maximum = 6*MaxCodemodeFullOutputBytes + 2
	}
	return decodeWorkerJSON(data, limits, maximum, target)
}

func decodeWorkerJSON(data []byte, limits Limits, maximum int, target any) error {
	if len(data) > maximum {
		return reject("worker JSON exceeds limit")
	}
	envelope := make([]byte, 0, len(data)+10)
	envelope = append(envelope, `{"value":`...)
	envelope = append(envelope, data...)
	envelope = append(envelope, '}')
	var wrapper struct {
		Value json.RawMessage `json:"value"`
	}
	if err := decodeStrict(envelope, limits, maximum+10, &wrapper); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(wrapper.Value))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
