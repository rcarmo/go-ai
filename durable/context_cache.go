package durable

import (
	goai "github.com/rcarmo/go-ai"
	"math"
	"time"
)

// All cache records belong to the Session line and never escape without a deep
// detached DTO copy. Sequence witnesses are process-local, not persisted state.
type contextRange struct {
	seq       uint64
	current   bool // tail was captured as current, not an older as-of request
	tail      ID
	head      ID
	history   []Entry // newest first, retained from the active head's start
	view      ContextView
	settled   []MessageReceipt // ordered messages before the last assistant
	open      []MessageReceipt // original contributions from the last assistant onward
	idleSince *int64
}

func leadContextWithSystem(messages []MessageReceipt) []MessageReceipt {
	for i, m := range messages {
		if m.Role == goai.RoleUser {
			continue
		}
		if i > 0 && m.Role == goai.RoleSystem {
			ordered := make([]MessageReceipt, 0, len(messages))
			ordered = append(ordered, m)
			ordered = append(ordered, messages[:i]...)
			ordered = append(ordered, messages[i+1:]...)
			return ordered
		}
		break
	}
	return messages
}
func detachContextView(v ContextView, l Limits) (ContextView, error) {
	owned := ContextView{}
	if v.Head != nil {
		head, e := copyEntry(*v.Head, l)
		if e != nil {
			return owned, e
		}
		owned.Head = &head
	}
	for _, entry := range v.Entries {
		copy, e := copyEntry(entry, l)
		if e != nil {
			return owned, e
		}
		owned.Entries = append(owned.Entries, copy)
	}
	detach := func(messages []MessageReceipt) ([]MessageReceipt, error) {
		out := make([]MessageReceipt, 0, len(messages))
		for _, m := range messages {
			object, e := dtoObject(m, l)
			if e != nil {
				return nil, e
			}
			var copy MessageReceipt
			if e = fromObject(object, &copy, l); e != nil {
				return nil, e
			}
			out = append(out, copy)
		}
		return out, nil
	}
	for _, part := range v.Contributions {
		copy, e := detach(part)
		if e != nil {
			return owned, e
		}
		owned.Contributions = append(owned.Contributions, copy)
	}
	var e error
	owned.Messages, e = detach(v.Messages)
	return owned, e
}
func contextTail(state Snapshot, conversation, at ID) (ID, error) {
	if _, ok := state.Conversations[conversation]; !ok {
		return 0, reject("unknown conversation")
	}
	if at != 0 {
		if !entryVisible(state, conversation, at) {
			return 0, reject("context cutoff not visible")
		}
		return at, nil
	}
	// Newest fork segment first, without sorting or copying the full history.
	upper := ID(MaxID)
	for conversation != 0 {
		var tail ID
		for id, entry := range state.Entries {
			if entry.Conversation == conversation && id <= upper && id > tail {
				tail = id
			}
		}
		if tail != 0 {
			return tail, nil
		}
		record := state.Conversations[conversation]
		upper = min(upper, record.ParentAt)
		conversation = record.Parent
	}
	return 0, nil
}
func (s *Session) contextBusy(conversation ID) bool {
	if s.taskScheduler == nil {
		return false
	}
	<-s.core.line
	defer s.core.leave()
	for _, task := range s.core.state.Tasks {
		if task.Conversation == conversation && !terminalStatus(task.Status) && !taskBackground(task) {
			return true
		}
	}
	return false
}
func (s *Session) contextRetention() int64 {
	if s.taskScheduler == nil {
		return 600000
	}
	settings := s.taskScheduler.h.resolvedSettings(RequestSettings{})
	if settings.ContextRetentionMs == nil {
		return 600000
	}
	return *settings.ContextRetentionMs
}
func (s *Session) expireContextRanges() {
	if len(s.contextRanges) == 0 {
		if s.contextExpiry != nil {
			s.contextExpiry.Stop()
			s.contextExpiry = nil
		}
		return
	}
	retention := s.contextRetention()
	now := s.now()
	for id, r := range s.contextRanges {
		if s.contextBusy(id) {
			r.idleSince = nil
			continue
		}
		if r.idleSince == nil {
			stamp := now
			r.idleSince = &stamp
		}
		if retention <= 0 || now >= *r.idleSince && uint64(now)-uint64(*r.idleSince) >= uint64(retention) {
			delete(s.contextRanges, id)
		}
	}
	if s.contextExpiry != nil {
		s.contextExpiry.Stop()
		s.contextExpiry = nil
	}
	var delay time.Duration
	for _, r := range s.contextRanges {
		if r.idleSince == nil {
			continue
		}
		elapsed := int64(0)
		if now >= *r.idleSince {
			difference := uint64(now) - uint64(*r.idleSince)
			if difference > uint64(math.MaxInt64) {
				difference = uint64(math.MaxInt64)
			}
			elapsed = int64(difference)
		}
		left := retentionDuration(max(int64(0), retention-elapsed))
		if left <= 0 {
			left = time.Millisecond
		}
		if delay == 0 || left < delay {
			delay = left
		}
	}
	if delay > 0 && !s.closing.Load() {
		s.contextExpiry = time.AfterFunc(delay, func() {
			s.taskBookkeeping(func() {
				if !s.closing.Load() {
					s.expireContextRanges()
				}
			})
		})
	}
}
func retentionDuration(ms int64) time.Duration {
	if ms > math.MaxInt64/int64(time.Millisecond) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(ms) * time.Millisecond
}
func (s *Session) cachedContextView(state Snapshot, conversation, at ID) (ContextView, error) {
	s.expireContextRanges()
	if kept := s.contextRanges[conversation]; kept != nil && kept.seq == state.Seq && (at == 0 && kept.current || at != 0 && at == kept.tail) {
		return detachContextView(kept.view, s.limits)
	}
	tail, e := contextTail(state, conversation, at)
	if e != nil {
		return ContextView{}, e
	}
	if tail == 0 {
		return ContextView{}, nil
	}
	previous := s.contextRanges[conversation]
	var history []Entry
	var view ContextView
	var settled, open []MessageReceipt
	if previous != nil && tail == previous.tail {
		previous.seq = state.Seq
		previous.current = at == 0 || previous.current
		return detachContextView(previous.view, s.limits)
	}
	// Older bounds are selected from a cached range when its head still covers
	// the cutoff; never replace a newer cache with a historical read.
	if previous != nil && tail < previous.tail && (previous.head == 0 || tail >= previous.head) {
		for _, entry := range previous.history {
			if entry.ID <= tail {
				history = append(history, entry)
			}
		}
	} else if previous != nil && tail > previous.tail {
		added, err := visibleHistory(state, EntryQuery{Conversation: conversation, MinEntryID: previous.tail + 1, MaxEntryID: tail})
		if err != nil {
			return ContextView{}, err
		}
		simple := true
		for _, entry := range added {
			if entry.Head != 0 || len(entry.Edits) > 0 {
				simple = false
				break
			}
		}
		if simple {
			history = append(added, previous.history...)
			// Derive added contributions only; reorder the final open message suffix
			// so later tool results can replace synthesized missing-result receipts.
			view, settled, open, e = s.extendContextRange(previous, added)
			if e != nil {
				return ContextView{}, e
			}
		}
	}
	if history == nil {
		history, e = visibleHistory(state, EntryQuery{Conversation: conversation, MaxEntryID: tail})
		if e != nil {
			return ContextView{}, e
		}
	}
	if view.Entries == nil {
		view, e = deriveContextHistory(history, s.limits)
		if e != nil {
			return ContextView{}, e
		}
	}
	if settled == nil && open == nil {
		var raw []MessageReceipt
		for _, part := range view.Contributions {
			raw = append(raw, part...)
		}
		settled, open = settleContext(nil, raw)
	}
	head := ID(0)
	if view.Head != nil {
		head = view.Head.ID
		var active []Entry
		for _, entry := range history {
			if entry.ID >= view.Head.Head {
				active = append(active, entry)
			}
		}
		history = active
	}
	if previous == nil || tail >= previous.tail {
		kept := &contextRange{seq: state.Seq, current: at == 0, tail: tail, head: head, history: history, view: view, settled: settled, open: open}
		if previous != nil {
			kept.idleSince = previous.idleSince
		}
		if s.contextRanges == nil {
			s.contextRanges = map[ID]*contextRange{}
		}
		s.contextRanges[conversation] = kept
		s.expireContextRanges()
	}
	return detachContextView(view, s.limits)
}

