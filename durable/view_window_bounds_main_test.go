package durable

import "testing"

// A large declared window is still valid when the actual transcript is small.
// Native JSON limits apply to actual values, not the requested window numbers.
func TestMainTranscriptWindowLargeDeclaration(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		h, c := transcriptFixture(t, b)
		ids := transcriptNotes(t, h, 2)
		min := h.session.limits.MaxMembers + 1
		max := 2 * min
		window := ConversationWindow{Min: &min, Max: &max}
		value, err := c.View(bg, window)
		if err != nil {
			t.Fatal(err)
		}
		transcriptIDs(t, value, ids, 0)
		watch, err := c.Watch(bg, window)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		value, err = watch.Value()
		if err != nil {
			t.Fatal(err)
		}
		transcriptIDs(t, value, ids, 0)
	})
}
