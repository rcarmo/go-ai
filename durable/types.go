package durable

import (
	"context"
	"errors"
	"fmt"
)

// MaxID is the exact-integer ceiling shared with upstream JavaScript records.
const MaxID uint64 = 1<<53 - 1

type ID uint64
type ConversationID = ID
type EntryID = ID
type TaskID = ID
type SubmissionID = ID
type DocumentID = ID

type JSON map[string]any

var (
	ErrClosed           = errors.New("durable: closed")
	ErrPoisoned         = errors.New("durable: uncertain storage outcome; close and reopen required")
	ErrCorrupt          = errors.New("durable: corrupt journal")
	ErrUnsupported      = errors.New("durable: unsupported operation")
	ErrConversationBusy = errors.New("durable: conversation busy")
	ErrSealed           = errors.New("durable: transaction sealed")
	ErrConcurrent       = errors.New("durable: overlapping or reentrant transaction method")
	ErrOwned            = errors.New("durable: storage already owned in this process")
)

// StorageRejected establishes that no write was admitted for this operation.
// Post-admission I/O errors instead poison storage; a failed return never proves
// that such an uncertain frame will be absent after reopen.
type StorageRejected struct{ Reason string }

func (e *StorageRejected) Error() string { return "durable: rejected: " + e.Reason }
func reject(reason string) error         { return &StorageRejected{Reason: reason} }

// Limits are persisted at journal creation. Zero-valued options select defaults;
// explicit options on reopen must exactly match the persisted policy.
type Limits struct {
	MaxFramePayloadBytes int   `json:"maxFramePayloadBytes"`
	MaxRecordBytes       int   `json:"maxRecordBytes"`
	MaxDocumentBytes     int   `json:"maxDocumentBytes"`
	MaxStringBytes       int   `json:"maxStringBytes"`
	MaxRequestIDBytes    int   `json:"maxRequestIDBytes"`
	MaxDepth             int   `json:"maxDepth"`
	MaxMembers           int   `json:"maxMembers"`
	MaxNodes             int   `json:"maxNodes"`
	MaxWrites            int   `json:"maxWrites"`
	MaxPage              int   `json:"maxPage"`
	MaxRecords           int   `json:"maxRecords"`
	MaxRetainedBytes     int64 `json:"maxRetainedBytes"`
	MaxJournalBytes      int64 `json:"maxJournalBytes"`
}

func DefaultLimits() Limits {
	return Limits{4 << 20, 512 << 10, 256 << 10, 256 << 10, 512, 32, 4096, 65536, 256, 256, 100000, 32 << 20, 256 << 20}
}
func (l Limits) validate() error {
	max := Limits{16 << 20, 1 << 20, 1 << 20, 512 << 10, 4096, 64, 16384, 262144, 1024, 4096, 1000000, 128 << 20, 4 << 30}
	vals := []int64{int64(l.MaxFramePayloadBytes), int64(l.MaxRecordBytes), int64(l.MaxDocumentBytes), int64(l.MaxStringBytes), int64(l.MaxRequestIDBytes), int64(l.MaxDepth), int64(l.MaxMembers), int64(l.MaxNodes), int64(l.MaxWrites), int64(l.MaxPage), int64(l.MaxRecords), l.MaxRetainedBytes, l.MaxJournalBytes}
	caps := []int64{int64(max.MaxFramePayloadBytes), int64(max.MaxRecordBytes), int64(max.MaxDocumentBytes), int64(max.MaxStringBytes), int64(max.MaxRequestIDBytes), int64(max.MaxDepth), int64(max.MaxMembers), int64(max.MaxNodes), int64(max.MaxWrites), int64(max.MaxPage), int64(max.MaxRecords), max.MaxRetainedBytes, max.MaxJournalBytes}
	for i, v := range vals {
		if v <= 0 || v > caps[i] {
			return reject(fmt.Sprintf("limit %d outside v1 ceiling", i))
		}
	}
	if l.MaxDocumentBytes > l.MaxRecordBytes || l.MaxRecordBytes > l.MaxFramePayloadBytes || l.MaxStringBytes > l.MaxRecordBytes || l.MaxRequestIDBytes > l.MaxStringBytes || int64(l.MaxFramePayloadBytes)+72 > l.MaxJournalBytes {
		return reject("incoherent limits")
	}
	return nil
}

