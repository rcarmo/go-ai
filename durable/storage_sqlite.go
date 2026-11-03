package durable

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SQLiteStorage keeps validated reservation/commit frames in atomic SQLite WAL
// transactions. It uses native Go record semantics, not upstream file formats.
// The engine is CGO-free; one connection owns all SQL settlement for its lifetime.
type SQLiteStorage struct {
	*storeCore
	db     *sql.Conn
	pool   *sql.DB
	prefix string
	path   string
}
type SQLiteOptions struct {
	Limits *Limits
	// Prefix namespaces all native tables. Empty preserves the original schema.
	// Changing the prefix opens a separate store; it never migrates existing data.
	Prefix string
}

var sqlitePrefixPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var sqliteTableToken = regexp.MustCompile(`\b(config|frames|checkpoint)\b`)

func (s *SQLiteStorage) schemaSQL(text string) string {
	if s.prefix == "" {
		return text
	}
	return sqliteTableToken.ReplaceAllStringFunc(text, func(table string) string { return `"` + s.prefix + table + `"` })
}

func OpenSQLite(path string, options SQLiteOptions) (store *SQLiteStorage, err error) {
	if options.Prefix != "" && (!sqlitePrefixPattern.MatchString(options.Prefix) || strings.HasPrefix(strings.ToLower(options.Prefix), "sqlite_")) {
		return nil, reject("invalid SQLite table prefix")
	}
	limits := DefaultLimits()
	if options.Limits != nil {
		limits = *options.Limits
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	// Reject unsupported targets before creating files.
	if !sqliteEngineAvailable {
		return nil, ErrUnsupported
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, err
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
	file, err := lockStorageFile(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = file.Close()
		}
	}()
	if info, e := os.Lstat(path); e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, reject("SQLite file must be regular and owner-only")
	}
	// Escape the canonical filename as a URI path, never as caller-controlled
	// DSN options. ?, # and % in filenames must not select another database or
	// inject mode/immutable/nolock/pragma parameters past the inode lock.
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	connector, err := newSQLiteConnector(uri.String())
	if err != nil {
		return nil, fmt.Errorf("durable: SQLite connector: %w", err)
	}
	pool := sql.OpenDB(connector)
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	defer func() {
		if err != nil {
			_ = pool.Close()
		}
	}()
	// Opening and all admitted SQL use a non-cancelling context. Public contexts
	// control storeCore admission only; they cannot interrupt durability settlement.
	db, err := pool.Conn(context.Background())
	if err != nil {
		return nil, fmt.Errorf("durable: SQLite open failed: %w", err)
	}
	defer func() {
		if err != nil {
			_ = db.Close()
		}
	}()
	store = &SQLiteStorage{storeCore: newCore(limits), db: db, pool: pool, path: path, prefix: options.Prefix}
	if err = store.exec("PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; CREATE TABLE IF NOT EXISTS config(id INTEGER PRIMARY KEY CHECK(id=1), value BLOB NOT NULL); CREATE TABLE IF NOT EXISTS frames(ordinal INTEGER PRIMARY KEY, kind INTEGER NOT NULL, highwater INTEGER NOT NULL, payload BLOB NOT NULL); CREATE TABLE IF NOT EXISTS checkpoint(id INTEGER PRIMARY KEY CHECK(id=1), value BLOB NOT NULL);"); err != nil {
		return nil, err
	}
	var data []byte
	err = store.db.QueryRowContext(context.Background(), store.schemaSQL("SELECT value FROM config WHERE id=1")).Scan(&data)
	switch {
	case err == nil:
		var config sqliteConfig
		if decodeStrict(data, DefaultLimits(), 16<<10, &config) != nil || config.Version != 1 || config.Limits.validate() != nil {
			return nil, ErrCorrupt
		}
		if options.Limits != nil && *options.Limits != config.Limits {
			return nil, reject("persisted SQLite limits conflict")
		}
		store.limits = config.Limits
	case errors.Is(err, sql.ErrNoRows):
		data, err = encodeBounded(sqliteConfig{Version: 1, Sync: true, Limits: limits}, DefaultLimits(), 16<<10)
		if err != nil {
			return nil, err
		}
		if err = store.exec("INSERT INTO config(id,value) VALUES(1,?)", data); err != nil {
			return nil, err
		}
	default:
		return nil, ErrCorrupt
	}
	data = nil
	err = store.db.QueryRowContext(context.Background(), store.schemaSQL("SELECT value FROM checkpoint WHERE id=1")).Scan(&data)
	if err == nil {
		var checkpoint storageCheckpoint
		if decodeStrict(data, store.limits, store.limits.MaxRetainedBytesAsInt(), &checkpoint) != nil {
			return nil, ErrCorrupt
		}
		store.state, err = restoreCheckpoint(checkpoint, store.limits)
		if err != nil {
			return nil, ErrCorrupt
		}
		store.ordinal = checkpoint.Ordinal
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCorrupt
	}
	if err = store.replayFrames(); err != nil {
		return nil, err
	}
	store.appendFrame = store.append
	store.appendFrames = store.appendSeries
	store.finish = func() error {
		// Conn.Close alone returns it to the pool. Retain the inode lock until
		// the pool has physically closed the connection as well.
		connErr := store.db.Close()
		dbErr := store.pool.Close()
		lockErr := file.Close()
		releasePath(path)
		return errors.Join(connErr, dbErr, lockErr)
	}
	return store, nil
}

