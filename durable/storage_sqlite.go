//go:build cgo

package durable

/*
#cgo pkg-config: sqlite3
#include <sqlite3.h>
#include <stdlib.h>
static int durable_bind_blob(sqlite3_stmt *s,int i,void *p,int n){return sqlite3_bind_blob(s,i,p,n,SQLITE_TRANSIENT);}
*/
import "C"
import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"
)

// SQLiteStorage keeps validated reservation/commit frames in atomic SQLite WAL
// transactions. It uses native Go record semantics, not upstream file formats.
type SQLiteStorage struct {
	*storeCore
	db   *C.sqlite3
	path string
}
type SQLiteOptions struct{ Limits *Limits }

func OpenSQLite(path string, options SQLiteOptions) (store *SQLiteStorage, err error) {
	limits := DefaultLimits()
	if options.Limits != nil {
		limits = *options.Limits
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	parent, canonicalErr := filepath.EvalSymlinks(filepath.Dir(path))
	if canonicalErr != nil {
		return nil, canonicalErr
	}
	path = filepath.Join(parent, filepath.Base(path))
	if err = ownPath(path); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			releasePath(path)
		}
	}()
	file, e := lockStorageFile(path)
	if e != nil {
		return nil, e
	}
	defer func() {
		if err != nil {
			file.Close()
		}
	}()
	if info, e := os.Lstat(path); e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, reject("SQLite file must be regular and owner-only")
	}
	name := C.CString(path)
	defer C.free(unsafe.Pointer(name))
	var db *C.sqlite3
	if C.sqlite3_open_v2(name, &db, C.SQLITE_OPEN_READWRITE|C.SQLITE_OPEN_CREATE|C.SQLITE_OPEN_FULLMUTEX, nil) != C.SQLITE_OK {
		if db != nil {
			C.sqlite3_close(db)
		}
		return nil, fmt.Errorf("durable: SQLite open failed")
	}
	store = &SQLiteStorage{storeCore: newCore(limits), db: db, path: path}
	defer func() {
		if err != nil {
			C.sqlite3_close(db)
		}
	}()
	C.sqlite3_busy_timeout(db, 5000)
	if err = store.exec("PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; CREATE TABLE IF NOT EXISTS config(id INTEGER PRIMARY KEY CHECK(id=1), value BLOB NOT NULL); CREATE TABLE IF NOT EXISTS frames(ordinal INTEGER PRIMARY KEY, kind INTEGER NOT NULL, highwater INTEGER NOT NULL, payload BLOB NOT NULL); CREATE TABLE IF NOT EXISTS checkpoint(id INTEGER PRIMARY KEY CHECK(id=1), value BLOB NOT NULL);"); err != nil {
		return nil, err
	}
	stmt, err := store.statement("SELECT value FROM config WHERE id=1")
	if err != nil {
		return nil, err
	}
	code := C.sqlite3_step(stmt)
	if code == C.SQLITE_ROW {
		data := C.GoBytes(C.sqlite3_column_blob(stmt, 0), C.sqlite3_column_bytes(stmt, 0))
		var config journalConfig
		if err = decodeStrict(data, DefaultLimits(), 16<<10, &config); err != nil {
			C.sqlite3_finalize(stmt)
			return nil, ErrCorrupt
		}
		if config.Version != 1 || config.Limits.validate() != nil {
			C.sqlite3_finalize(stmt)
			return nil, ErrCorrupt
		}
		if options.Limits != nil && *options.Limits != config.Limits {
			C.sqlite3_finalize(stmt)
			return nil, reject("persisted SQLite limits conflict")
		}
		store.limits = config.Limits
	} else if code == C.SQLITE_DONE {
		payload, e := encodeBounded(journalConfig{Version: 1, Sync: true, Limits: limits}, DefaultLimits(), 16<<10)
		if e != nil {
			C.sqlite3_finalize(stmt)
			return nil, e
		}
		C.sqlite3_finalize(stmt)
		stmt = nil
		insert, e := store.statement("INSERT INTO config(id,value) VALUES(1,?)")
		if e != nil {
			return nil, e
		}
		if e = bindSQLiteBlob(insert, 1, payload); e != nil {
			C.sqlite3_finalize(insert)
			return nil, e
		}
		if C.sqlite3_step(insert) != C.SQLITE_DONE {
			C.sqlite3_finalize(insert)
			return nil, fmt.Errorf("durable: SQLite config write failed")
		}
		C.sqlite3_finalize(insert)
	} else {
		C.sqlite3_finalize(stmt)
		return nil, ErrCorrupt
	}
	if stmt != nil {
		C.sqlite3_finalize(stmt)
	}
	stmt, err = store.statement("SELECT value FROM checkpoint WHERE id=1")
	if err != nil {
		return nil, err
	}
	code = C.sqlite3_step(stmt)
	if code == C.SQLITE_ROW {
		data := C.GoBytes(C.sqlite3_column_blob(stmt, 0), C.sqlite3_column_bytes(stmt, 0))
		var checkpoint storageCheckpoint
		if err = decodeStrict(data, store.limits, store.limits.MaxRetainedBytesAsInt(), &checkpoint); err == nil {
			store.state, err = restoreCheckpoint(checkpoint, store.limits)
			store.ordinal = checkpoint.Ordinal
		}
		if err != nil {
			C.sqlite3_finalize(stmt)
			return nil, ErrCorrupt
		}
	} else if code != C.SQLITE_DONE {
		C.sqlite3_finalize(stmt)
		return nil, ErrCorrupt
	}
	C.sqlite3_finalize(stmt)
	stmt, err = store.statement("SELECT ordinal,kind,highwater,payload FROM frames ORDER BY ordinal")
	if err != nil {
		return nil, err
	}
	for {
		code = C.sqlite3_step(stmt)
		if code == C.SQLITE_DONE {
			break
		}
		if code != C.SQLITE_ROW {
			err = ErrCorrupt
			break
		}
		ordinal := uint64(C.sqlite3_column_int64(stmt, 0))
		kind := byte(C.sqlite3_column_int(stmt, 1))
		high := uint64(C.sqlite3_column_int64(stmt, 2))
		data := C.GoBytes(C.sqlite3_column_blob(stmt, 3), C.sqlite3_column_bytes(stmt, 3))
		if ordinal != store.ordinal+1 || high < store.state.HighWater || high > MaxID {
			err = ErrCorrupt
			break
		}
		switch kind {
		case 1:
			var reservation reservation
			if e := decodeStrict(data, store.limits, store.limits.MaxFramePayloadBytes, &reservation); e != nil || reservation.First != store.state.HighWater+1 || reservation.Last != high {
				err = ErrCorrupt
				break
			}
			store.state.HighWater = high
		case 2:
			if high != store.state.HighWater {
				err = ErrCorrupt
				break
			}
			var record commitRecord
			if e := decodeStrict(data, store.limits, store.limits.MaxFramePayloadBytes, &record); e != nil {
				err = ErrCorrupt
				break
			}
			next, e := prepare(store.state, record, store.limits)
			if e != nil {
				err = ErrCorrupt
				break
			}
			store.state = next
		default:
			err = ErrCorrupt
		}
		if err != nil {
			break
		}
		store.ordinal = ordinal
	}
	C.sqlite3_finalize(stmt)
	if err != nil {
		return nil, err
	}
	store.appendFrame = store.append
	store.finish = func() error {
		code := C.sqlite3_close(store.db)
		lockErr := file.Close()
		releasePath(path)
		if code != C.SQLITE_OK {
			return fmt.Errorf("durable: SQLite close failed")
		}
		return lockErr
	}
	return store, nil
}
func (s *SQLiteStorage) statement(sql string) (*C.sqlite3_stmt, error) {
	text := C.CString(sql)
	defer C.free(unsafe.Pointer(text))
	var stmt *C.sqlite3_stmt
	if C.sqlite3_prepare_v2(s.db, text, -1, &stmt, nil) != C.SQLITE_OK {
		return nil, fmt.Errorf("durable: SQLite prepare failed")
	}
	return stmt, nil
}
func (s *SQLiteStorage) exec(sql string) error {
	text := C.CString(sql)
	defer C.free(unsafe.Pointer(text))
	if C.sqlite3_exec(s.db, text, nil, nil, nil) != C.SQLITE_OK {
		return fmt.Errorf("durable: SQLite operation failed")
	}
	return nil
}
func bindSQLiteBlob(stmt *C.sqlite3_stmt, index int, data []byte) error {
	ptr := C.CBytes(data)
	defer C.free(ptr)
	if C.durable_bind_blob(stmt, C.int(index), ptr, C.int(len(data))) != C.SQLITE_OK {
		return errors.New("durable: SQLite binding failed")
	}
	return nil
}
func (s *SQLiteStorage) append(kind byte, ordinal, high uint64, payload []byte) error {
	if err := s.exec("BEGIN IMMEDIATE"); err != nil {
		return reject("SQLite write admission unavailable")
	}
	stmt, err := s.statement("INSERT INTO frames(ordinal,kind,highwater,payload) VALUES(?,?,?,?)")
	if err == nil {
		C.sqlite3_bind_int64(stmt, 1, C.sqlite3_int64(ordinal))
		C.sqlite3_bind_int(stmt, 2, C.int(kind))
		C.sqlite3_bind_int64(stmt, 3, C.sqlite3_int64(high))
		err = bindSQLiteBlob(stmt, 4, payload)
		if err == nil && C.sqlite3_step(stmt) != C.SQLITE_DONE {
			err = errors.New("SQLite frame insert failed")
		}
		C.sqlite3_finalize(stmt)
	}
	if err != nil {
		if rollback := s.exec("ROLLBACK"); rollback != nil {
			return rollback
		}
		return reject("SQLite frame rejected")
	}
	// Commit uncertainty is classified by storeCore as poison, never a retryable
	// StorageRejected. Reopen reconstructs the confirmed SQLite transaction prefix.
	return s.exec("COMMIT")
}

