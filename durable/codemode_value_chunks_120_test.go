package durable

import "testing"

func TestCodemode120StringChunkOwnershipAndIntegrity(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "foreign", "retired", "wrong-key", "duplicate", "empty"} {
		t.Run(mode, func(t *testing.T) {
			s := Snapshot{Documents: map[ID]Document{3: {ID: 3, Scope: "task", Owner: 2, Kind: codemodeImageChunkKind, Family: true, Version: 1, Key: "run/value/0", Value: JSON{"data": "hello"}}}}
			ids := []ID{3}
			d := s.Documents[3]
			switch mode {
			case "missing":
				delete(s.Documents, 3)
			case "foreign":
				d.Owner = 9
			case "retired":
				d.Retired = true
			case "wrong-key":
				d.Key = "other/value/0"
			case "duplicate":
				ids = append(ids, 3)
			case "empty":
				d.Value = JSON{"data": ""}
			}
			if mode != "missing" {
				s.Documents[3] = d
			}
			value, err := readCodemodeStringValue(s, 2, "run", ids, DefaultLimits())
			if mode == "valid" {
				if err != nil || value != "hello" {
					t.Fatal(value, err)
				}
			} else if err == nil {
				t.Fatal("invalid chunks accepted", mode)
			}
		})
	}
}
