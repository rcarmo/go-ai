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

func (r *CodemodeWorkerRemote) Execute(ctx context.Context, code string, start CodemodeStart, exchange func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error), receive func(context.Context) (CodemodeDelivery, error)) error {
	if ctx == nil || r == nil || r.Transport == nil || r.Fingerprint == "" || exchange == nil || receive == nil {
		return reject("invalid worker remote adapter")
	}
	request := CodemodeWorkerRequest{Data: CodemodeWorkerStart{Code: code, MemoryLimitBytes: start.MemoryLimitBytes, Tools: []CodemodeWorkerTool{}, Globals: []CodemodeWorkerGlobal{}, Store: map[string]string{}, Deterministic: &CodemodeWorkerDeterminism{StartedAt: start.StartedAt, Seed: uint32(start.Seed)}}, InterruptBudget: start.InterruptBudget}
	for _, tool := range start.Tools {
		request.Data.Tools = append(request.Data.Tools, CodemodeWorkerTool{tool.Name, tool.Identifier, tool.Description})
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
	started, terminal := false, false
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
			ch <- delivery
			mu.Unlock()
		}
	}()
	defer func() { cancel(); <-joined }()
	drain := func(ctx context.Context, seq, count int) error {
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
		d := CodemodeDrain{DeliveryID: callID, Issued: count}
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
				var args JSON
				if message.Target == "global" {
					var values []any
					if message.Args != nil {
						err = decodeWorkerJSON([]byte(*message.Args), limits, limits.MaxRecordBytes, &values)
					}
					if values == nil {
						values = []any{}
					}
					args = JSON{"args": values}
				} else if message.Args != nil {
					err = decodeWorkerJSON([]byte(*message.Args), limits, limits.MaxRecordBytes, &args)
				}
				if err != nil {
					break
				}
				if args == nil {
					args = JSON{}
				}
				call := CodemodeCall{ID: message.ID, Name: message.Name, Arguments: args, Global: message.Target == "global"}
				pending = make(chan CodemodeDelivery, 1)
				waiting[call.ID] = pending
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
					err = drain(callCtx, message.Seq, message.Issued)
				}
			case "done":
				if !started || message.During == nil || message.During.Issued != issued || message.During.Seq < lastDrain || message.During.Seq > lastDrain+1 {
					err = reject("worker done lacks deterministic position")
					break
				}
				result := CodemodeResult{Images: images, WallMs: time.Since(began).Milliseconds()}
				if message.OK {
					err = drain(callCtx, message.During.Seq, message.During.Issued)
					if err != nil {
						break
					}
					if message.Value != nil {
						result.HasValue = true
						err = decodeWorkerJSON([]byte(*message.Value), limits, limits.MaxRecordBytes, &result.Value)
						if err != nil {
							break
						}
					}
					if message.Writes == nil {
						err = reject("worker done lacks writes")
						break
					}
					var writes [][]json.RawMessage
					err = decodeWorkerJSON([]byte(*message.Writes), limits, limits.MaxRecordBytes, &writes)
					if err != nil {
						break
					}
					result.StoreWrites = JSON{}
					for _, write := range writes {
						if len(write) < 1 || len(write) > 2 {
							err = reject("invalid worker store patch")
							break
						}
						var key string
						if err = json.Unmarshal(write[0], &key); err != nil {
							break
						}
						if len(write) == 1 {
							result.StoreDeletes = append(result.StoreDeletes, key)
						} else {
							var encoded string
							if err = json.Unmarshal(write[1], &encoded); err != nil {
								break
							}
							var value any
							if err = decodeWorkerJSON([]byte(encoded), limits, limits.MaxRecordBytes, &value); err != nil {
								break
							}
							result.StoreWrites[key] = value
						}
					}
				} else {
					var scriptError string
					err = json.Unmarshal(message.Error, &scriptError)
					if err == nil {
						var detail struct {
							Message string `json:"message"`
						}
						err = json.Unmarshal([]byte(scriptError), &detail)
						result.Error = detail.Message
						if result.Error == "" {
							result.Error = "Script failed"
						}
					}
				}
				if err == nil {
					formatted := quickjs.Result{OK: message.OK, Output: outputItems}
					if message.OK && message.Value != nil {
						formatted.Value = json.RawMessage(*message.Value)
					}
					if !message.OK {
						formatted.Error = &quickjs.Error{Kind: "script", Message: result.Error}
					}
					result.Output, err = formatQuickJSText(formatted)
				}
				if err == nil {
					_, err = exchange(callCtx, CodemodeEvent{Result: &result})
					terminal = err == nil
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
					_, err = exchange(callCtx, CodemodeEvent{Result: &result})
					terminal = err == nil
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
