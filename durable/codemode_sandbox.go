package durable

import "context"

// Host sandbox callbacks run outside the Session line. Contain callback panics
// without exposing arbitrary host panic data to durable records or scripts.
func startCodemodeSandbox(ctx context.Context, sandbox CodemodeSandbox, source string, start CodemodeStart) (vm CodemodeExecution, err error) {
	defer func() {
		if recover() != nil {
			err = reject("codemode sandbox start panic")
		}
	}()
	return sandbox.Start(ctx, source, start)
}
func nextCodemodeEvent(ctx context.Context, vm CodemodeExecution) (event CodemodeEvent, err error) {
	defer func() {
		if recover() != nil {
			err = reject("codemode sandbox event panic")
		}
	}()
	return vm.Next(ctx)
}
func deliverCodemodeEvent(ctx context.Context, vm CodemodeExecution, delivery CodemodeDelivery) (err error) {
	defer func() {
		if recover() != nil {
			err = reject("codemode sandbox delivery panic")
		}
	}()
	return vm.Deliver(ctx, delivery)
}
func closeCodemodeSandbox(vm CodemodeExecution) (err error) {
	defer func() {
		if recover() != nil {
			err = reject("codemode sandbox close panic")
		}
	}()
	return vm.Close()
}
