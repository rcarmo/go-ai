package durable

import "context"

// CodemodeRemoteServer adapts a sandbox to the native remote transport contract.
// Network transports supply the authenticated exchange/receive functions. The
// server never receives host tool implementations or ambient filesystem access.
type CodemodeRemoteServer struct {
	Sandbox CodemodeSandbox
	// MaxInterruptBudget defaults to the upstream 100000-poll server ceiling.
	MaxInterruptBudget int
	// MaxMemoryLimitBytes defaults to the native 32MiB heap ceiling.
	MaxMemoryLimitBytes int
}

func (s *CodemodeRemoteServer) Execute(ctx context.Context, code string, start CodemodeStart, exchange func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error), receive func(context.Context) (CodemodeDelivery, error)) error {
	if ctx == nil || s == nil || s.Sandbox == nil || exchange == nil || receive == nil {
		return reject("invalid remote sandbox server")
	}
	maximum := s.MaxInterruptBudget
	if maximum == 0 {
		maximum = DefaultCodemodeInterruptBudget
	}
	if maximum < 1 || maximum > 1000000 || start.InterruptBudget < 0 {
		return reject("invalid remote interrupt ceiling")
	}
	if start.InterruptBudget == 0 || start.InterruptBudget > maximum {
		start.InterruptBudget = maximum
	}
	memoryMaximum := s.MaxMemoryLimitBytes
	if memoryMaximum == 0 {
		memoryMaximum = DefaultCodemodeMemoryLimitBytes
	}
	if memoryMaximum < 1 || memoryMaximum > 256<<20 || start.MemoryLimitBytes < 0 {
		return reject("invalid remote memory ceiling")
	}
	if start.MemoryLimitBytes == 0 || start.MemoryLimitBytes > memoryMaximum {
		start.MemoryLimitBytes = memoryMaximum
	}
	linked, cancel := context.WithCancel(ctx)
	defer cancel()
	vm, err := startCodemodeSandbox(linked, s.Sandbox, code, start)
	if err != nil {
		return err
	}
	defer closeCodemodeSandbox(vm)
	ack, ok := vm.(CodemodeDrainExecution)
	if !ok {
		return reject("remote sandbox lacks drain protocol")
	}
	errors := make(chan error, 1)
	joined := make(chan struct{})
	admitted := make(chan struct{}, 1)
	go func() {
		defer close(joined)
		for {
			delivery, err := receive(linked)
			if err == nil {
				select {
				case <-admitted:
					err = deliverCodemodeEvent(linked, vm, delivery)
				case <-linked.Done():
					err = linked.Err()
				}
			}
			if err != nil {
				select {
				case errors <- err:
				default:
				}
				cancel()
				return
			}
		}
	}()
	defer func() { cancel(); <-joined }()
	for {
		event, err := nextCodemodeEvent(linked, vm)
		if err != nil {
			select {
			case cause := <-errors:
				return cause
			default:
				return err
			}
		}
		reply, err := exchange(linked, event)
		if err != nil {
			return err
		}
		if event.Result != nil {
			return nil
		}
		if reply.Stop {
			return context.Canceled
		}
		if event.Drain != nil {
			if reply.Acknowledged == nil || *reply.Acknowledged != *event.Drain {
				return reject("remote drain acknowledgement mismatch")
			}
			if err := ack.AcknowledgeDrain(linked, *reply.Acknowledged); err != nil {
				return err
			}
			select {
			case admitted <- struct{}{}:
			case <-linked.Done():
				return linked.Err()
			}
		}
	}
}