// Conversation has immutable ancestry. ParentAt is the inclusive entry-ID cutoff
// visible through Parent, not a commit sequence (later entries in the same commit
// are excluded). Owner names the creating task, independently of ancestry.
type Conversation struct {
	ID       ID     `json:"id"`
	Owner    ID     `json:"owner,omitempty"`
	Parent   ID     `json:"parent,omitempty"`
	Name     string `json:"name,omitempty"`
	ParentAt ID     `json:"parentAt,omitempty"`
}
type Entry struct {
	ID           ID     `json:"id"`
	Conversation ID     `json:"conversation"`
	Kind         string `json:"kind"`
	Value        JSON   `json:"value"`
	Seq          uint64 `json:"seq,omitempty"`
	Position     uint64 `json:"position,omitempty"`
	// Head is the first visible entry contributing to active model context.
	// A marker may select itself; zero leaves the preceding marker unchanged.
	Head    ID               `json:"head,omitempty"`
	Data    any              `json:"data,omitempty"`
	HasData bool             `json:"hasData,omitempty"`
	Model   []MessageReceipt `json:"model,omitempty"`
	Edits   []ContextEdit    `json:"edits,omitempty"`
	ByTask  ID               `json:"byTask,omitempty"`
}

// Task retains the original native journal checkpoint envelope. Execution is
// absent in old journals; those records are never guessed into generic tasks.
// New readers accept old records. Old binaries reject new Execution fields.
type Task struct {
	ID           ID             `json:"id"`
	Conversation ID             `json:"conversation"`
	Owner        ID             `json:"owner,omitempty"`
	Kind         string         `json:"kind"`
	Status       string         `json:"status"`
	Checkpoint   JSON           `json:"checkpoint"`
	Execution    *TaskExecution `json:"execution,omitempty"`
}

// TaskValue explicitly distinguishes an absent field from a strict JSON null.
// Present must be true. Value accepts primitive, array, object or null roots;
// executable/custom-marshaler values never cross the task ownership boundary.
type TaskValue struct {
	Present bool `json:"present"`
	Value   any  `json:"value"`
}

type TaskOutcomeError struct {
	Message string     `json:"message"`
	Detail  *TaskValue `json:"detail,omitempty"`
}

type TaskOutcome struct {
	Status string            `json:"status"`
	Result *TaskValue        `json:"result,omitempty"`
	Error  *TaskOutcomeError `json:"error,omitempty"`
	Reason string            `json:"reason,omitempty"`
}

// TaskState is a strict union. Live states own an object checkpoint. Outcomes
// have no checkpoint/memos; waiting additionally owns On and Policy.
type TaskState struct {
	Status     string       `json:"status"`
	Checkpoint JSON         `json:"checkpoint,omitempty"`
	On         []ID         `json:"on,omitempty"`
	Policy     string       `json:"policy,omitempty"`
	Outcome    *TaskOutcome `json:"outcome,omitempty"`
}

type NativeTaskExecution struct {
	Version        uint64                `json:"version"`
	Input          *TaskValue            `json:"input"`
	Background     bool                  `json:"background"`
	AbortRequested bool                  `json:"abortRequested"`
	Memos          map[string]*TaskValue `json:"memos,omitempty"`
	State          TaskState             `json:"state"`
}

