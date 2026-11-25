package durable

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type codemodeCompletedCall struct {
	delivery CodemodeDelivery
	err      error
}

func (r *TaskRuntime) runParallelCodemode(ctx context.Context, vm CodemodeExecution, key string, start CodemodeStart, journalID ID, journal *codemodeJournalState, globals []CodemodeGlobal, allowed map[string]bool, paid bool, completed chan codemodeCompletedCall, terminalOut *codemodeTerminal) (out CodemodeResult, runErr error) {
	divergence := func(message string) (CodemodeResult, error) {
		if err := r.abortCodemodeChildren(ctx, key, journal); err != nil {
			return CodemodeResult{}, err
		}
		return CodemodeResult{}, reject(message)
	}
	linked, cancel := context.WithCancel(ctx)
	var jobs sync.WaitGroup
	defer func() {
		cancel()
		jobs.Wait()
		if ctx.Err() == nil && runErr == nil {
			if err := r.abortCodemodeChildren(ctx, key, journal); err != nil && runErr == nil {
				runErr = err
			}
		}
	}()
	type vmEvent struct {
		event CodemodeEvent
		err   error
	}
	events := make(chan vmEvent, 1)
	jobs.Add(1)
	go func() {
		defer jobs.Done()
		for {
			event, err := nextCodemodeEvent(linked, vm)
			select {
			case events <- vmEvent{event, err}:
			case <-linked.Done():
				return
			}
			if err != nil || event.Result != nil {
				return
			}
		}
	}()
	ack, ok := vm.(CodemodeDrainExecution)
	if !ok {
		return CodemodeResult{}, reject("parallel sandbox lacks drains")
	}
	l := r.harness.session.limits
	if completed == nil {
		completed = make(chan codemodeCompletedCall, l.MaxPage)
	}
	observed, delivered, launched := 0, 0, 0
	drainPending := true
	replayPrefix := len(journal.Deliveries)
	var held *codemodeCompletedCall
	var group *codemodeGroupReady
	var terminal *codemodeTerminal
	if start.JournalVersion == codemodeGroupedJournalVersion {
		group = &codemodeGroupReady{}
	}
	deliveredIDs := map[int]bool{}
	for _, delivery := range journal.Deliveries {
		deliveredIDs[delivery.Call.ID] = true
	}
	modelCapacity := make(chan struct{}, 4)
	launch := func(count int) {
		for launched < count {
			call := journal.Issued[launched]
			launched++
			if deliveredIDs[call.ID] {
				continue
			}
			jobs.Add(1)
			go func() {
				defer jobs.Done()
				if call.Global && (call.Name == "models.classify" || call.Name == "models.generateImages") {
					select {
					case modelCapacity <- struct{}{}:
						defer func() { <-modelCapacity }()
					case <-linked.Done():
						return
					}
				}
				if linked.Err() != nil {
					return
				}
				delivery, err := r.executeCodemodeCall(linked, key, start, call, globals, paid)
				select {
				case completed <- codemodeCompletedCall{delivery, err}:
				case <-linked.Done():
				}
			}()
		}
	}
	for {
		if err := linked.Err(); err != nil {
			return CodemodeResult{}, err
		}
		if drainPending {
			var received vmEvent
			select {
			case received = <-events:
			case <-linked.Done():
				return CodemodeResult{}, linked.Err()
			}
			event, err := received.event, received.err
			if err != nil {
				return CodemodeResult{}, err
			}
			if terminal != nil {
				if event.Result == nil || event.Result.Error != "" || event.Call != nil || event.Drain != nil {
					return divergence("codemode terminal position lacks successful result")
				}
				*terminalOut = *terminal
				return *event.Result, nil
			}
			if event.Call != nil && event.Drain == nil && event.Result == nil {
				call := *event.Call
				if observed >= l.MaxPage || call.ID != observed+1 || !call.Global && !allowed[call.Name] || call.Global && !codemodeGlobalAllowed(start, call) {
					return divergence("parallel sandbox call authority mismatch")
				}
				call.Arguments, err = ownCodemodeArguments(call, l)
				if err != nil {
					return CodemodeResult{}, err
				}
				if observed < len(journal.Issued) {
					old := journal.Issued[observed]
					if call.ID != old.ID || call.Name != old.Name || call.Global != old.Global || !codemodeArgumentModeCompatible(call, old) || !equalJSONValue(call.Arguments, old.Arguments) {
						label := call.Name
						if call.Global != old.Global {
							label = fmt.Sprintf("%s, but was %s", codemodeReplayCallLabel(call), codemodeReplayCallLabel(old))
						} else if !call.Global {
							label = "tools." + label
						}
						return divergence(fmt.Sprintf("sandbox call replay divergence: call %d is %s; the script took a different path when resumed after a restart", call.ID, label))
					}
				} else {
					journal.Issued = append(journal.Issued, call)
				}
				observed++
				continue
			}
			if event.Drain != nil && event.Call == nil && event.Result == nil {
				var ready <-chan codemodeCompletedCall
				if delivered > replayPrefix && delivered < observed && delivered == len(journal.Deliveries) && launched == observed && !event.Drain.Final {
					ready = completed
				}
				commit := func(ctx context.Context, next codemodeJournalState, withDelivery *codemodeJournalState) (bool, error) {
					return commitCodemodeJournalWithReady(ctx, r, journalID, next, withDelivery)
				}
				var err error
				if group == nil {
					held, err = settleCodemodeDrain(linked, ack, journal, *event.Drain, observed, delivered, ready, commit)
				} else {
					terminal, err = settleGroupedCodemodeDrain(linked, ack, start, journal, *event.Drain, observed, delivered, group, ready, commit)
				}
				if err != nil {
					if linked.Err() == nil && strings.Contains(err.Error(), "codemode drain") {
						return divergence(err.Error())
					}
					return CodemodeResult{}, err
				}
				if terminal != nil {
					continue // The next event is the terminal result, not a suffix delivery.
				}
				drainPending = false
				// Admit effects only after replay catches up to a live drain.
				// A final drain records the count but the guest has already ended.
				if !event.Drain.Final && delivered == len(journal.Deliveries) {
					launch(observed)
				}
			} else if event.Result != nil && event.Call == nil && event.Drain == nil {
				if group != nil {
					return divergence("grouped codemode ended without terminal position")
				}
				if observed != len(journal.Issued) || delivered != len(journal.Deliveries) {
					return divergence("sandbox ended before replay journal")
				}
				if event.Result.Error != "" {
					if err := commitCodemodeJournal(linked, r, journalID, *journal); err != nil {
						return CodemodeResult{}, err
					}
				}
				return *event.Result, nil
			} else {
				return CodemodeResult{}, reject("invalid parallel sandbox event union")
			}
		}
		var delivery CodemodeDelivery
		if delivered < len(journal.Deliveries) {
			delivery = journal.Deliveries[delivered]
		} else {
			// A completed script emits its result after the final drain. When no
			// calls remain, read that event rather than waiting for a host result.
			if delivered == observed {
				drainPending = true
				continue
			}
			var next <-chan codemodeCompletedCall = completed
			if group != nil && len(group.pending) > 0 {
				first := make(chan codemodeCompletedCall, 1)
				first <- group.pending[0]
				next = first
			} else if held != nil {
				// A failed pre-stage fold leaves the result at the head of
				// the queue, still competing with terminal events/cancellation.
				first := make(chan codemodeCompletedCall, 1)
				first <- *held
				next, held = first, nil
			}
			select {
			case result := <-next:
				if result.err != nil {
					return CodemodeResult{}, result.err
				}
				delivery = result.delivery
				if group != nil && len(group.pending) == 0 {
					group.pending = append(group.pending, result)
				}
			case received := <-events:
				if received.err != nil {
					return CodemodeResult{}, received.err
				}
				if received.event.Result == nil || received.event.Call != nil || received.event.Drain != nil {
					return CodemodeResult{}, reject("unexpected event before delivery")
				}
				if group != nil {
					return divergence("grouped codemode ended without terminal position")
				}
				if observed != len(journal.Issued) || delivered != len(journal.Deliveries) {
					return divergence("sandbox ended before replay journal")
				}
				return *received.event.Result, nil
			case <-linked.Done():
				return CodemodeResult{}, linked.Err()
			}
			if group == nil {
				delivery = nextParallelCodemodeDelivery(*journal, delivery)
				journal.Deliveries = append(journal.Deliveries, delivery)
				if err := commitCodemodeJournal(linked, r, journalID, *journal); err != nil {
					return CodemodeResult{}, err
				}
			} else {
				group.collect(completed)
				first, n := group.candidate(*journal, observed, 1)
				if first == nil {
					return CodemodeResult{}, reject("group completion outside admitted calls")
				}
				preferred, more := group.candidate(*journal, observed, len(group.pending))
				if more == 1 {
					preferred = nil
				}
				fused, err := commitCodemodeJournalWithReady(linked, r, journalID, *first, preferred)
				if err != nil {
					return CodemodeResult{}, err
				}
				if fused {
					first, n = preferred, more
				}
				*journal = *first
				group.consume(n)
				delivery = journal.Deliveries[delivered]
			}
		}
		if delivery.Seq != delivered+1 {
			return CodemodeResult{}, reject("codemode delivery sequence gap")
		}
		object, err := dtoObject(delivery, l)
		if err != nil {
			return CodemodeResult{}, err
		}
		var owned CodemodeDelivery
		if err := fromObject(object, &owned, l); err != nil {
			return CodemodeResult{}, err
		}
		if err := deliverCodemodeEvent(linked, vm, owned); err != nil {
			return CodemodeResult{}, err
		}
		delivered++
		drainPending = true
	}
}

// Show the call target and the empty argument forms needed to distinguish a
// tool call from a global. Do not copy argument values into model diagnostics.
func codemodeReplayCallLabel(call CodemodeCall) string {
	if call.Global {
		if call.UndefinedArgument {
			return call.Name + "()"
		}
		return call.Name + "(…)"
	}
	if len(call.Arguments) == 0 && len(call.UTF16Arguments) == 0 {
		return "tools." + call.Name + "({})"
	}
	return "tools." + call.Name + "(…)"
}