// Reclaim atomically replaces historical physical frames with a complete logical
// checkpoint. No transcript entry or document revision is dropped. New frames
// continue at the original ordinal and sequence after reopen.
func (s *SQLiteStorage) Reclaim(ctx context.Context) error {
	if err := s.enter(ctx); err != nil {
		return err
	}
	defer s.leave()
	value := checkpointOf(s.state, s.ordinal)
	payload, err := encodeBounded(value, s.limits, s.limits.MaxRetainedBytesAsInt())
	if err != nil {
		return err
	}
	if err = s.exec("BEGIN IMMEDIATE"); err != nil {
		return reject("SQLite reclaim admission unavailable")
	}
	stmt, err := s.statement("INSERT OR REPLACE INTO checkpoint(id,value) VALUES(1,?)")
	if err == nil {
		err = bindSQLiteBlob(stmt, 1, payload)
		if err == nil && C.sqlite3_step(stmt) != C.SQLITE_DONE {
			err = errors.New("SQLite checkpoint write failed")
		}
		C.sqlite3_finalize(stmt)
	}
	if err == nil {
		err = s.exec("DELETE FROM frames")
	}
	if err != nil {
		if rollback := s.exec("ROLLBACK"); rollback != nil {
			s.poisoned = true
			return ErrPoisoned
		}
		return reject("SQLite reclaim rejected")
	}
	if err = s.exec("COMMIT"); err != nil {
		s.poisoned = true
		return ErrPoisoned
	}
	// A failed optional vacuum leaves the committed checkpoint valid.
	return s.exec("PRAGMA wal_checkpoint(TRUNCATE); VACUUM;")
}
