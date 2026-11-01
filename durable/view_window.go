package durable

// ConversationWindow controls transcript hydration and per-publication trimming.
// Nil bounds mean min=50 and max=2*min. From is inclusive and applies only at
// acquisition. Pointers preserve explicit zero (invalid) versus omitted bounds.
// Bounds use the source safe-integer domain; native DTO limits still apply to
// the actual values.
type ConversationWindow struct {
	From *ID  `json:"from,omitempty"`
	Min  *int `json:"min,omitempty"`
	Max  *int `json:"max,omitempty"`
}

type conversationWindowBounds struct {
	min, max int
	from     ID
	hasFrom  bool
}

func resolveConversationWindow(options []ConversationWindow) (conversationWindowBounds, error) {
	bounds := conversationWindowBounds{min: 50}
	if len(options) > 1 {
		return bounds, reject("at most one conversation window")
	}
	var option ConversationWindow
	if len(options) == 1 {
		option = options[0]
	}
	if option.Min != nil {
		bounds.min = *option.Min
	}
	if bounds.min < 1 || uint64(bounds.min) > MaxID {
		return bounds, reject("conversation window min must be a safe integer >= 1")
	}
	if option.Max == nil {
		// Check the platform integer before multiplying the default max.
		if bounds.min > int(^uint(0)>>1)/2 {
			return bounds, reject("conversation window max overflows integer")
		}
		bounds.max = 2 * bounds.min
	} else {
		bounds.max = *option.Max
	}
	if bounds.max < bounds.min || uint64(bounds.max) > MaxID {
		return bounds, reject("conversation window max must be a safe integer >= min")
	}
	if option.From != nil {
		bounds.from, bounds.hasFrom = *option.From, true
		if uint64(bounds.from) > MaxID {
			return bounds, reject("conversation window from exceeds ID bound")
		}
	}
	return bounds, nil
}

func buildConversationWindow(state Snapshot, id ID, limits Limits, bounds conversationWindowBounds) (ConversationView, error) {
	value, err := buildConversationBase(state, id, limits)
	if err != nil {
		return ConversationView{}, err
	}
	var entries []Entry
	useNewest := !bounds.hasFrom
	if bounds.hasFrom {
		entries, err = visibleHistory(state, EntryQuery{Conversation: id, MinEntryID: bounds.from, Order: Ascending})
		if err != nil {
			return ConversationView{}, err
		}
		if len(entries) <= bounds.max {
			if bounds.from > 1 {
				preceding, err := visibleHistory(state, EntryQuery{Conversation: id, MaxEntryID: bounds.from - 1, Order: Descending})
				if err != nil {
					return ConversationView{}, err
				}
				if len(preceding) > 0 {
					value.Before = preceding[0].ID
				}
			}
		} else {
			useNewest = true
		}
	}
	// A future from cursor can legitimately select no entries.
	if useNewest {
		newest, err := visibleHistory(state, EntryQuery{Conversation: id, Order: Ascending})
		if err != nil {
			return ConversationView{}, err
		}
		start := len(newest) - bounds.min
		if start < 0 {
			start = 0
		}
		if start > 0 {
			value.Before = newest[start-1].ID
		}
		entries = newest[start:]
	}
	value.Entries = make([]Entry, len(entries))
	for i, entry := range entries {
		value.Entries[i], err = copyEntry(entry, limits)
		if err != nil {
			return ConversationView{}, err
		}
	}
	return value, nil
}
