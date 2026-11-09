package durable

// Port of transaction.writeKeys() from pinned v1.5.0 src/session/transaction.ts.
// These are the keys a commit writes, taken once the transaction is settled (and
// after held changes were selected), for the Session's unpublished write set:
// its table records, and the address and incarnation of every document it
// creates, changes, or retires, with the scope of every incarnation it creates
// or retires.
//
// The writer has not been installed on Session yet, so nothing gates on these
// keys; see notes/writer-install-crosswalk.md. No D01/D02/D04 parity claim.

// writeKeys returns the keys this transaction writes. It must run after the
// transaction is sealed and its document writes are finalized, because a delta
// write carries only its incarnation: the address comes from the state the
// transaction settled against.
func (t *Tx) writeKeys() ([]string, error) {
	keys := tableKeys(t.writes, nil)
	for _, write := range t.writes {
		switch {
		case write.Document != nil:
			document := write.Document
			keys = append(keys, documentKey(documentAddress(*document)), incarnationKey(document.ID))
			// Creating or retiring an incarnation changes which incarnations a
			// scope scan sees. Retirement is signalled by the op: the staged record
			// is the CURRENT one, whose Retired flag storage has not yet applied.
			// Creation is decided against the state the transaction started from,
			// because native fills CreatedAt during finalisation.
			_, existed := t.state.Documents[document.ID]
			if write.Op == "retire-document" || !existed {
				keys = append(keys, scopeKey(document.Scope, document.Owner))
			}
		case write.Delta != nil:
			// A delta changes an existing incarnation's content, never which
			// incarnation a scope scan sees, so no scope key is written.
			address, ok := t.incarnationAddress(write.Delta.ID)
			if !ok {
				return nil, reject("delta write has no incarnation address")
			}
			keys = append(keys, documentKey(address), incarnationKey(write.Delta.ID))
		}
	}
	return keys, nil
}

// incarnationAddress resolves the address an incarnation was written at. An
// incarnation created by this transaction is keyed from its own write above, so
// the settled state is the authority for the address of an existing one.
func (t *Tx) incarnationAddress(id ID) (DocumentAddress, bool) {
	state := t.state
	if len(t.writes) > 0 {
		if current, err := t.current(); err == nil {
			state = current
		}
	}
	if document, ok := state.Documents[id]; ok {
		return documentAddress(document), true
	}
	return DocumentAddress{}, false
}