// BuiltinTaskHold is a private-adapter disposition, not executable authority.
// Receipt actions identify an already committed receipt and spend; finalising
// them performs deterministic cleanup only, never a host callback or effect.
type BuiltinTaskHold struct {
	Stage        string      `json:"stage"` // held or final
	Action       string      `json:"action"`
	Outcome      TaskOutcome `json:"outcome"`
	FinalStatus  string      `json:"finalStatus"`
	Entry        ID          `json:"entry,omitempty"`
	Submission   ID          `json:"submission,omitempty"`
	Conversation ID          `json:"conversation"`
	Owner        ID          `json:"owner,omitempty"`
	CallID       string      `json:"callId,omitempty"`
}

type BuiltinTaskExecution struct {
	Background     bool                  `json:"background"`
	AbortRequested bool                  `json:"abortRequested"`
	Memos          map[string]*TaskValue `json:"memos,omitempty"`
	Hold           *BuiltinTaskHold      `json:"hold,omitempty"`
}

// Exactly one tag-matched variant may be present. Built-ins keep their single
// live checkpoint in Task.Checkpoint, including legacy cp.Abort/tool joins.
type TaskExecution struct {
	Tag     string                `json:"tag"`
	Native  *NativeTaskExecution  `json:"native,omitempty"`
	Builtin *BuiltinTaskExecution `json:"builtin,omitempty"`
}

// TaskRecord is a detached canonical runtime view. Legacy raw records remain
// available through Snapshot; their absent generic results are not invented.
type TaskRecord struct {
	ID             ID
	Conversation   ID
	Owner          ID
	Kind           string
	Version        uint64
	Input          *TaskValue
	Background     bool
	AbortRequested bool
	Memos          map[string]*TaskValue
	State          TaskState
}

type TaskOwnership struct {
	Kind string // conversation or task; explicitly required
	Task ID
}

type TaskOptions struct {
	Conversation ID
	Ownership    TaskOwnership
	Background   bool
}

type TaskInspection struct {
	Record   TaskRecord
	Kind     string // ready, running, waiting, completing, blocked
	Reason   string
	Migrates bool
	On       []ID
}

type ConversationAbortOptions struct{ Background bool }

type HarnessInspection struct {
	Scheduling string // paused, running, closing
	Tasks      []TaskInspection
	// Native pending/running correspond to reference queued/placed states.
	Submissions []Submission
}

// TaskInspectionView preserves the existing Go helper result name.
type TaskInspectionView = HarnessInspection

// Submission type is follow-up or write. Steering is unsupported in M1a.
// Replacement preserves conversation, request ID and type identity.
type Submission struct {
	ID           ID     `json:"id"`
	Conversation ID     `json:"conversation"`
	RequestID    string `json:"requestId,omitempty"`
	Type         string `json:"type"`
	Status       string `json:"status"`
	Value        JSON   `json:"value"`
}

// Document is a detached materialised incarnation at an exact scope/kind/key address.
// Retirement preserves its ID/history metadata but removes the current address.
type Document struct {
	ID      ID     `json:"id"`
	Scope   string `json:"scope"`
	Owner   ID     `json:"owner,omitempty"`
	Kind    string `json:"kind"`
	Key     string `json:"key"`
	Version uint64 `json:"version"`
	Value   JSON   `json:"value"`
	Retired bool   `json:"retired,omitempty"`
	// Family distinguishes a keyed member (including an empty key) from a
	// singleton. Non-empty legacy keys are also treated as family members.
	Family    bool   `json:"family,omitempty"`
	History   string `json:"history,omitempty"` // conversation: latest or rewindable
	Fork      string `json:"fork,omitempty"`    // conversation: initial, current or asOf
	CreatedAt uint64 `json:"createdAt,omitempty"`
	RetiredAt uint64 `json:"retiredAt,omitempty"`
	// DeltasSinceBase is storage-owned and excludes any uncommitted change.
	DeltasSinceBase uint64 `json:"deltasSinceBase,omitempty"`
}

