package quickjs

import "fmt"

func acquireWorkerCapacity(acquire func() (func(), error)) (release func(), err error) {
	defer func() {
		if recover() != nil {
			release = nil
			err = fmt.Errorf("worker capacity callback failed")
		}
	}()
	return acquire()
}

func notifyWorkerStarted(started func(<-chan struct{}) error, exited <-chan struct{}) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("worker-start callback failed")
		}
	}()
	return started(exited)
}
