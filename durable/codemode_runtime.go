package durable

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"time"
)

const codemodeRunKind = "pi.codemode.run"
const codemodeJournalKind = "pi.codemode.journal"

// CodemodeSandbox is a host-supplied isolated VM. Implementations must enforce
// memory/CPU limits, provide no ambient filesystem/network/process authority,
// and derive clock/random behaviour from Start. This interface itself does not
// implement a JS engine. No automatic model tool is installed without one.
type CodemodeSandbox interface {
	Start(context.Context, string, CodemodeStart) (CodemodeExecution, error)
}

// CodemodeSandboxIdentity optionally pins a host VM's replay semantics. Hosts
// without an identity must themselves preserve semantics across reopen.
type CodemodeSandboxIdentity interface{ RuntimeIdentity() string }

// CodemodeSandboxDrains advertises drain-count acknowledgement support.
type CodemodeSandboxParallel interface{ SupportsParallelCalls() bool }

type CodemodeSandboxDrains interface{ SupportsDrainAcknowledgements() bool }

// DefaultCodemodeInterruptBudget matches pi-codemode 1.2.0 inline execution.
const DefaultCodemodeInterruptBudget = 100000

// DefaultCodemodeMemoryLimitBytes preserves the native QuickJS heap ceiling.
const DefaultCodemodeMemoryLimitBytes = 32 << 20

type CodemodeStart struct {
	MemoryLimitBytes      int                         `json:"memoryLimitBytes,omitempty"`
	ParallelCalls         bool                        `json:"parallelCalls,omitempty"`
	DrainAcknowledgements bool                        `json:"drainAcknowledgements,omitempty"`
	InterruptBudget       int                         `json:"interruptBudget,omitempty"`
	Globals               []CodemodeGlobalDescription `json:"globals,omitempty"`
	PaidModels            bool                        `json:"paidModels,omitempty"`
	Namespaces            []CodemodeNamespace         `json:"namespaces,omitempty"`
	RuntimeIdentity       string                      `json:"runtimeIdentity,omitempty"`
	Deadline              int64                       `json:"deadline"`
	MaxOutputTokens       int                         `json:"maxOutputTokens"`
	Store                 JSON                        `json:"store"`
	Version               int                         `json:"version"`
	StartedAt             int64                       `json:"startedAt"`
	Seed                  uint64                      `json:"seed"`
	Tools                 []CodemodeToolDescription   `json:"tools"`
}
type CodemodeExecution interface {
	Next(context.Context) (CodemodeEvent, error)
	Deliver(context.Context, CodemodeDelivery) error
	Close() error
}

// CodemodeDrain identifies a completed VM drain. DeliveryID zero denotes
// initial execution; Issued is the cumulative issued-call count.
type CodemodeDrain struct {
	DeliveryID int
	Issued     int
}

// CodemodeDrainExecution acknowledges a count after the host persists or verifies it.
type CodemodeDrainExecution interface {
	AcknowledgeDrain(context.Context, CodemodeDrain) error
}

// Exactly one Call, Drain or Result is emitted by Next. Calls remain serial.
type CodemodeEvent struct {
	Drain  *CodemodeDrain
	Call   *CodemodeCall
	Result *CodemodeResult
}
type CodemodeCall struct {
	Global    bool   `json:"global,omitempty"`
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Arguments JSON   `json:"arguments"`
}
type CodemodeResult struct {
	WallMs         int64               `json:"wallMs"`
	FullOutputPath string              `json:"fullOutputPath,omitempty"`
	Images         []goai.ImageContent `json:"images,omitempty"`
	StoreDeletes   []string            `json:"storeDeletes,omitempty"`
	StoreWrites    JSON                `json:"storeWrites,omitempty"`
	Output         string              `json:"output"`
	Value          any                 `json:"value,omitempty"`
	HasValue       bool                `json:"hasValue"`
	Error          string              `json:"error,omitempty"`
}
type CodemodeDelivery struct {
	IssuedAfter *int         `json:"issuedAfter,omitempty"`
	Seq         int          `json:"seq"`
	Now         int64        `json:"now"`
	Call        CodemodeCall `json:"call"`
	Value       any          `json:"value,omitempty"`
	HasValue    bool         `json:"hasValue"`
	Error       string       `json:"error,omitempty"`
}
type codemodeRunState struct {
	OutputDir string          `json:"outputDir,omitempty"`
	OutputFS  string          `json:"outputFS,omitempty"`
	Source    string          `json:"source"`
	Start     CodemodeStart   `json:"start"`
	Result    *CodemodeResult `json:"result,omitempty"`
}
type codemodeJournalState struct {
	FirstIssued *int               `json:"firstIssued,omitempty"`
	Issued      []CodemodeCall     `json:"issued"`
	Deliveries  []CodemodeDelivery `json:"deliveries"`
}

