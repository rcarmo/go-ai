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
	db                sqliteConnection
	pool              *sql.DB
	prefix            string
	path              string
	implicitRoot      bool
	upstreamSchema    bool
	upstreamHighWater uint64
}

// sqliteConnection is the database boundary used by the native store.
type sqliteConnection interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	Close() error
}

type SQLiteOptions struct {
	// ReadOnly opens an existing database without acquiring its writer lock.
	ReadOnly bool
	Limits   *Limits
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
	if !options.ReadOnly {
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	path = filepath.Join(parent, filepath.Base(path))
	var releaseWriter func() error
	if !options.ReadOnly {
		releaseWriter, err = ownSQLiteWriter(path)
		if err != nil {
			return nil, err
		}
		defer func() {
			if err != nil {
				_ = releaseWriter()
			}
		}()
	}
	if info, e := os.Lstat(path); e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, reject("SQLite file must be regular and owner-only")
	}
	// Escape the canonical filename as a URI path, never as caller-controlled
	// DSN options. ?, # and % in filenames must not select another database or
	// inject mode/immutable/nolock/pragma parameters past the inode lock.
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if options.ReadOnly {
		uri.RawQuery = "mode=ro"
	}
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
	var readVersion int64
	store.upstreamSchema, err = store.hasUpstreamSQLiteSchema()
	if err != nil {
		return nil, err
	}
	store.readOnly = options.ReadOnly
	if options.ReadOnly || store.upstreamSchema {
		if err = store.exec("PRAGMA busy_timeout=5000"); err != nil {
			return nil, err
		}
		if readVersion, err = store.sqliteDataVersion(); err != nil {
			return nil, err
		}
		begin := "BEGIN"
		if !options.ReadOnly {
			begin = "BEGIN IMMEDIATE"
		}
		if err = store.exec(begin); err != nil {
			return nil, err
		}
	} else {
		if err = store.exec("PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; CREATE TABLE IF NOT EXISTS config(id INTEGER PRIMARY KEY CHECK(id=1), value BLOB NOT NULL); CREATE TABLE IF NOT EXISTS frames(ordinal INTEGER PRIMARY KEY, kind INTEGER NOT NULL, highwater INTEGER NOT NULL, payload BLOB NOT NULL); CREATE TABLE IF NOT EXISTS checkpoint(id INTEGER PRIMARY KEY CHECK(id=1), value BLOB NOT NULL);"); err != nil {
			return nil, err
		}
	}
	if !store.upstreamSchema {
		var data []byte
		err = store.db.QueryRowContext(context.Background(), store.schemaSQL("SELECT value FROM config WHERE id=1")).Scan(&data)
		switch {
		case err == nil:
			var config sqliteConfig
			if decodeStrict(data, DefaultLimits(), 16<<10, &config) != nil || (config.Version != 1 && config.Version != sqliteConfigVersion) || config.Limits.validate() != nil {
				return nil, ErrCorrupt
			}
			if options.Limits != nil && *options.Limits != config.Limits {
				return nil, reject("persisted SQLite limits conflict")
			}
			store.limits = config.Limits
			// Version 1 predates the raw/fresh root split and never persisted a root
			// conversation write, so the reserved root stays implicit there.
			store.implicitRoot = config.Version == 1
		case errors.Is(err, sql.ErrNoRows):
			if options.ReadOnly {
				return nil, ErrCorrupt
			}
			data, err = encodeBounded(sqliteConfig{Version: sqliteConfigVersion, Sync: true, Limits: limits}, DefaultLimits(), 16<<10)
			if err != nil {
				return nil, err
			}
			if err = store.exec("INSERT INTO config(id,value) VALUES(1,?)", data); err != nil {
				return nil, err
			}
		default:
			return nil, ErrCorrupt
		}
	}
	if err = store.readSQLitePrefix(); err != nil {
		return nil, err
	}
	if store.upstreamSchema && !options.ReadOnly {
		if err = store.migrateUpstreamSQLite(); err != nil {
			return nil, err
		}
	}
	if options.ReadOnly || store.upstreamSchema {
		if err = store.exec("COMMIT"); err != nil {
			return nil, err
		}
	}
	if options.ReadOnly {
		store.refreshRead = func() error { return store.refreshSQLitePrefix(&readVersion) }
	} else if store.upstreamSchema {
		store.volatileReservations = true
		store.upstreamHighWater = store.state.HighWater
		store.appendFrames = store.appendUpstreamSeries
		store.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
			return store.appendUpstreamSeries([]storageFrame{{kind: kind, ordinal: ordinal, high: high, payload: payload}})
		}
	} else {
		store.appendFrame = store.append
		store.appendFrames = store.appendSeries
	}
	store.finish = func() error {
		// Conn.Close alone returns it to the pool. Retain the inode lock until
		// the pool has physically closed the connection as well.
		connErr := store.db.Close()
		dbErr := store.pool.Close()
		if releaseWriter != nil {
			return errors.Join(connErr, dbErr, releaseWriter())
		}
		return errors.Join(connErr, dbErr)
	}
	return store, nil
}

