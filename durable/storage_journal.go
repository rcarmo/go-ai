package durable

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const headerBytes = 64
const trailerBytes = 8

var journalMagic = []byte("GODJNL1!")
var journalEnd = []byte("ENDGOD1!")

// JournalOptions are creation policy. Omitted options use durable Sync=true and
// defaults on creation, or the exact persisted policy on reopen.
type JournalOptions struct {
	Sync   *bool
	Limits *Limits
}
type journalConfig struct {
	Version uint64 `json:"version"`
	Sync    bool   `json:"sync"`
	Limits  Limits `json:"limits"`
}
type journalFile interface {
	io.ReaderAt
	io.Writer
	Stat() (os.FileInfo, error)
	Sync() error
	Truncate(int64) error
	Seek(int64, int) (int64, error)
	Close() error
}
type JournalStorage struct {
	*storeCore
	file       journalFile
	path       string
	size       int64
	syncWrites bool
}

var journalOwners = struct {
	sync.Mutex
	paths map[string]bool
}{paths: map[string]bool{}}

func ownPath(p string) error {
	journalOwners.Lock()
	defer journalOwners.Unlock()
	if journalOwners.paths[p] {
		return ErrOwned
	}
	journalOwners.paths[p] = true
	return nil
}
func releasePath(p string) {
	journalOwners.Lock()
	delete(journalOwners.paths, p)
	journalOwners.Unlock()
}

func OpenJournal(dir string, options JournalOptions) (store *JournalStorage, err error) {
	l := DefaultLimits()
	if options.Limits != nil {
		l = *options.Limits
	}
	if err = l.validate(); err != nil {
		return nil, err
	}
	syncWrites := true
	if options.Sync != nil {
		syncWrites = *options.Sync
	}
	// Config has its own fixed bootstrap budgets, independent of lowered data
	// container/depth/node limits. Reject a profile unable to fit its config
	// before even creating a directory/file.
	configPayload, e := encodeBounded(journalConfig{Version: 1, Sync: syncWrites, Limits: l}, DefaultLimits(), 16<<10)
	if e != nil {
		return nil, e
	}
	if int64(len(configPayload))+72 > l.MaxJournalBytes {
		return nil, reject("journal cannot hold config frame")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, reject("invalid journal path")
	}
	// Track every newly created directory edge; syncing only the leaf does not
	// acknowledge the parent's directory entry. Hosts must exclude concurrent
	// directory replacement and other processes opening this path.
	var createdParents []string
	for p := absolute; ; p = filepath.Dir(p) {
		_, e := os.Stat(p)
		if e == nil {
			break
		}
		if !errors.Is(e, os.ErrNotExist) {
			return nil, reject("invalid journal directory")
		}
		parent := filepath.Dir(p)
		if parent == p {
			return nil, reject("invalid journal directory")
		}
		createdParents = append(createdParents, parent)
	}
	if err = os.MkdirAll(absolute, 0700); err != nil {
		return nil, fmt.Errorf("durable: journal directory unavailable")
	}
	if syncWrites {
		for i := len(createdParents) - 1; i >= 0; i-- {
			if e := syncDirectory(createdParents[i]); e != nil {
				return nil, fmt.Errorf("%w: parent directory sync failed", ErrPoisoned)
			}
		}
	}
	// Canonical directory aliases share one in-process ownership fence. Cross-
	// process exclusion is explicitly the host's responsibility.
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, reject("invalid journal directory")
	}
	info, err := os.Stat(absolute)
	if err != nil || info.Mode().Perm()&0077 != 0 {
		return nil, reject("journal directory must be owner-only")
	}
	path := filepath.Join(absolute, "journal.bin")
	if err = ownPath(path); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			releasePath(path)
		}
	}()
	if info, e := os.Lstat(path); e == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, reject("journal must be regular and owner-only")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, fmt.Errorf("durable: journal unavailable")
	}
	f, err := openJournalFile(path)
	if err != nil {
		return nil, fmt.Errorf("durable: journal open failed")
	}
	defer func() {
		if err != nil {
			_ = f.Close()
		}
	}()
	c := newCore(l)
	j := &JournalStorage{storeCore: c, file: f, path: path, syncWrites: syncWrites}
	if err = j.replay(options); err != nil {
		return nil, err
	}
	if j.size == 0 {
		payload, e := encodeBounded(journalConfig{Version: 1, Sync: syncWrites, Limits: l}, DefaultLimits(), 16<<10)
		if e != nil {
			return nil, e
		}
		if e = j.append(0, 1, 1, payload); e != nil {
			return nil, fmt.Errorf("%w: journal creation settlement failed", ErrPoisoned)
		}
		// Sync the directory even when a legal partial first config was truncated.
		if syncWrites {
			if e := syncDirectory(absolute); e != nil {
				return nil, fmt.Errorf("%w: directory sync failed", ErrPoisoned)
			}
		}
	}
	// Reopen establishes the adopted prefix's durability even when a previous
	// creation acknowledged uncertain file/directory sync. Sync every directory
	// edge to the root; a canceled Open never releases an owned file mid-barrier.
	if j.syncWrites {
		if e := j.file.Sync(); e != nil {
			return nil, fmt.Errorf("%w: reopen file sync failed", ErrPoisoned)
		}
		for p := absolute; ; p = filepath.Dir(p) {
			if e := syncDirectory(p); e != nil {
				return nil, fmt.Errorf("%w: directory durability failed", ErrPoisoned)
			}
			if filepath.Dir(p) == p {
				break
			}
		}
	}
	c.appendFrame = j.append
	c.finish = func() error { e := j.file.Close(); releasePath(path); return e }
	return j, nil
}

