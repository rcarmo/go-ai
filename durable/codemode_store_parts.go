package durable

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	codemodeStoreItemKind   = "pi.codemode.store.item"
	codemodeStoreStartKind  = "pi.codemode.store.start"
	codemodeStoreResultKind = "pi.codemode.store.result"
	codemodeStoreItemsV1    = "items-v1"
)

func codemodeStoreItemKey(key string) string {
	return fmt.Sprintf("key-%x", sha256.Sum256([]byte(key)))
}

// The guest limits the sum of each value's JSON UTF-16 length, not
// just this run's writes. Enforce it on the merged authoritative host store:
// an untrusted guest need not have seen (or honoured) the current snapshot.
func validateCodemodeLogicalStore(values JSON, l Limits) error {
	const storedValueUnits = 1 << 20 // MAX_STORE_TOTAL_CHARS, prelude-source.ts.
	if len(values) > l.MaxMembers {
		return reject("codemode store member limit")
	}
	remaining := storedValueUnits
	bytes := min(l.MaxFramePayloadBytes, DefaultLimits().MaxFramePayloadBytes) - 2
	for key, value := range values {
		wire, err := encodeBounded(value, l, l.MaxDocumentBytes)
		if err != nil {
			return err
		}
		name, err := encodeBounded(key, l, l.MaxDocumentBytes)
		if err != nil {
			return err
		}
		bytes -= len(name) + len(wire) + 2
		if bytes < 0 {
			return reject("codemode logical store exceeds frame limit")
		}
		for len(wire) > 0 {
			r, size := utf8.DecodeRune(wire)
			remaining--
			if r > 0xffff {
				remaining--
			}
			if remaining < 0 {
				return reject("codemode logical store exceeds retained value limit")
			}
			wire = wire[size:]
		}
	}
	return nil
}

// The legacy map remains the only value on the small path. An atomic
// transition replaces it with a marker and creates all the as-of items.
func codemodeStoreValues(tx *Tx, owner ID, store *DocumentHandle) (JSON, map[string]ID, error) {
	stored, err := store.Get()
	if err != nil {
		return nil, nil, err
	}
	if stored["layout"] == nil {
		values, ok := stored["values"].(map[string]any)
		if !ok {
			return nil, nil, reject("codemode store shape")
		}
		return JSON(values), nil, nil
	}
	if len(stored) != 1 || stored["layout"] != codemodeStoreItemsV1 {
		return nil, nil, reject("codemode store layout")
	}
	values := JSON{}
	items := map[string]ID{}
	for _, d := range tx.state.Documents {
		if d.Scope != "conversation" || d.Owner != owner || d.Kind != codemodeStoreItemKind || d.Retired {
			continue
		}
		if len(items) >= tx.limits.MaxMembers || !d.Family || d.Version != 1 || d.History != "rewindable" || d.Fork != "asOf" {
			return nil, nil, reject("invalid codemode store item")
		}
		h, err := tx.documentRecord(d.ID)
		if err != nil {
			return nil, nil, err
		}
		v, err := h.Get()
		if err != nil {
			return nil, nil, err
		}
		key, ok := v["key"].(string)
		value, present := v["value"]
		if !ok || !present || len(v) != 2 || d.Key != codemodeStoreItemKey(key) || items[key] != 0 {
			return nil, nil, reject("invalid codemode store item")
		}
		values[key], items[key] = value, d.ID
	}
	return values, items, nil
}

