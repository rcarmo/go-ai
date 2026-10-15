package durable

import (
	"context"
	"sync"
)

type codemodeCompletedCall struct {
	delivery CodemodeDelivery
	err      error
}

func (r *TaskRuntime) runParallelCodemode(ctx context.Context, vm CodemodeExecution, key string, start CodemodeStart, journalID ID, journal *codemodeJournalState, globals []CodemodeGlobal, allowed map[string]bool, paid bool) (out CodemodeResult, runErr error) {
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
	completed := make(chan codemodeCompletedCall, l.MaxPage)
	observed, delivered, launched := 0, 0, 0
	drainPending := true
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
			if event.Call != nil && event.Drain == nil && event.Result == nil {
				call := *event.Call
				if observed >= l.MaxPage || call.ID != observed+1 || !call.Global && !allowed[call.Name] || call.Global && !codemodeGlobalAllowed(start, call.Name) {
					return CodemodeResult{}, reject("parallel sandbox call authority mismatch")
				}
				call.Arguments, err = copyObject(call.Arguments, l)
				if err != nil {
					return CodemodeResult{}, err
				}
				if observed < len(journal.Issued) {
					old := journal.Issued[observed]
					if call.ID != old.ID || call.Name != old.Name || call.Global != old.Global || !equalJSONValue(call.Arguments, old.Arguments) {
						return CodemodeResult{}, reject("sandbox call replay divergence")
					}
				} else {
					journal.Issued = append(journal.Issued, call)
				}
				observed++
				continue
			}
			if event.Drain != nil && event.Call == nil && event.Result == nil {
				err := acknowledgeCodemodeDrain(linked, ack, journal, *event.Drain, observed, delivered, func(ctx context.Context, next codemodeJournalState) error {
					return commitCodemodeJournal(ctx, r, journalID, next)
				})
				if err != nil {
					return CodemodeResult{}, err
				}
				drainPending = false
				// Replay all confirmed deliveries before admitting any unsettled effect.
				if delivered == len(journal.Deliveries) {
					launch(observed)
				}
			} else if event.Result != nil && event.Call == nil && event.Drain == nil {
				if observed != len(journal.Issued) || delivered != len(journal.Deliveries) {
					return CodemodeResult{}, reject("sandbox ended before replay journal")
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
			select {
			case result := <-completed:
				if result.err != nil {
					return CodemodeResult{}, result.err
				}
				delivery = result.delivery
			case received := <-events:
				if received.err != nil {
					return CodemodeResult{}, received.err
				}
				if received.event.Result == nil || received.event.Call != nil || received.event.Drain != nil {
					return CodemodeResult{}, reject("unexpected event before delivery")
				}
				if observed != len(journal.Issued) || delivered != len(journal.Deliveries) {
					return CodemodeResult{}, reject("sandbox ended before replay journal")
				}
				return *received.event.Result, nil
			case <-linked.Done():
				return CodemodeResult{}, linked.Err()
			}
			delivery.Seq = len(journal.Deliveries) + 1
			if len(journal.Deliveries) > 0 && delivery.Now < journal.Deliveries[len(journal.Deliveries)-1].Now {
				delivery.Now = journal.Deliveries[len(journal.Deliveries)-1].Now
			}
			journal.Deliveries = append(journal.Deliveries, delivery)
			if err := commitCodemodeJournal(linked, r, journalID, *journal); err != nil {
				return CodemodeResult{}, err
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
