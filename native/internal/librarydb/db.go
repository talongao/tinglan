// Package librarydb owns the SQLite library: tracks, source identities and
// bindings, favorites, playlists and import journals.
//
// The database is the single truth for the local library. Source accounts only
// contribute bindings; signing out never deletes a canonical row or a favorite.
package librarydb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaFS embed.FS

const (
	// FileName is the database file inside the profile directory.
	FileName = "library.sqlite3"
	// SchemaVersion is the DDL version recorded in schema_migrations.
	SchemaVersion = 1
	// DefaultProfile is the single profile used by the desktop app.
	DefaultProfile = "default"
)

// DB is a handle to the library database. All writes are serialized by SQLite
// (one connection) so callers never see SQLITE_BUSY.
type DB struct {
	mu   sync.Mutex
	sql  *sql.DB
	path string
	now  func() time.Time
}

// Open opens (or creates) the library database in dir. The file is restricted
// to 0600 because it contains account labels and library metadata.
func Open(dir string) (*DB, error) {
	if dir == "" {
		return nil, errors.New("librarydb: empty profile dir")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("librarydb: create profile dir: %w", err)
	}
	path := filepath.Join(dir, FileName)
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(0)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("librarydb: open: %w", err)
	}
	// One connection keeps BEGIN IMMEDIATE deterministic and avoids lock
	// contention between the UI thread and the import worker.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("librarydb: ping: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("librarydb: restrict database: %w", err)
	}
	db := &DB{sql: sqlDB, path: path, now: time.Now}
	if err := db.migrate(ctx); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if err := db.seed(ctx); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// Close releases the database handle.
func (d *DB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sql == nil {
		return nil
	}
	err := d.sql.Close()
	d.sql = nil
	return err
}

// Path reports the database file path.
func (d *DB) Path() string { return d.path }

// migrate applies the embedded DDL once and records its digest.
func (d *DB) migrate(ctx context.Context) error {
	ddl, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("librarydb: read schema: %w", err)
	}
	sum := sha256.Sum256(ddl)
	digest := hex.EncodeToString(sum[:])
	var exists int
	if err := d.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&exists); err != nil {
		return fmt.Errorf("librarydb: inspect schema: %w", err)
	}
	if exists == 0 {
		return d.applySchema(ctx, ddl, digest)
	}
	var applied string
	err = d.sql.QueryRowContext(ctx, `SELECT ddl_digest FROM schema_migrations WHERE version=?`, SchemaVersion).Scan(&applied)
	switch {
	case err == nil:
		if applied != digest {
			return fmt.Errorf("librarydb: schema %d digest mismatch (database was written by a different build)", SchemaVersion)
		}
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("librarydb: read schema version: %w", err)
	}
	return d.applySchema(ctx, ddl, digest)
}

func (d *DB) applySchema(ctx context.Context, ddl []byte, digest string) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("librarydb: begin schema: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, string(ddl)); err != nil {
		return fmt.Errorf("librarydb: apply schema: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations(version,ddl_digest,applied_at) VALUES(?,?,?)`,
		SchemaVersion, digest, d.now().Unix()); err != nil {
		return fmt.Errorf("librarydb: record schema: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("librarydb: commit schema: %w", err)
	}
	return nil
}

// seed makes sure the default profile row exists.
func (d *DB) seed(ctx context.Context) error {
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO profiles(id,name,created_at) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING`,
		DefaultProfile, "本地", d.now().Unix())
	if err != nil {
		return fmt.Errorf("librarydb: seed profile: %w", err)
	}
	return nil
}

// Clock returns the current library revision and import sequence.
func (d *DB) Clock(ctx context.Context) (revision, importSeq int64, err error) {
	err = d.sql.QueryRowContext(ctx,
		`SELECT revision,import_seq FROM library_clock WHERE singleton=1`).Scan(&revision, &importSeq)
	if err != nil {
		return 0, 0, fmt.Errorf("librarydb: read clock: %w", err)
	}
	return revision, importSeq, nil
}

// NextImportSeq allocates one import sequence inside tx.
func NextImportSeq(ctx context.Context, tx *sql.Tx) (int64, error) {
	if _, err := tx.ExecContext(ctx,
		`UPDATE library_clock SET import_seq=import_seq+1 WHERE singleton=1`); err != nil {
		return 0, fmt.Errorf("librarydb: bump import seq: %w", err)
	}
	var seq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT import_seq FROM library_clock WHERE singleton=1`).Scan(&seq); err != nil {
		return 0, fmt.Errorf("librarydb: read import seq: %w", err)
	}
	return seq, nil
}