func applyCodemodeStore(tx *Tx, owner ID, result CodemodeResult) error {
	store, err := tx.AcquireDocument(codemodeStoreDefinition(), owner, nil, nil)
	if err != nil {
		return err
	}
	values, items, err := codemodeStoreValues(tx, owner, store)
	if err != nil {
		return err
	}
	writes := result.StoreWrites
	for _, key := range result.StoreDeletes {
		delete(values, key)
	}
	maps.Copy(values, writes)
	if err := validateCodemodeLogicalStore(values, tx.limits); err != nil {
		return err
	}
	if items == nil {
		inline := JSON{"values": values}
		if _, err := encodeBounded(inline, tx.limits, tx.limits.MaxDocumentBytes); err == nil {
			return store.Set(inline)
		} else if !strings.Contains(err.Error(), "encoded JSON byte limit") {
			return err
		}
		writes = values // A single atomic split includes retained legacy keys.
	}
	// Never accept a part count that cannot be captured by a later start
	// together with its run and journal even before considering their bytes.
	if len(values) > tx.limits.MaxPage || len(values) > tx.limits.MaxWrites-2 {
		return reject("codemode logical store part count")
	}
	if items == nil {
		if err := store.Set(JSON{"layout": codemodeStoreItemsV1}); err != nil {
			return err
		}
	}
	for _, key := range result.StoreDeletes {
		if id := items[key]; id != 0 {
			h, err := tx.documentRecord(id)
			if err != nil {
				return err
			}
			if err := h.Retire(); err != nil {
				return err
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(writes)) {
		value := JSON{"key": key, "value": writes[key]}
		if id := items[key]; id != 0 {
			h, err := tx.documentRecord(id)
			if err != nil {
				return err
			}
			if err := h.Set(value); err != nil {
				return err
			}
			continue
		}
		id, err := tx.mintID()
		if err != nil {
			return err
		}
		if _, err := tx.createDocumentRecord(Document{ID: id, Scope: "conversation", Owner: owner, Kind: codemodeStoreItemKind, History: "rewindable", Fork: "asOf", Family: true, Key: codemodeStoreItemKey(key), Version: 1, Value: value}); err != nil {
			return err
		}
	}
	return nil
}

func createCodemodeStoreParts(tx *Tx, owner ID, key, kind string, values JSON) ([]ID, error) {
	if len(values) > tx.limits.MaxPage {
		return nil, reject("codemode store part count")
	}
	refs := make([]ID, 0, len(values))
	for i, name := range slices.Sorted(maps.Keys(values)) {
		id, err := tx.mintID()
		if err != nil {
			return nil, err
		}
		value := JSON{"key": name, "value": values[name]}
		address := fmt.Sprintf("%s/%d", key, i)
		if _, err := tx.createDocumentRecord(Document{ID: id, Scope: "task", Owner: owner, Kind: kind, Family: true, Key: address, Version: 1, Value: value}); err != nil {
			return nil, err
		}
		refs = append(refs, id)
	}
	return refs, nil
}

func readCodemodeStoreParts(s Snapshot, owner ID, key, kind string, refs []ID, l Limits) (JSON, error) {
	if len(refs) > l.MaxPage {
		return nil, reject("codemode store part count")
	}
	values := make(JSON, len(refs))
	for i, id := range refs {
		d, ok := s.Documents[id]
		if !ok || d.Retired || d.Scope != "task" || d.Owner != owner || d.Kind != kind || !d.Family || d.Version != 1 || d.Key != fmt.Sprintf("%s/%d", key, i) {
			return nil, reject("invalid codemode store part reference")
		}
		name, ok := d.Value["key"].(string)
		value, present := d.Value["value"]
		if _, repeated := values[name]; !ok || !present || len(d.Value) != 2 || repeated {
			return nil, reject("invalid codemode store part")
		}
		values[name] = value
	}
	return values, nil
}

// Storage must reject orphaned parts and forbid their retirement before the
// owner terminates, even when writes did not originate from TaskRuntime.
func validateCodemodeStorePart(s Snapshot, doc Document, l Limits) error {
	key, ordinal, ok := strings.Cut(doc.Key, "/")
	index, err := strconv.Atoi(ordinal)
	if !ok || !validNestedKey(key) || err != nil || index < 0 || index >= l.MaxPage || strconv.Itoa(index) != ordinal || doc.Scope != "task" || !doc.Family || (doc.Version != 1 && !documentMetadataOnly(doc)) {
		return reject("invalid codemode store part address")
	}
	task, exists := s.Tasks[doc.Owner]
	if !exists || doc.Retired != terminalStatus(task.Status) {
		return reject("invalid codemode store part retirement")
	}
	if doc.Retired {
		return nil
	}
	for _, d := range s.Documents {
		if d.Scope != "task" || d.Owner != doc.Owner || !d.Family || d.Key != key || d.Kind != codemodeRunKind || d.Retired {
			continue
		}
		var run codemodeRunState
		if err := fromObject(d.Value, &run, l); err != nil {
			return err
		}
		refs := run.StoreStartParts
		if doc.Kind == codemodeStoreResultKind {
			refs = run.StoreResultParts
		}
		if index < len(refs) && refs[index] == doc.ID {
			return nil
		}
	}
	return reject("orphan codemode store part")
}