// RunCodemode persists a serial deterministic replay boundary in caller-owned
// documents. Host effects launch only after their call record commits; delivery
// commits before VM resolution. A replayed call must match ID/name/arguments.
// Source is a durable JSON string, never evaluated by the host Go process.
func (r *TaskRuntime) RunCodemode(ctx context.Context, key, source string, sandbox CodemodeSandbox) (CodemodeResult, error) {
	return r.runCodemode(ctx, key, source, sandbox, nil, nil, nil, nil)
}

// Namespace overrides apply to new runs; replay uses persisted metadata.
func (r *TaskRuntime) runCodemode(ctx context.Context, key, source string, sandbox CodemodeSandbox, namespaces *[]CodemodeNamespace, models *bool, globals *[]CodemodeGlobal, interruptBudget *int) (CodemodeResult, error) {
	if err := r.check(); err != nil {
		return CodemodeResult{}, err
	}
	if ctx == nil || sandbox == nil {
		return CodemodeResult{}, reject("codemode context/sandbox required")
	}
	r.codemodeMu.Lock()
	if r.codemodeActive[key] {
		r.codemodeMu.Unlock()
		return CodemodeResult{}, reject("codemode run already active")
	}
	if r.codemodeActive == nil {
		r.codemodeActive = map[string]bool{}
	}
	r.codemodeActive[key] = true
	r.codemodeMu.Unlock()
	defer func() { r.codemodeMu.Lock(); delete(r.codemodeActive, key); r.codemodeMu.Unlock() }()
	l := r.harness.session.limits
	if !validNestedKey(key) || len(key) > l.MaxRequestIDBytes || len(source) > l.MaxStringBytes {
		return CodemodeResult{}, reject("invalid codemode key/source")
	}
	parsed, err := ParseCodemodeSource(source)
	if err != nil {
		return CodemodeResult{}, err
	}
	identity := ""
	if pinned, ok := sandbox.(CodemodeSandboxIdentity); ok {
		identity = pinned.RuntimeIdentity()
		if identity == "" || len(identity) > l.MaxStringBytes {
			return CodemodeResult{}, reject("invalid codemode runtime identity")
		}
	}
	tools, err := r.CodemodeTools(ctx)
	if err != nil {
		return CodemodeResult{}, err
	}
	now, err := r.Now()
	if err != nil {
		return CodemodeResult{}, err
	}
	var random [8]byte
	if _, err = rand.Read(random[:]); err != nil {
		return CodemodeResult{}, err
	}
	if now > int64(^uint64(0)>>1)-parsed.TimeoutMs {
		return CodemodeResult{}, reject("codemode deadline overflow")
	}
	paid := r.harness.options.PaidModels
	if models != nil {
		paid = paid && *models
	}
	selectedNamespaces := r.harness.options.CodemodeNamespaces
	if namespaces != nil {
		selectedNamespaces = *namespaces
	}
	budget := DefaultCodemodeInterruptBudget
	if interruptBudget != nil {
		budget = *interruptBudget
	}
	if budget < 0 || budget > 1000000 {
		return CodemodeResult{}, reject("invalid codemode interrupt budget")
	}
	start := CodemodeStart{InterruptBudget: budget, PaidModels: paid, Namespaces: append([]CodemodeNamespace(nil), selectedNamespaces...), RuntimeIdentity: identity, Deadline: now + parsed.TimeoutMs, MaxOutputTokens: parsed.MaxOutputTokens, Version: 2, StartedAt: now, Seed: uint64(binary.LittleEndian.Uint32(random[:4])), Tools: tools}
	if drains, ok := sandbox.(CodemodeSandboxDrains); ok {
		start.DrainAcknowledgements = drains.SupportsDrainAcknowledgements()
	}
	if parallel, ok := sandbox.(CodemodeSandboxParallel); ok {
		start.ParallelCalls = parallel.SupportsParallelCalls()
		if start.ParallelCalls {
			start.DrainAcknowledgements = true
		}
	}
	selectedGlobals := r.harness.options.CodemodeGlobals
	if globals != nil {
		selectedGlobals = *globals
	}
	for _, global := range selectedGlobals {
		start.Globals = append(start.Globals, codemodeGlobalDescription(global))
	}
	var runID, journalID ID
	var run codemodeRunState
	var journal codemodeJournalState
	err = r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		for _, doc := range tx.state.Documents {
			if doc.Scope != "task" || doc.Owner != r.taskID || !doc.Family || doc.Key != key || doc.Retired {
				continue
			}
			switch doc.Kind {
			case codemodeRunKind:
				runID = doc.ID
				if err := fromObject(doc.Value, &run, l); err != nil {
					return nil, err
				}
			case codemodeJournalKind:
				journalID = doc.ID
				if err := fromObject(doc.Value, &journal, l); err != nil {
					return nil, err
				}
			}
		}
		if runID != 0 {
			if journalID == 0 || run.Source != source || run.Start.Version != 2 || run.Start.RuntimeIdentity != identity {
				return nil, reject("codemode replay identity mismatch")
			}
			return nil, nil
		}
		if journalID != 0 {
			return nil, reject("orphan codemode journal")
		}
		store, err := tx.AcquireDocument(codemodeStoreDefinition(), r.conversation, nil, nil)
		if err != nil {
			return nil, err
		}
		stored, err := store.Get()
		if err != nil {
			return nil, err
		}
		values, ok := stored["values"].(map[string]any)
		if !ok {
			return nil, reject("codemode store shape")
		}
		start.Store, err = copyObject(JSON(values), l)
		if err != nil {
			return nil, err
		}
		run = codemodeRunState{Source: source, Start: start}
		journal = codemodeJournalState{Issued: []CodemodeCall{}, Deliveries: []CodemodeDelivery{}}
		for _, item := range []struct {
			kind  string
			value any
			id    *ID
		}{{codemodeRunKind, run, &runID}, {codemodeJournalKind, journal, &journalID}} {
			id, err := tx.MintID()
			if err != nil {
				return nil, err
			}
			value, err := dtoObject(item.value, l)
			if err != nil {
				return nil, err
			}
			if _, err = tx.CreateDocument(Document{ID: id, Scope: "task", Owner: r.taskID, Kind: item.kind, Version: 1, Family: true, Key: key, Value: value}); err != nil {
				return nil, err
			}
			*item.id = id
		}
		return nil, nil
	})
	if err != nil {
		return CodemodeResult{}, err
	}
	if run.Result != nil {
		return ownCodemodeResult(*run.Result, l)
	}
	current, err := r.Now()
	if err != nil {
		return CodemodeResult{}, err
	}
	if current < run.Start.StartedAt {
		current = run.Start.StartedAt
	} // clock rollback cannot extend original timeout
	remaining := run.Start.Deadline - current
	if current >= run.Start.Deadline || remaining <= 0 {
		return CodemodeResult{}, context.DeadlineExceeded
	}
	linked, cancel := context.WithTimeout(ctx, time.Duration(remaining)*time.Millisecond)
	stop := context.AfterFunc(r.context, cancel)
	defer func() { stop(); cancel() }()
	startObject, err := dtoObject(run.Start, l)
	if err != nil {
		return CodemodeResult{}, err
	}
	var vmStart CodemodeStart
	if err = fromObject(startObject, &vmStart, l); err != nil {
		return CodemodeResult{}, err
	}
	vm, err := startCodemodeSandbox(linked, sandbox, parsed.Code, vmStart)
	if err != nil {
		return CodemodeResult{}, err
	}
	if vm == nil {
		return CodemodeResult{}, reject("sandbox returned nil execution")
	}
	defer func() {
		if err := closeCodemodeSandbox(vm); err != nil {
			r.harness.scheduler.report(err)
		}
	}()
	allowed := map[string]bool{}
	for _, tool := range run.Start.Tools {
		allowed[tool.Name] = true
	}
	var parallelResult *CodemodeResult
	if run.Start.ParallelCalls {
		result, err := r.runParallelCodemode(linked, vm, key, run.Start, journalID, &journal, selectedGlobals, allowed, paid)
		if err != nil {
			return CodemodeResult{}, err
		}
		parallelResult = &result
	}
	index := 0
	if parallelResult != nil {
		index = len(journal.Issued)
	}
	ackDrain := func(drain CodemodeDrain, observed int) error {
		ack, ok := vm.(CodemodeDrainExecution)
		if !run.Start.DrainAcknowledgements || !ok {
			return reject("unexpected codemode drain")
		}
		return acknowledgeCodemodeDrain(linked, ack, &journal, drain, observed, index, func(ctx context.Context, next codemodeJournalState) error {
			return commitCodemodeJournal(ctx, r, journalID, next)
		})
	}
	for index <= l.MaxPage {
		if err = r.check(); err != nil {
			return CodemodeResult{}, err
		}
		if err = linked.Err(); err != nil {
			return CodemodeResult{}, err
		}
		var event CodemodeEvent
		if parallelResult != nil {
			event.Result = parallelResult
		} else {
			event, err = nextCodemodeEvent(linked, vm)
		}
		if err != nil {
			return CodemodeResult{}, err
		}
		if err = linked.Err(); err != nil {
			return CodemodeResult{}, err
		}
		if event.Drain != nil {
			if event.Call != nil || event.Result != nil {
				return CodemodeResult{}, reject("invalid sandbox event union")
			}
			if err := ackDrain(*event.Drain, index); err != nil {
				return CodemodeResult{}, err
			}
			continue
		}
		if (event.Call == nil) == (event.Result == nil) {
			return CodemodeResult{}, reject("invalid sandbox event union")
		}
		if event.Result != nil {
			if !run.Start.ParallelCalls && run.Start.DrainAcknowledgements && event.Result.Error != "" && (journal.FirstIssued == nil || index > 0 && journal.Deliveries[index-1].IssuedAfter == nil) {
				result := *event.Result
				result.StoreWrites, result.StoreDeletes = nil, nil
				return ownCodemodeResult(result, l)
			}
			if index < len(journal.Issued) || index < len(journal.Deliveries) {
				return CodemodeResult{}, reject("sandbox ended before replay journal")
			}
			// Validate and detach every persisted field before external writes.
			// Text is projected separately because the full text may need spill.
			fullText := event.Result.Output
			if notice := codemodeUnshownImageNotice(journal.Deliveries, len(event.Result.Images)); notice != "" {
				if fullText != "" {
					fullText += "\n"
				}
				fullText += notice
			}
			if err := validateCodemodeFullText(fullText); err != nil {
				return CodemodeResult{}, err
			}
			projection := *event.Result
			finished, err := r.Now()
			if err != nil {
				return CodemodeResult{}, err
			}
			projection.WallMs = 0
			if finished > run.Start.StartedAt {
				projection.WallMs = finished - run.Start.StartedAt
			}
			projection.FullOutputPath = "" // only the environment writer grants paths
			projection.Output = projectCodemodeOutput(fullText, codemodeInlineTokens(run.Start.MaxOutputTokens, l))
			bounded, err := ownCodemodeResult(projection, l)
			if err != nil {
				return CodemodeResult{}, err
			}
			bounded.Output = fullText
			bounded, err = r.prepareCodemodeOutput(linked, key, bounded, run.Start.MaxOutputTokens)
			if err != nil {
				return CodemodeResult{}, err
			}
			result, err := ownCodemodeResult(bounded, l)
			if err != nil {
				return CodemodeResult{}, err
			}
			err = r.Commit(linked, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				if err := linked.Err(); err != nil {
					return nil, err
				}
				now, err := r.Now()
				if err != nil {
					return nil, err
				}
				if now >= run.Start.Deadline {
					return nil, context.DeadlineExceeded
				}
				doc, err := tx.Document(runID)
				if err != nil {
					return nil, err
				}
				if result.Error == "" && (len(result.StoreWrites) > 0 || len(result.StoreDeletes) > 0) {
					store, err := tx.AcquireDocument(codemodeStoreDefinition(), r.conversation, nil, nil)
					if err != nil {
						return nil, err
					}
					stored, err := store.Get()
					if err != nil {
						return nil, err
					}
					values, ok := stored["values"].(map[string]any)
					if !ok {
						return nil, reject("codemode store shape")
					}
					for _, key := range result.StoreDeletes {
						delete(values, key)
					}
					for key, value := range result.StoreWrites {
						values[key] = value
					}
					if err = store.Set(stored); err != nil {
						return nil, err
					}
				}
				latest, err := doc.Get()
				if err != nil {
					return nil, err
				}
				var currentRun codemodeRunState
				if err = fromObject(latest, &currentRun, l); err != nil {
					return nil, err
				}
				run.OutputDir, run.OutputFS = currentRun.OutputDir, currentRun.OutputFS
				run.Result = &result
				object, err := dtoObject(run, l)
				if err != nil {
					return nil, err
				}
				return nil, doc.Set(object)
			})
			return result, err
		}
		if index == l.MaxPage {
			return CodemodeResult{}, reject("codemode serial call budget exhausted")
		}
		call := *event.Call
		if call.ID != index+1 || (!call.Global && !allowed[call.Name]) || (call.Global && !codemodeGlobalAllowed(run.Start, call.Name)) {
			return CodemodeResult{}, reject("sandbox call ID/authority mismatch")
		}
		call.Arguments, err = copyObject(call.Arguments, l)
		if err != nil {
			return CodemodeResult{}, err
		}
		if index < len(journal.Issued) {
			old := journal.Issued[index]
			if old.ID != call.ID || old.Name != call.Name || old.Global != call.Global || !equalJSONValue(old.Arguments, call.Arguments) {
				return CodemodeResult{}, reject("sandbox call replay divergence")
			}
		} else {
			journal.Issued = append(journal.Issued, call)
			if err = commitCodemodeJournal(linked, r, journalID, journal); err != nil {
				return CodemodeResult{}, err
			}
		}
		if run.Start.DrainAcknowledgements {
			drainEvent, err := nextCodemodeEvent(linked, vm)
			if err != nil {
				return CodemodeResult{}, err
			}
			if drainEvent.Result != nil && drainEvent.Call == nil && drainEvent.Drain == nil && drainEvent.Result.Error != "" {
				// Fatal VM termination cannot certify the pending drain. Admit no
				// effect and discard store mutations; the incomplete run can retry.
				result := *drainEvent.Result
				result.StoreWrites, result.StoreDeletes = nil, nil
				return ownCodemodeResult(result, l)
			}
			if drainEvent.Drain == nil || drainEvent.Call != nil || drainEvent.Result != nil {
				return CodemodeResult{}, reject("missing serial codemode drain")
			}
			if err := ackDrain(*drainEvent.Drain, index+1); err != nil {
				return CodemodeResult{}, err
			}
		}
		var delivery CodemodeDelivery
		if index < len(journal.Deliveries) {
			delivery = journal.Deliveries[index]
			if delivery.Seq != index+1 {
				return CodemodeResult{}, reject("codemode delivery sequence gap")
			}
		} else {
			delivery, err = r.executeCodemodeCall(linked, key, run.Start, call, selectedGlobals, paid)
			if err != nil {
				return CodemodeResult{}, err
			}
			delivery.Seq = index + 1
			journal.Deliveries = append(journal.Deliveries, delivery)
			if err = commitCodemodeJournal(linked, r, journalID, journal); err != nil {
				return CodemodeResult{}, err
			}
		}
		deliveryObject, e := dtoObject(delivery, l)
		if e != nil {
			return CodemodeResult{}, e
		}
		var vmDelivery CodemodeDelivery
		if e = fromObject(deliveryObject, &vmDelivery, l); e != nil {
			return CodemodeResult{}, e
		}
		if err = deliverCodemodeEvent(linked, vm, vmDelivery); err != nil {
			return CodemodeResult{}, err
		}
		index++
	}
	return CodemodeResult{}, errors.New("codemode serial call budget exhausted")
}
func commitCodemodeJournal(ctx context.Context, r *TaskRuntime, id ID, journal codemodeJournalState) error {
	return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		doc, err := tx.Document(id)
		if err != nil {
			return nil, err
		}
		object, err := dtoObject(journal, tx.limits)
		if err != nil {
			return nil, err
		}
		return nil, doc.Set(object)
	})
}
func ownCodemodeResult(result CodemodeResult, l Limits) (CodemodeResult, error) {
	object, err := dtoObject(result, l)
	if err != nil {
		return CodemodeResult{}, err
	}
	var owned CodemodeResult
	err = fromObject(object, &owned, l)
	if err != nil {
		return CodemodeResult{}, err
	}
	if err = validateCodemodeResult(owned, l); err != nil {
		return CodemodeResult{}, err
	}
	return owned, nil
}