var openJournalFile = func(path string) (journalFile, error) { return os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600) }

var syncDirectory = func(path string) error {
	d, e := os.Open(path)
	if e != nil {
		return e
	}
	syncErr := d.Sync()
	closeErr := d.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func frame(typ byte, ordinal, high uint64, payload []byte) []byte {
	b := make([]byte, headerBytes+len(payload)+trailerBytes)
	copy(b, journalMagic)
	binary.LittleEndian.PutUint16(b[8:10], 1)
	b[10] = typ
	binary.LittleEndian.PutUint32(b[12:16], uint32(len(payload)))
	binary.LittleEndian.PutUint64(b[16:24], ordinal)
	binary.LittleEndian.PutUint64(b[24:32], high)
	copy(b[64:], payload)
	h := sha256.New()
	h.Write(b[:32])
	h.Write(payload)
	copy(b[32:64], h.Sum(nil))
	copy(b[64+len(payload):], journalEnd)
	return b
}
func (j *JournalStorage) append(typ byte, ordinal, high uint64, payload []byte) error {
	max := j.limits.MaxFramePayloadBytes
	if typ == 0 {
		max = 16 << 10
	}
	if len(payload) > max || int64(len(payload))+72 > j.limits.MaxJournalBytes-j.size {
		return reject("journal payload/file limit")
	}
	b := frame(typ, ordinal, high, payload)
	// Write is one admission boundary. Any short write or subsequent sync error is
	// uncertain, including zero-byte ENOSPC. Never publish from uncertain state.
	n, e := j.file.Write(b)
	if e != nil || n != len(b) {
		return errors.New("journal append failed")
	}
	if j.syncWrites {
		if e = j.file.Sync(); e != nil {
			return errors.New("journal sync failed")
		}
	}
	j.size += int64(len(b))
	return nil
}
func (j *JournalStorage) replay(options JournalOptions) error {
	info, e := j.file.Stat()
	if e != nil {
		return fmt.Errorf("durable: journal stat failed")
	}
	size := info.Size()
	if size < 0 || size > 4<<30 {
		return fmt.Errorf("%w: journal file limit", ErrCorrupt)
	}
	pos := int64(0)
	ordinal := uint64(0)
	state := initialState()
	configured := false
	truncate := func() error {
		if e := j.file.Truncate(pos); e != nil {
			return fmt.Errorf("%w: tail truncation failed", ErrPoisoned)
		}
		if j.syncWrites {
			if e := j.file.Sync(); e != nil {
				return fmt.Errorf("%w: tail sync failed", ErrPoisoned)
			}
		}
		size = pos
		return nil
	}
	for pos < size {
		available := size - pos
		header := make([]byte, 64)
		n := int64(64)
		if available < n {
			n = available
		}
		if _, e = j.file.ReadAt(header[:n], pos); e != nil {
			return fmt.Errorf("%w: header read failed", ErrCorrupt)
		}
		if e = checkHeaderPrefix(header[:n], ordinal, state.HighWater, configured, j.limits); e != nil {
			return e
		}
		if n < 64 {
			if e = truncate(); e != nil {
				return e
			}
			break
		}
		typ := header[10]
		length := int64(binary.LittleEndian.Uint32(header[12:16]))
		ord := binary.LittleEndian.Uint64(header[16:24])
		high := binary.LittleEndian.Uint64(header[24:32])
		total := length + 72
		if available < total {
			// If payload is complete, validate any available terminator prefix. A
			// mismatched suffix cannot be relabelled a harmless torn append.
			if available > 64+length {
				suffix := make([]byte, available-64-length)
				if _, e = j.file.ReadAt(suffix, pos+64+length); e != nil {
					return fmt.Errorf("%w: tail read failed", ErrCorrupt)
				}
				if !bytes.Equal(suffix, journalEnd[:len(suffix)]) {
					return fmt.Errorf("%w: torn terminator mismatch", ErrCorrupt)
				}
			}
			if e = truncate(); e != nil {
				return e
			}
			break
		}
		payload := make([]byte, length)
		if _, e = j.file.ReadAt(payload, pos+64); e != nil {
			return fmt.Errorf("%w: payload read failed", ErrCorrupt)
		}
		suffix := make([]byte, 8)
		if _, e = j.file.ReadAt(suffix, pos+64+length); e != nil {
			return fmt.Errorf("%w: trailer read failed", ErrCorrupt)
		}
		h := sha256.New()
		h.Write(header[:32])
		h.Write(payload)
		if !bytes.Equal(header[32:64], h.Sum(nil)) || !bytes.Equal(suffix, journalEnd) {
			return fmt.Errorf("%w: frame integrity", ErrCorrupt)
		}
		switch typ {
		case 0:
			var shape JSON
			if e = decodeStrict(payload, DefaultLimits(), 16<<10, &shape); e != nil || len(shape) != 3 {
				return fmt.Errorf("%w: config shape", ErrCorrupt)
			}
			for _, field := range []string{"version", "sync", "limits"} {
				if _, ok := shape[field]; !ok {
					return fmt.Errorf("%w: missing config field", ErrCorrupt)
				}
			}
			var config journalConfig
			if e = decodeStrict(payload, DefaultLimits(), 16<<10, &config); e != nil {
				return fmt.Errorf("%w: config JSON", ErrCorrupt)
			}
			if config.Version != 1 || config.Limits.validate() != nil {
				return fmt.Errorf("%w: persisted config", ErrCorrupt)
			}
			if options.Sync != nil && *options.Sync != config.Sync || options.Limits != nil && *options.Limits != config.Limits {
				return reject("persisted journal policy conflict")
			}
			j.limits = config.Limits
			j.syncWrites = config.Sync
			configured = true
			if size > j.limits.MaxJournalBytes {
				return fmt.Errorf("%w: persisted journal file limit", ErrCorrupt)
			}
		case 1:
			var r reservation
			if e = decodeStrict(payload, j.limits, j.limits.MaxFramePayloadBytes, &r); e != nil {
				return fmt.Errorf("%w: reservation JSON", ErrCorrupt)
			}
			if state.HighWater >= MaxID || r.First != state.HighWater+1 || r.Last < r.First || r.Last > MaxID || high != r.Last {
				return fmt.Errorf("%w: reservation relation", ErrCorrupt)
			}
			state.HighWater = r.Last
		case 2:
			var r commitRecord
			if e = decodeStrict(payload, j.limits, j.limits.MaxFramePayloadBytes, &r); e != nil {
				return fmt.Errorf("%w: commit JSON", ErrCorrupt)
			}
			next, e := prepare(state, r, j.limits)
			if e != nil {
				return fmt.Errorf("%w: invalid batch", ErrCorrupt)
			}
			state = next
		}
		ordinal = ord
		pos += total
	}
	if configured {
		j.state = state
		j.ordinal = ordinal
	} else if size != 0 {
		return fmt.Errorf("%w: missing config", ErrCorrupt)
	}
	j.size = size
	if _, e = j.file.Seek(size, io.SeekStart); e != nil {
		return fmt.Errorf("%w: journal seek failed", ErrCorrupt)
	}
	return nil
}

// checkHeaderPrefix checks every fully available field, including legal torn
// first config headers. It never scans ahead for magic after corrupt bytes.
func checkHeaderPrefix(b []byte, ordinal, high uint64, configured bool, l Limits) error {
	bad := func() error { return fmt.Errorf("%w: invalid header prefix", ErrCorrupt) }
	n := len(b)
	m := n
	if m > 8 {
		m = 8
	}
	if !bytes.Equal(b[:m], journalMagic[:m]) {
		return bad()
	}
	if n >= 9 && b[8] != 1 {
		return bad()
	}
	if n >= 10 && b[9] != 0 {
		return bad()
	}
	if n >= 11 {
		typ := b[10]
		if typ > 2 || (!configured && typ != 0) || (configured && typ == 0) {
			return bad()
		}
	}
	if n >= 12 && b[11] != 0 {
		return bad()
	}
	if n >= 16 {
		length := binary.LittleEndian.Uint32(b[12:16])
		max := l.MaxFramePayloadBytes
		if b[10] == 0 {
			max = 16 << 10
		}
		if length == 0 || uint64(length) > uint64(max) {
			return bad()
		}
	}
	if n >= 24 {
		ord := binary.LittleEndian.Uint64(b[16:24])
		if ordinal >= MaxID || ord != ordinal+1 || ord > MaxID {
			return bad()
		}
	}
	if n >= 32 {
		h := binary.LittleEndian.Uint64(b[24:32])
		if h < 1 || h > MaxID {
			return bad()
		}
		switch b[10] {
		case 0:
			if h != 1 {
				return bad()
			}
		case 1:
			if h <= high {
				return bad()
			}
		case 2:
			if h != high {
				return bad()
			}
		}
	}
	return nil
}

var _ Storage = (*JournalStorage)(nil)
