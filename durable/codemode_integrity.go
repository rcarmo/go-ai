package durable

func validateCodemodeDocuments(s Snapshot, doc Document, l Limits) error {
	if doc.Scope != "task" || !doc.Family || doc.Version != 1 || !validNestedKey(doc.Key) {
		return reject("invalid codemode document identity")
	}
	var runDoc, journalDoc *Document
	for _, candidate := range s.Documents {
		if candidate.Scope == doc.Scope && candidate.Owner == doc.Owner && candidate.Key == doc.Key && candidate.Family {
			copy := candidate
			switch candidate.Kind {
			case codemodeRunKind:
				runDoc = &copy
			case codemodeJournalKind:
				journalDoc = &copy
			}
		}
	}
	if runDoc == nil || journalDoc == nil || runDoc.Retired != journalDoc.Retired {
		return reject("codemode run/journal pair mismatch")
	}
	if doc.Retired && !terminalStatus(s.Tasks[doc.Owner].Status) {
		return reject("codemode document retired before caller")
	}
	var run codemodeRunState
	var journal codemodeJournalState
	if err := fromObject(runDoc.Value, &run, l); err != nil {
		return err
	}
	if err := fromObject(journalDoc.Value, &journal, l); err != nil {
		return err
	}
	if run.Start.InterruptBudget < 0 || run.Start.InterruptBudget > 1000000 || run.Start.Version != 2 || run.Start.Deadline <= run.Start.StartedAt || run.Start.Deadline-run.Start.StartedAt > 2147483647 || run.Start.MaxOutputTokens < 0 || uint64(run.Start.MaxOutputTokens) > MaxID || run.Start.Seed > 0xffffffff || len(run.Source) > l.MaxStringBytes || len(journal.Issued) > l.MaxPage || len(journal.Deliveries) > len(journal.Issued) {
		return reject("invalid codemode start/journal limits")
	}
	if run.Start.ParallelCalls && !run.Start.DrainAcknowledgements {
		return reject("parallel codemode requires drain admission")
	}
	if err := validateCodemodeWatermarks(run.Start, journal, run.Result != nil && (!run.Start.ParallelCalls || run.Result.Error == "")); err != nil {
		return err
	}
	if err := validateCodemodeGlobalDescriptions(run.Start.Globals, l); err != nil {
		return err
	}
	if err := validateCodemodeNamespaces(run.Start.Namespaces, l); err != nil {
		return err
	}
	tools := map[string]bool{}
	for _, tool := range run.Start.Tools {
		if tools[tool.Name] || tool.Identifier != CodemodeIdentifier(tool.Name) {
			return reject("invalid codemode tool snapshot")
		}
		tools[tool.Name] = true
	}
	for i, call := range journal.Issued {
		if call.ID != i+1 || (!call.Global && !tools[call.Name]) || (call.Global && !codemodeGlobalAllowed(run.Start, call.Name)) || call.Arguments == nil {
			return reject("invalid codemode issued sequence/authority")
		}
	}
	seenDeliveries := map[int]bool{}
	for i, delivery := range journal.Deliveries {
		id := delivery.Call.ID
		if id < 1 || id > len(journal.Issued) || seenDeliveries[id] || !run.Start.ParallelCalls && id != i+1 {
			return reject("invalid codemode delivery identity")
		}
		seenDeliveries[id] = true
		call := journal.Issued[id-1]
		if delivery.Seq != i+1 || delivery.Call.Name != call.Name || delivery.Call.Global != call.Global || !equalJSONValue(delivery.Call.Arguments, call.Arguments) || delivery.HasValue && delivery.Error != "" || !delivery.HasValue && delivery.Value != nil {
			return reject("invalid codemode delivery sequence/union")
		}
	}

	if (run.OutputDir == "") != (run.OutputFS == "") || len(run.OutputDir) > l.MaxStringBytes || len(run.OutputFS) > l.MaxStringBytes {
		return reject("invalid codemode output address")
	}
	if run.Result != nil {
		if err := validateCodemodeResult(*run.Result, l); err != nil {
			return err
		}
	}
	if run.Result != nil && !run.Start.ParallelCalls && len(journal.Issued) != len(journal.Deliveries) {
		return reject("codemode result before delivered calls")
	}
	return nil
}
func validateCodemodeReplacement(old, next Document, l Limits) error {
	if old.Kind == codemodeRunKind {
		var a, b codemodeRunState
		if err := fromObject(old.Value, &a, l); err != nil {
			return err
		}
		if err := fromObject(next.Value, &b, l); err != nil {
			return err
		}
		aStart, err := dtoObject(a.Start, l)
		if err != nil {
			return err
		}
		bStart, err := dtoObject(b.Start, l)
		if err != nil {
			return err
		}
		if a.Source != b.Source || !equalJSONValue(aStart, bStart) {
			return reject("codemode start identity changed")
		}
		if a.OutputDir != "" && (a.OutputDir != b.OutputDir || a.OutputFS != b.OutputFS) {
			return reject("codemode output address changed")
		}
		if a.Result != nil {
			av, err := dtoObject(a.Result, l)
			if err != nil {
				return err
			}
			bv, err := dtoObject(b.Result, l)
			if err != nil {
				return err
			}
			if !equalJSONValue(av, bv) {
				return reject("settled codemode result changed")
			}
		}
		return nil
	}
	var a, b codemodeJournalState
	if err := fromObject(old.Value, &a, l); err != nil {
		return err
	}
	if err := fromObject(next.Value, &b, l); err != nil {
		return err
	}
	if len(b.Issued) < len(a.Issued) || len(b.Deliveries) < len(a.Deliveries) {
		return reject("codemode journal truncated")
	}
	if a.FirstIssued != nil && (b.FirstIssued == nil || *a.FirstIssued != *b.FirstIssued) {
		return reject("codemode initial watermark changed")
	}
	for i, call := range a.Issued {
		other := b.Issued[i]
		if call.ID != other.ID || call.Name != other.Name || call.Global != other.Global || !equalJSONValue(call.Arguments, other.Arguments) {
			return reject("codemode issued prefix changed")
		}
	}
	for i, delivery := range a.Deliveries {
		other := b.Deliveries[i]
		if delivery.IssuedAfter != nil && (other.IssuedAfter == nil || *delivery.IssuedAfter != *other.IssuedAfter) {
			return reject("codemode delivery watermark changed")
		}
		// A previously unacknowledged delivery may acquire its count once.
		delivery.IssuedAfter, other.IssuedAfter = nil, nil
		left, err := dtoObject(delivery, l)
		if err != nil {
			return err
		}
		right, err := dtoObject(other, l)
		if err != nil {
			return err
		}
		if !equalJSONValue(left, right) {
			return reject("codemode delivery prefix changed")
		}
	}
	return nil
}

