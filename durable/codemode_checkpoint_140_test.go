package durable

import "testing"

func TestCodemode140StoreCheckpointThresholdAndHistory(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, reopen func() *Session) {
		var id ID
		definition := codemodeStoreDefinition()
		checkpointCommit(t, s, func(tx *Tx) error {
			doc, err := tx.AcquireDocument(definition, 1, nil, nil)
			if err == nil {
				id = doc.id
			}
			return err
		})
		var historical uint64
		for n := 1; n <= 65; n++ {
			checkpointCommit(t, s, func(tx *Tx) error {
				doc, err := tx.AcquireDocument(definition, 1, nil, nil)
				if err != nil {
					return err
				}
				return doc.ApplyOperations([]Operation{{"s", []any{"values", "n"}, n}})
			})
			snapshot := checkpointSnapshot(t, s)
			if n == 32 {
				historical = snapshot.Seq
			}
			if n == 64 && snapshot.Documents[id].DeltasSinceBase != 64 {
				t.Fatal("checkpoint before threshold", snapshot.Documents[id])
			}
			if n == 65 && snapshot.Documents[id].DeltasSinceBase != 0 {
				t.Fatal("checkpoint not selected", snapshot.Documents[id])
			}
		}
		s = reopen()
		past, ok, err := s.store.(DocumentHistoryStorage).DocumentAt(bg, id, DocumentPoint{Seq: historical})
		if err != nil || !ok || !equalJSONValue(past.Value, JSON{"values": JSON{"n": 32}}) {
			t.Fatal(past, ok, err)
		}
		current, ok, err := s.store.(DocumentHistoryStorage).DocumentAt(bg, id, CurrentDocumentPoint())
		if err != nil || !ok || !equalJSONValue(current.Value, JSON{"values": JSON{"n": 65}}) || current.DeltasSinceBase != 0 {
			t.Fatal(current, ok, err)
		}
	})
}
