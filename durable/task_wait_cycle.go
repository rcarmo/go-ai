package durable

// Persisted wait admission follows pi-durable v1.5.0 scheduler.ts:2470-2581
// (MIT, Copyright (c) 2025 Mario Zechner). This is separate from invocation-bound
// volatile waits: every live task holds its ordinary owned work, not only tasks
// that have already selected an outcome.
func persistedWaitCycle(state Snapshot, task ID, on []ID) []ID {
	children := make(map[ID][]ID)
	for _, id := range ids(state.Tasks) {
		record := state.Tasks[id]
		parent := record.Owner
		if parent == 0 {
			parent = state.Conversations[record.Conversation].Owner
		}
		if parent != 0 {
			children[parent] = append(children[parent], id)
		}
	}
	from := make(map[ID]ID)
	walked := make(map[ID]bool)
	pending := []ID{}
	reach := func(id, via ID) {
		if _, seen := from[id]; !seen {
			from[id] = via
			pending = append(pending, id)
		}
	}
	for _, id := range on {
		member, exists := state.Tasks[id]
		if exists && !terminalStatus(member.Status) && member.Owner != task {
			reach(id, 0)
		}
	}
	for len(pending) > 0 {
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if id == task {
			path := []ID{}
			for at := id; at != 0; at = from[at] {
				path = append(path, at)
			}
			for a, b := 0, len(path)-1; a < b; a, b = a+1, b-1 {
				path[a], path[b] = path[b], path[a]
			}
			return path
		}
		record, exists := state.Tasks[id]
		if !exists || terminalStatus(record.Status) {
			continue
		}
		if record.Execution != nil && record.Execution.Native != nil && record.Execution.Native.State.Status == "waiting" && !taskAborted(record) {
			for _, dependency := range record.Execution.Native.State.On {
				reach(dependency, id)
			}
		}
		if walked[id] {
			continue
		}
		walked[id] = true
		descendants := append([]ID(nil), children[id]...)
		for len(descendants) > 0 {
			below := descendants[len(descendants)-1]
			descendants = descendants[:len(descendants)-1]
			child, exists := state.Tasks[below]
			if !exists || taskBackground(child) {
				continue
			}
			if !terminalStatus(child.Status) {
				reach(below, id)
			}
			if !walked[below] {
				walked[below] = true
				descendants = append(descendants, children[below]...)
			}
		}
	}
	return nil
}
