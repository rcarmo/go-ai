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
	for i, delivery := range journal.Deliveries {
		if delivery.Seq != i+1 || delivery.Call.ID != i+1 || delivery.Call.Name != journal.Issued[i].Name || delivery.Call.Global != journal.Issued[i].Global || !equalJSONValue(delivery.Call.Arguments, journal.Issued[i].Arguments) || delivery.HasValue && delivery.Error != "" || !delivery.HasValue && delivery.Value != nil {
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
	if run.Result != nil && len(journal.Issued) != len(journal.Deliveries) {
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
	for i, call := range a.Issued {
		other := b.Issued[i]
		if call.ID != other.ID || call.Name != other.Name || call.Global != other.Global || !equalJSONValue(call.Arguments, other.Arguments) {
			return reject("codemode issued prefix changed")
		}
	}
	for i, delivery := range a.Deliveries {
		left, err := dtoObject(delivery, l)
		if err != nil {
			return err
		}
		right, err := dtoObject(b.Deliveries[i], l)
		if err != nil {
			return err
		}
		if !equalJSONValue(left, right) {
			return reject("codemode delivery prefix changed")
		}
	}
	return nil
}
