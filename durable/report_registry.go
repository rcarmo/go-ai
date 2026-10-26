package durable

// taskInstaller reads only registration metadata, without cloning tools or
// calling any extension code. The last extension wins, as in effectiveTasksLocked.
func (r *Registry) taskInstaller(kind string) string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := len(r.extensions) - 1; i >= 0; i-- {
		if r.extensions[i].tasks[kind] != nil {
			return r.extensions[i].name
		}
	}
	return ""
}