func ownSQLiteWriter(path string) (func() error, error) {
	if err := ownPath(path); err != nil {
		return nil, err
	}
	file, err := lockStorageFile(path)
	if err != nil {
		releasePath(path)
		return nil, err
	}
	return func() error {
		err := file.Close()
		releasePath(path)
		return err
	}, nil
}

func (s *SQLiteStorage) readSQLitePrefix() error {
	if s.upstreamSchema {
		return s.readUpstreamSQLitePrefix()
	}
	if s.implicitRoot {
		s.state.Conversations[1] = Conversation{ID: 1}
	}
	var data []byte
	err := s.db.QueryRowContext(context.Background(), s.schemaSQL("SELECT value FROM checkpoint WHERE id=1")).Scan(&data)
	if err == nil {
		var checkpoint storageCheckpoint
		if decodeStrict(data, s.limits, s.limits.MaxRetainedBytesAsInt(), &checkpoint) != nil {
			return ErrCorrupt
		}
		s.state, err = restoreCheckpoint(checkpoint, s.limits)
		if err != nil {
			return ErrCorrupt
		}
		s.ordinal = checkpoint.Ordinal
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ErrCorrupt
	}
	return s.replayFrames()
}

func (s *SQLiteStorage) sqliteDataVersion() (int64, error) {
	var version int64
	err := s.db.QueryRowContext(context.Background(), "PRAGMA data_version").Scan(&version)
	return version, err
}

// A commit between reading data_version and starting the transaction can only
// cause an extra refresh. The checkpoint and tail come from one SQL snapshot.
func (s *SQLiteStorage) refreshSQLitePrefix(stamp *int64) error {
	next, err := s.sqliteDataVersion()
	if err != nil || next == *stamp {
		return err
	}
	if err := s.exec("BEGIN"); err != nil {
		return err
	}
	candidate := &SQLiteStorage{storeCore: &storeCore{state: initialState(), limits: s.limits, ordinal: 1}, db: s.db, prefix: s.prefix, implicitRoot: s.implicitRoot, upstreamSchema: s.upstreamSchema}
	if err := candidate.readSQLitePrefix(); err != nil {
		return errors.Join(err, s.exec("ROLLBACK"))
	}
	if err := s.exec("COMMIT"); err != nil {
		return errors.Join(err, s.exec("ROLLBACK"))
	}
	s.state, s.ordinal = candidate.state, candidate.ordinal
	*stamp = next
	return nil
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
			var record commitRecord
			if decodeStrict(data, s.limits, s.limits.MaxFramePayloadBytes, &record) != nil {
				return ErrCorrupt
			}
			// A commit frame may raise the allocator only to its own highest
			// explicit ID; any other high-water value is corruption.
			next, err := prepareExplicit(s.state, record, s.limits)
			if err != nil || next.HighWater != uint64(high) {
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
	if err := s.insertFrames(frames); err != nil {
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
	if s.readOnly {
		return ErrReadOnlyStorage
	}
	if s.upstreamSchema {
		return s.exec("PRAGMA wal_checkpoint(TRUNCATE)")
	}
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