// DocumentRevision is an immutable full base or same-version decoded delta.
// Legacy records with no Kind are bases; retained revisions allow exact point reads.
type DocumentRevision struct {
	Seq     uint64 `json:"seq"`
	Version uint64 `json:"version"`
	Value   JSON   `json:"value,omitempty"`
	// Kind empty is a legacy full base. Delta revisions have Ops, no Value.
	Kind string      `json:"kind,omitempty"`
	Ops  []Operation `json:"ops,omitempty"`
}

// DocumentDelta identifies an existing incarnation, never its mutable policies.
type DocumentDelta struct {
	ID      ID          `json:"id"`
	Version uint64      `json:"version"`
	Ops     []Operation `json:"ops"`
}

// DocumentPoint selects current content or an exact historical commit. The zero
// value selects Seq0 (before the first commit), not current state.
type DocumentPoint struct {
	Current bool   `json:"current,omitempty"`
	Seq     uint64 `json:"seq,omitempty"`
}

func CurrentDocumentPoint() DocumentPoint { return DocumentPoint{Current: true} }

type DocumentCopySource struct {
	ID ID            `json:"id"`
	At DocumentPoint `json:"at"`
}

// Write is an explicit tagged command. Exactly its matching record is allowed.
// Entry/Conversation creations are immutable. Task and Submission are full
// replacements. Documents use explicit full bases, decoded deltas, copies or retirement.
type Write struct {
	Op           string              `json:"op"`
	Conversation *Conversation       `json:"conversation,omitempty"`
	Entry        *Entry              `json:"entry,omitempty"`
	Task         *Task               `json:"task,omitempty"`
	Submission   *Submission         `json:"submission,omitempty"`
	Document     *Document           `json:"document,omitempty"`
	Source       *DocumentCopySource `json:"source,omitempty"` // copy-document only
	Delta        *DocumentDelta      `json:"delta,omitempty"`  // delta-document only
}
type Batch struct {
	Writes []Write `json:"writes"`
}

// Snapshot is a detached, atomic adopted revision, including allocator metadata.
// Mutating its maps never changes storage. It is not a raw authority accessor.
type Snapshot struct {
	Seq               uint64                    `json:"seq"`
	HighWater         uint64                    `json:"highWater"`
	Conversations     map[ID]Conversation       `json:"conversations"`
	Entries           map[ID]Entry              `json:"entries"`
	Tasks             map[ID]Task               `json:"tasks"`
	Submissions       map[ID]Submission         `json:"submissions"`
	Documents         map[ID]Document           `json:"documents"`
	DocumentRevisions map[ID][]DocumentRevision `json:"documentRevisions,omitempty"`
	retained          *retainedSizes            // private immutable encoded-size cache; never persisted
}

type retainedSizes struct {
	records   map[retainedKey]int
	revisions map[ID][]int
}
type retainedKey struct {
	kind string
	id   ID
}

type Query struct {
	Conversation ID
	Owner        ID
	Status       string
	Kind         string
	Scope        string
	Key          *string
	After        ID
	Limit        int
}

// EntryCursor is a stable (commit sequence, in-batch position) cursor. IDs need
// not be committed in mint order. A cursor is only valid for its conversation.
type EntryCursor struct {
	Conversation ID
	Seq          uint64
	Position     uint64
}

// Storage acknowledgements follow append/sync settlement and state adoption.
// Implementations may reject canceled admission; cancellation after admission
// cannot end settlement. Reads reject after poison or close.
type Storage interface {
	MintID(context.Context) (ID, error)
	Apply(context.Context, Batch) (uint64, error)
	Snapshot(context.Context) (Snapshot, error)
	Conversations(context.Context, Query) ([]Conversation, error)
	Entries(context.Context, ID, EntryCursor, int) ([]Entry, error)
	Tasks(context.Context, Query) ([]Task, error)
	Submissions(context.Context, Query) ([]Submission, error)
	Documents(context.Context, Query) ([]Document, error)
	Request(context.Context, ID, string) (Submission, bool, error)
	Limits() (Limits, error)
	Close(context.Context) error
}