// Invalidate on out-of-order appends/fork changes. Plain newer appends keep the
// range so the next read derives only their contributions.
func (s *Session) invalidateContextRanges(state Snapshot, writes []Write) {
	for id, r := range s.contextRanges {
		for _, w := range writes {
			if w.Entry != nil && w.Entry.ID <= r.tail && entryVisible(state, id, w.Entry.ID) || w.Conversation != nil && w.Conversation.ID == id {
				delete(s.contextRanges, id)
				break
			}
		}
	}
}
func settleContext(settled, open []MessageReceipt) ([]MessageReceipt, []MessageReceipt) {
	last := -1
	for i, m := range open {
		if m.Role == goai.RoleAssistant {
			last = i
		}
	}
	if last > 0 {
		settled = append(append([]MessageReceipt(nil), settled...), orderContextToolResults(open[:last])...)
		open = open[last:]
	}
	return settled, open
}
func (s *Session) extendContextRange(previous *contextRange, added []Entry) (ContextView, []MessageReceipt, []MessageReceipt, error) {
	v := previous.view
	v.Entries = append([]Entry(nil), v.Entries...)
	v.Contributions = append([][]MessageReceipt(nil), v.Contributions...)
	open := append([]MessageReceipt(nil), previous.open...)
	for i := len(added) - 1; i >= 0; i-- {
		entry := added[i]
		contribution, err := deriveContextHistory([]Entry{entry}, s.limits)
		if err != nil {
			return ContextView{}, nil, nil, err
		}
		for _, part := range contribution.Contributions {
			open = append(open, part...)
		}
		v.Entries = append(v.Entries, contribution.Entries...)
		v.Contributions = append(v.Contributions, contribution.Contributions...)
	}
	// Reorder only the open suffix; old settled assistants/results do not need
	// repeated pairing. Original contributions exclude synthetic placeholders.
	settled, open := settleContext(previous.settled, open)
	v.Messages = leadContextWithSystem(append(append([]MessageReceipt(nil), settled...), orderContextToolResults(open)...))
	return v, settled, open, nil
}