// BumpRevision advances the coarse library revision inside tx.
func BumpRevision(ctx context.Context, tx *sql.Tx) (int64, error) {
	if _, err := tx.ExecContext(ctx,
		`UPDATE library_clock SET revision=revision+1 WHERE singleton=1`); err != nil {
		return 0, fmt.Errorf("librarydb: bump revision: %w", err)
	}
	var rev int64
	if err := tx.QueryRowContext(ctx,
		`SELECT revision FROM library_clock WHERE singleton=1`).Scan(&rev); err != nil {
		return 0, fmt.Errorf("librarydb: read revision: %w", err)
	}
	return rev, nil
}

// Account is one signed-in source account.
type Account struct {
	ID                string
	Source            string
	Key               string
	Label             string
	State             string
	IsCurrent         bool
	SessionGeneration int64
}

// EnsureAccount upserts a source account and marks it current.
func (d *DB) EnsureAccount(ctx context.Context, source, key, label string) (Account, error) {
	if source == "" || key == "" {
		return Account{}, errors.New("librarydb: account source and key are required")
	}
	id := AccountID(source, key)
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	now := d.now().Unix()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO source_accounts(id,source,account_key,account_label,state,is_current,session_generation,credential_ref,expires_at,last_verified_at,updated_at)
		 VALUES(?,?,?,?, 'logged_in',0,1,'',NULL,?,?)
		 ON CONFLICT(id) DO UPDATE SET account_label=excluded.account_label,state='logged_in',last_verified_at=excluded.last_verified_at,updated_at=excluded.updated_at`,
		id, source, key, label, now, now); err != nil {
		return Account{}, fmt.Errorf("librarydb: upsert account: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE source_accounts SET is_current=0 WHERE source=? AND is_current=1 AND id<>?`, source, id); err != nil {
		return Account{}, fmt.Errorf("librarydb: clear current account: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE source_accounts SET is_current=1 WHERE id=?`, id); err != nil {
		return Account{}, fmt.Errorf("librarydb: set current account: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Account{}, err
	}
	return d.Account(ctx, source)
}

// Account returns the current account for one source.
func (d *DB) Account(ctx context.Context, source string) (Account, error) {
	var a Account
	var current int
	err := d.sql.QueryRowContext(ctx,
		`SELECT id,source,account_key,account_label,state,is_current,session_generation
		 FROM source_accounts WHERE source=? AND is_current=1 LIMIT 1`, source).
		Scan(&a.ID, &a.Source, &a.Key, &a.Label, &a.State, &current, &a.SessionGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, nil
	}
	if err != nil {
		return Account{}, fmt.Errorf("librarydb: read account: %w", err)
	}
	a.IsCurrent = current == 1
	return a, nil
}

// SetAccountState changes one account's login state. Logging out keeps every
// binding and favorite; playback simply finds no usable account.
func (d *DB) SetAccountState(ctx context.Context, source, state string) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE source_accounts SET state=?,is_current=0,updated_at=? WHERE source=? AND is_current=1`,
		state, d.now().Unix(), source); err != nil {
		return fmt.Errorf("librarydb: update account state: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE source_accounts SET session_generation=session_generation+1 WHERE source=?`, source); err != nil {
		return fmt.Errorf("librarydb: bump session generation: %w", err)
	}
	return tx.Commit()
}

func AccountID(source, key string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + key))
	return "acct_" + hex.EncodeToString(sum[:8])
}

// NextSeqs reserves n import sequence numbers in one transaction and returns
// the highest reserved value. Gaps are allowed: a reserved number that is never
// used does not break ordering.
func (d *DB) NextSeqs(ctx context.Context, n int) (int64, error) {
	if n <= 0 {
		return 0, nil
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE library_clock SET import_seq=import_seq+? WHERE singleton=1`, n); err != nil {
		return 0, fmt.Errorf("librarydb: reserve import seq: %w", err)
	}
	var seq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT import_seq FROM library_clock WHERE singleton=1`).Scan(&seq); err != nil {
		return 0, fmt.Errorf("librarydb: read import seq: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return seq, nil
}
