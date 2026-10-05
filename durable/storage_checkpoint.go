package durable

// Storage checkpoints retain every logical record and revision, but replace the
// physical append log. Arrays avoid JSON's non-string map-key limitation.
type storageCheckpoint struct {
	Seq           uint64                `json:"seq"`
	HighWater     uint64                `json:"highWater"`
	Ordinal       uint64                `json:"ordinal"`
	Conversations []Conversation        `json:"conversations"`
	Entries       []Entry               `json:"entries"`
	Tasks         []Task                `json:"tasks"`
	Submissions   []Submission          `json:"submissions"`
	Documents     []Document            `json:"documents"`
	Revisions     []checkpointRevisions `json:"revisions"`
}
type checkpointRevisions struct {
	ID     ID                 `json:"id"`
	Values []DocumentRevision `json:"values"`
}

func checkpointOf(state Snapshot, ordinal uint64) storageCheckpoint {
	value := storageCheckpoint{Seq: state.Seq, HighWater: state.HighWater, Ordinal: ordinal}
	for _, id := range ids(state.Conversations) {
		value.Conversations = append(value.Conversations, state.Conversations[id])
	}
	for _, id := range ids(state.Entries) {
		value.Entries = append(value.Entries, state.Entries[id])
	}
	for _, id := range ids(state.Tasks) {
		value.Tasks = append(value.Tasks, state.Tasks[id])
	}
	for _, id := range ids(state.Submissions) {
		value.Submissions = append(value.Submissions, state.Submissions[id])
	}
	for _, id := range ids(state.Documents) {
		value.Documents = append(value.Documents, state.Documents[id])
	}
	for _, id := range ids(state.DocumentRevisions) {
		value.Revisions = append(value.Revisions, checkpointRevisions{id, state.DocumentRevisions[id]})
	}
	return value
}
func restoreCheckpoint(value storageCheckpoint, limits Limits) (Snapshot, error) {
	if value.Seq > MaxID || value.HighWater < 1 || value.HighWater > MaxID || value.Ordinal < 1 || value.Ordinal > MaxID {
		return Snapshot{}, ErrCorrupt
	}
	state := initialState()
	state.Seq = value.Seq
	state.HighWater = value.HighWater
	state.DocumentRevisions = map[ID][]DocumentRevision{}
	used := map[ID]bool{}
	admit := func(id ID) error {
		if id < 1 || uint64(id) > value.HighWater || used[id] {
			return ErrCorrupt
		}
		used[id] = true
		return nil
	}
	state.Conversations = map[ID]Conversation{}
	for _, record := range value.Conversations {
		if err := admit(record.ID); err != nil {
			return Snapshot{}, err
		}
		state.Conversations[record.ID] = record
	}
	for _, record := range value.Entries {
		if err := admit(record.ID); err != nil {
			return Snapshot{}, err
		}
		if record.Seq == 0 || record.Seq > state.Seq || record.Position == 0 {
			return Snapshot{}, ErrCorrupt
		}
		state.Entries[record.ID] = record
	}
	for _, record := range value.Tasks {
		if err := admit(record.ID); err != nil {
			return Snapshot{}, err
		}
		state.Tasks[record.ID] = record
	}
	for _, record := range value.Submissions {
		if err := admit(record.ID); err != nil {
			return Snapshot{}, err
		}
		state.Submissions[record.ID] = record
	}
	for _, record := range value.Documents {
		if err := admit(record.ID); err != nil {
			return Snapshot{}, err
		}
		state.Documents[record.ID] = record
	}
	for _, record := range value.Revisions {
		if _, exists := state.DocumentRevisions[record.ID]; exists {
			return Snapshot{}, ErrCorrupt
		}
		state.DocumentRevisions[record.ID] = record.Values
	}
	if _, exists := state.Conversations[1]; !exists {
		return Snapshot{}, ErrCorrupt
	}
	if err := validateState(&state, limits, true, Snapshot{}, nil); err != nil {
		return Snapshot{}, ErrCorrupt
	}
	return state, nil
}
