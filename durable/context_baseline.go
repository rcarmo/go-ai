package durable

import goai "github.com/rcarmo/go-ai"

// A head marker or pi.system entry establishes a baseline. An edit replacing
// such a contribution retains its identity; later system deltas do not become
// baselines merely because no earlier system message is visible.
func contextHasSystemBaseline(view ContextView) bool {
	for i, entry := range view.Entries {
		if i >= len(view.Contributions) {
			break
		}
		part := view.Contributions[i]
		if entry.Kind == "pi.system" || entry.Head != 0 {
			for _, m := range part {
				if m.Role == goai.RoleSystem {
					return true
				}
			}
			continue
		}
		// Imported non-system history can precede a baseline. An earlier
		// app-provided system message prevents a later delta becoming baseline.
		for _, m := range part {
			if m.Role == goai.RoleSystem {
				return false
			}
		}
	}
	return false
}
