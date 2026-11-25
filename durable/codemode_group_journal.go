package durable

const (
	codemodeGroupedJournalVersion = 2
	codemodeJournalBatchLimit     = 16
)

// Terminal is guest position when present, not host completion. A worker
// stop does not report a position; Delivered is absent in that case. The
// committed suffix stays in the journal in either case.
type codemodeTerminal struct {
	Delivered     *int `json:"delivered,omitempty"`
	WorkerStopped bool `json:"workerStopped,omitempty"`
	Issued        int  `json:"issued"`
	Committed     int  `json:"committed"`
}

func codemodeTerminalPosition(delivered, issued, committed int) *codemodeTerminal {
	return &codemodeTerminal{Delivered: &delivered, Issued: issued, Committed: committed}
}

func sameCodemodeTerminal(a, b *codemodeTerminal) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.WorkerStopped != b.WorkerStopped || a.Issued != b.Issued || a.Committed != b.Committed {
		return false
	}
	if a.Delivered == nil || b.Delivered == nil {
		return a.Delivered == b.Delivered
	}
	return *a.Delivered == *b.Delivered
}

func validateGroupedCodemodeJournal(start CodemodeStart, journal codemodeJournalState, terminal *codemodeTerminal, settled bool) error {
	if start.JournalVersion != codemodeGroupedJournalVersion || !start.ParallelCalls || !start.DrainAcknowledgements || journal.Version != start.JournalVersion {
		return reject("invalid codemode group journal version")
	}
	if (terminal != nil) != settled {
		return reject("codemode terminal position must seal result")
	}
	count := 0
	if journal.FirstIssued != nil {
		count = *journal.FirstIssued
		if count < 0 || count > len(journal.Issued) {
			return reject("invalid codemode initial watermark")
		}
	}
	begin := 0
	for _, end := range journal.BatchEnds {
		if end <= begin || end > len(journal.Deliveries) || end-begin > codemodeJournalBatchLimit {
			return reject("invalid codemode batch extent")
		}
		if journal.FirstIssued == nil || begin > 0 && journal.Deliveries[begin-1].IssuedAfter == nil {
			return reject("codemode batch lacks preceding durable count")
		}
		opening := count
		for _, delivery := range journal.Deliveries[begin:end] {
			if delivery.Call.ID < 1 || delivery.Call.ID > opening {
				return reject("codemode batch delivery lacks opening admission")
			}
			if delivery.IssuedAfter != nil {
				next := *delivery.IssuedAfter
				if next < count || next > len(journal.Issued) {
					return reject("invalid codemode batch watermark")
				}
				count = next
			}
		}
		begin = end
	}
	if begin != len(journal.Deliveries) {
		return reject("codemode delivery missing batch extent")
	}
	if !settled {
		if count != len(journal.Issued) {
			return reject("codemode batch contains unadmitted issued calls")
		}
		return nil
	}
	if terminal.Committed != len(journal.Deliveries) || terminal.Issued != len(journal.Issued) {
		return reject("invalid codemode terminal extent")
	}
	if terminal.WorkerStopped {
		if terminal.Delivered != nil {
			return reject("codemode worker stop claims a guest position")
		}
		return nil
	}
	if terminal.Delivered == nil || *terminal.Delivered < 0 || *terminal.Delivered > len(journal.Deliveries) {
		return reject("invalid codemode terminal extent")
	}
	delivered := *terminal.Delivered
	if delivered == 0 {
		if journal.FirstIssued != nil || len(journal.Deliveries) != 0 {
			return reject("codemode ended before durable first idle")
		}
	} else {
		for _, delivery := range journal.Deliveries[delivered-1:] {
			if delivery.IssuedAfter != nil {
				return reject("codemode ended before durable continuation")
			}
		}
	}
	return nil
}

// Replacements before terminal may fill a missing count once. Once a result and
// its extent have been sealed, neither put nor delta may change any outcome or
// add a count to an outcome that the guest never acknowledged.
func validateGroupedCodemodeJournalSeal(s Snapshot, old, next Document, l Limits) error {
	if old.Kind != codemodeJournalKind || old.Value["journalVersion"] == nil || equalJSONValue(old.Value, next.Value) {
		return nil
	}
	for _, doc := range s.Documents {
		if doc.Kind == codemodeRunKind && doc.Scope == old.Scope && doc.Owner == old.Owner && doc.Key == old.Key && doc.Family == old.Family {
			var run codemodeRunState
			if err := fromObject(doc.Value, &run, l); err != nil {
				return err
			}
			if run.Start.JournalVersion == codemodeGroupedJournalVersion && run.Result != nil {
				return reject("sealed codemode journal changed")
			}
			return nil
		}
	}
	return reject("codemode group journal lacks run")
}
