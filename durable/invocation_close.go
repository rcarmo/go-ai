package durable

// takeWatches detaches resource ownership without requiring the Session line.
// Acquisition still happens on that line and rechecks the close fence while
// holding the same resource mutex; a late acquisition stops its own watch.
func (r *TaskRuntime) takeWatches() []*DocumentWatch {
	r.resourceMu.Lock()
	defer r.resourceMu.Unlock()
	watches := r.watches
	r.watches = nil
	return watches
}

// requestClose seals invocation authority before cancellation. It does not
// mutate a persisted task, publish an outcome or charge a restart. In-flight
// admitted storage still drains before Close requeues the interrupted task.
func (r *TaskRuntime) requestClose() {
	if !r.closeRequested.CompareAndSwap(false, true) {
		return
	}
	for _, watch := range r.takeWatches() {
		watch.Stop()
	}
	r.stopTimers()
	r.cancel()
}

func (s *taskScheduler) signalClose() {
	s.active.Range(func(key, value any) bool { key.(*TaskRuntime).requestClose(); return true })
}