func (s *SQLiteStorage) replayFrames() error {
	rows, err := s.db.QueryContext(context.Background(), s.schemaSQL("SELECT ordinal,kind,highwater,payload FROM frames ORDER BY ordinal"))
	if err != nil {
		return ErrCorrupt
	}
	defer rows.Close()
	for rows.Next() {
		var ordinal, kind, high int64
		var data []byte
		if rows.Scan(&ordinal, &kind, &high, &data) != nil || ordinal <= 0 || uint64(ordinal) != s.ordinal+1 || high < 0 || uint64(high) < s.state.HighWater || uint64(high) > MaxID {
			return ErrCorrupt
		}
		switch kind {
		case 1:
			var reservation reservation
			if decodeStrict(data, s.limits, s.limits.MaxFramePayloadBytes, &reservation) != nil || reservation.First != s.state.HighWater+1 || reservation.Last != uint64(high) {
				return ErrCorrupt
			}
			s.state.HighWater = uint64(high)
		case 2:
			if uint64(high) != s.state.HighWater {
				return ErrCorrupt
			}
			var record commitRecord
			if decodeStrict(data, s.limits, s.limits.MaxFramePayloadBytes, &record) != nil {
				return ErrCorrupt
			}
			next, err := prepare(s.state, record, s.limits)
			if err != nil {
				return ErrCorrupt
			}
			s.state = next
		default:
			return ErrCorrupt
		}
		s.ordinal = uint64(ordinal)
	}
	if rows.Err() != nil {
		return ErrCorrupt
	}
	if rows.Close() != nil {
		return ErrCorrupt
	}
	return nil
}

func (s *SQLiteStorage) exec(text string, args ...any) error {
	if _, err := s.db.ExecContext(context.Background(), s.schemaSQL(text), args...); err != nil {
		return fmt.Errorf("durable: SQLite operation failed: %w", err)
	}
	return nil
}

func (s *SQLiteStorage) append(kind byte, ordinal, high uint64, payload []byte) error {
	return s.appendSeries([]storageFrame{{kind: kind, ordinal: ordinal, high: high, payload: payload}})
}

// appendSeries emits no receipt or state change. Only the caller adopts after
// COMMIT. Explicit SQL preserves confirmed rollback versus uncertain commit;
// sql.Tx cleanup must not hide a failed rollback or change poison semantics.
func (s *SQLiteStorage) appendSeries(frames []storageFrame) error {
	if err := s.exec("BEGIN IMMEDIATE"); err != nil {
		return reject("SQLite write admission unavailable")
	}
	var err error
	for _, frame := range frames {
		err = s.exec("INSERT INTO frames(ordinal,kind,highwater,payload) VALUES(?,?,?,?)", int64(frame.ordinal), int64(frame.kind), int64(frame.high), frame.payload)
		if err != nil {
			break
		}
	}
	if err != nil {
		if rollback := s.exec("ROLLBACK"); rollback != nil {
			return rollback
		}
		return reject("SQLite frame rejected")
	}
	return s.exec("COMMIT")
}

// Reclaim replaces physical frames with a full logical checkpoint atomically.
// No transcript entry or document revision is dropped; ordinal/sequence survive.
func (s *SQLiteStorage) Reclaim(ctx context.Context) error {
	if err := s.enter(ctx); err != nil {
		return err
	}
	defer s.leave()
	payload, err := encodeBounded(checkpointOf(s.state, s.ordinal), s.limits, s.limits.MaxRetainedBytesAsInt())
	if err != nil {
		return err
	}
	if err = s.exec("BEGIN IMMEDIATE"); err != nil {
		return reject("SQLite reclaim admission unavailable")
	}
	err = s.exec("INSERT OR REPLACE INTO checkpoint(id,value) VALUES(1,?)", payload)
	if err == nil {
		err = s.exec("DELETE FROM frames")
	}
	if err != nil {
		if rollback := s.exec("ROLLBACK"); rollback != nil {
			s.poison()
			return ErrPoisoned
		}
		return reject("SQLite reclaim rejected")
	}
	if err = s.exec("COMMIT"); err != nil {
		s.poison()
		return ErrPoisoned
	}
	// Optional maintenance failure leaves the committed checkpoint valid.
	return s.exec("PRAGMA wal_checkpoint(TRUNCATE); VACUUM;")
}