// A nil count is an unacknowledged drain; zero is an acknowledged empty drain.
func validateCodemodeWatermarks(start CodemodeStart, journal codemodeJournalState, settled bool) error {
	if !start.DrainAcknowledgements {
		if journal.FirstIssued != nil {
			return reject("codemode watermark without drain protocol")
		}
		for _, delivery := range journal.Deliveries {
			if delivery.IssuedAfter != nil {
				return reject("codemode watermark without drain protocol")
			}
		}
		return nil
	}
	if journal.FirstIssued == nil {
		if len(journal.Deliveries) > 0 || settled {
			return reject("codemode initial watermark missing")
		}
		return nil
	}
	count := *journal.FirstIssued
	if count < 0 || count > len(journal.Issued) {
		return reject("invalid codemode initial watermark")
	}
	for i, delivery := range journal.Deliveries {
		if delivery.Call.ID < 1 || delivery.Call.ID > count {
			return reject("codemode delivery before admitted call")
		}
		if delivery.IssuedAfter == nil {
			if i != len(journal.Deliveries)-1 || settled {
				return reject("codemode delivery watermark missing")
			}
			return nil
		}
		next := *delivery.IssuedAfter
		if next < count || next > len(journal.Issued) {
			return reject("invalid codemode delivery watermark")
		}
		count = next
	}
	if settled && count != len(journal.Issued) {
		return reject("codemode result before acknowledged calls")
	}
	return nil
}
