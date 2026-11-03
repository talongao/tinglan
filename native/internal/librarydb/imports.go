package librarydb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"tinglan/native/internal/music"
)

// Import status values mirror the CHECK constraint on imports.status.
const (
	ImportNever    = "never"
	ImportImported = "imported"
	ImportUpdated  = "updated"
	ImportFailed   = "failed"
	ImportExpired  = "expired"
	ImportDeleted  = "deleted"
)

// Run states mirror import_runs.state.
const (
	RunFetching  = "fetching"
	RunReady     = "ready"
	RunCommitted = "committed"
	RunFailed    = "failed"
	RunCancelled = "cancelled"
	RunSuperseded = "superseded"
)

// ImportRecord is one row of imports.
type ImportRecord struct {
	ID                 string
	ProfileID          string
	AccountID          string
	Source             string
	Kind               string
	RemoteCollectionID string
	LocalPlaylistID    string
	Status             string
	AddedCount         int
	UpdatedCount       int
	LastSuccessAt      time.Time
}

// RemoteKey encodes the imports.remote_collection_id JSON pair.
func RemoteKey(source, remoteID string) string {
	raw, _ := json.Marshal([]string{source, remoteID})
	return string(raw)
}

// EnsureImport returns the imports row for one remote collection, creating it
// with status "never" on first use. accountID records the last account that
// touched the row; it is not an ownership gate.
func (d *DB) EnsureImport(ctx context.Context, profileID, accountID, source, kind, remoteID, localPlaylistID string) (ImportRecord, error) {
	if profileID == "" {
		profileID = DefaultProfile
	}
	key := RemoteKey(source, remoteID)
	if _, err := d.sql.ExecContext(ctx, `
		INSERT INTO imports(id,profile_id,account_id,source,kind,remote_collection_id,local_playlist_id,status,target_generation,updated_at)
		VALUES(?,?,?,?,?,?,?, 'never', 0, ?)
		ON CONFLICT(profile_id,kind,remote_collection_id) DO NOTHING`,
		newID("imp"), profileID, accountID, source, kind, key, nullString(localPlaylistID), time.Now().Unix()); err != nil {
		return ImportRecord{}, fmt.Errorf("librarydb: ensure import: %w", err)
	}
	row := d.sql.QueryRowContext(ctx, `
		SELECT id,profile_id,COALESCE(account_id,''),source,kind,remote_collection_id,
		       COALESCE(local_playlist_id,''),status,added_count,updated_count
		FROM imports WHERE profile_id=? AND kind=? AND remote_collection_id=?`,
		profileID, kind, key)
	var rec ImportRecord
	if err := row.Scan(&rec.ID, &rec.ProfileID, &rec.AccountID, &rec.Source, &rec.Kind,
		&rec.RemoteCollectionID, &rec.LocalPlaylistID, &rec.Status, &rec.AddedCount, &rec.UpdatedCount); err != nil {
		return ImportRecord{}, fmt.Errorf("librarydb: read import: %w", err)
	}
	return rec, nil
}

// BeginRun opens a fetching run for an import and supersedes any stale live run.
func (d *DB) BeginRun(ctx context.Context, imp ImportRecord, accountID string, accountGeneration int64) (string, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE import_runs SET state='superseded', finished_at=?
		WHERE import_id=? AND state IN ('fetching','ready')`,
		time.Now().Unix(), imp.ID); err != nil {
		return "", fmt.Errorf("librarydb: supersede run: %w", err)
	}
	runID := newID("run")
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO import_runs(id,import_id,account_id,account_generation,target_generation,state,
			outcome,cursor,unique_count,started_at)
		VALUES(?,?,?,?,0,'fetching','snapshot','',0,?)`,
		runID, imp.ID, accountID, accountGeneration, now); err != nil {
		return "", fmt.Errorf("librarydb: begin run: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE imports SET last_attempt_at=?, status=CASE WHEN status='never' THEN 'never' ELSE status END, updated_at=?
		WHERE id=?`, now, now, imp.ID); err != nil {
		return "", fmt.Errorf("librarydb: touch import: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return runID, nil
}

// StageTrack records one fetched track in the run staging set.
func (d *DB) StageTrack(ctx context.Context, runID string, ordinal int, track music.Track) error {
	meta, err := json.Marshal(track)
	if err != nil {
		return err
	}
	digest := metadataDigest(music.Link(track, 0, time.Now()))
	if _, err := d.sql.ExecContext(ctx, `
		INSERT INTO import_stage(run_id,source_track_id,ordinal,metadata_json,metadata_digest)
		VALUES(?,?,?,?,?)
		ON CONFLICT(run_id,source_track_id) DO UPDATE SET ordinal=excluded.ordinal,
			metadata_json=excluded.metadata_json,metadata_digest=excluded.metadata_digest`,
		runID, track.ID, ordinal, string(meta), digest); err != nil {
		return fmt.Errorf("librarydb: stage track: %w", err)
	}
	return nil
}

// SetRunState updates one run's terminal state.
func (d *DB) SetRunState(ctx context.Context, runID, state, errorCode string) error {
	finished := time.Now().Unix()
	if _, err := d.sql.ExecContext(ctx,
		`UPDATE import_runs SET state=?, error_code=?, finished_at=? WHERE id=?`,
		state, errorCode, finished, runID); err != nil {
		return fmt.Errorf("librarydb: set run state: %w", err)
	}
	return nil
}

// PublishInput is the single-transaction publish of a completed favorites
// snapshot.
type PublishInput struct {
	ProfileID string
	ImportID  string
	RunID     string
	AccountID string
	Source    string
	Rows      []music.CanonicalTrack
	// Seen holds the source identities present in this snapshot. Links that are
	// absent keep their rows and favorites membership but lose the origin.
	Seen    map[string]bool
	Added   int
	Updated int
}

// PublishFavorites writes the whole canonical document, favorites membership and
// favorites origins in one transaction. Only a run that reached this point can
// change the library; a failed or cancelled run leaves everything untouched.
func (d *DB) PublishFavorites(ctx context.Context, in PublishInput) error {
	if in.ProfileID == "" {
		in.ProfileID = DefaultProfile
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for position, row := range in.Rows {
		if err := upsertTrack(ctx, tx, row); err != nil {
			return err
		}
		for _, link := range row.Links {
			if in.Seen != nil && !in.Seen[link.Source.String()+"\x00"+link.TrackID] {
				continue
			}
			origin := originRef{Kind: "favorites", Key: in.ImportID, Run: in.RunID}
			if err := d.putLink(ctx, tx, row.ID, link, origin); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO favorites(profile_id,track_id,position,added_at) VALUES(?,?,?,?)
			 ON CONFLICT(profile_id,track_id) DO UPDATE SET position=excluded.position`,
			in.ProfileID, row.ID, position, row.CreatedAt.Unix()); err != nil {
			return fmt.Errorf("librarydb: publish favorite: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO member_refs(owner_kind,owner_id,ref_key,anchor_track_id,position,added_at,revision)
			 VALUES('favorites',?,?,?,?,?,1)
			 ON CONFLICT(owner_kind,owner_id,ref_key) DO UPDATE SET position=excluded.position`,
			in.ProfileID, "favorite:"+row.ID, row.ID, position, row.CreatedAt.Unix()); err != nil {
			return fmt.Errorf("librarydb: publish member ref: %w", err)
		}
	}

	// Origins of this collection that the run did not see are no longer current.
	if _, err := tx.ExecContext(ctx, `
		UPDATE binding_origins SET active=0, updated_at=?
		WHERE origin_kind='favorites' AND origin_key=? AND COALESCE(last_seen_run,'')<>?`,
		time.Now().Unix(), in.ImportID, in.RunID); err != nil {
		return fmt.Errorf("librarydb: deactivate origins: %w", err)
	}
	// A binding with no active origin is no longer playable.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM source_bindings
		WHERE account_id=? AND id NOT IN (SELECT binding_id FROM binding_origins WHERE active=1)`,
		in.AccountID); err != nil {
		return fmt.Errorf("librarydb: prune bindings: %w", err)
	}

	now := time.Now().Unix()
	status := ImportImported
	if in.Updated > 0 || in.Added > 0 {
		status = ImportUpdated
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE imports SET account_id=?, status=?, added_count=?, updated_count=?,
			last_success_at=?, error_code='', updated_at=?
		WHERE id=?`,
		in.AccountID, status, in.Added, in.Updated, now, now, in.ImportID); err != nil {
		return fmt.Errorf("librarydb: update import: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE import_runs SET state='committed', unique_count=?, added_count=?, updated_count=?,
			committed_revision=(SELECT revision FROM library_clock WHERE singleton=1), finished_at=?
		WHERE id=?`,
		len(in.Rows), in.Added, in.Updated, now, in.RunID); err != nil {
		return fmt.Errorf("librarydb: commit run: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE library_clock SET revision=revision+1 WHERE singleton=1`); err != nil {
		return fmt.Errorf("librarydb: bump revision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

type originRef struct {
	Kind string
	Key  string
	Run  string
}

func newID(prefix string) string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(raw[:])
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

var errNoAccount = errors.New("librarydb: no account")
var _ = sql.ErrNoRows
var _ = errNoAccount

// MarkImportFailed records a failed attempt without touching published data.
func (d *DB) MarkImportFailed(ctx context.Context, importID string) error {
	if _, err := d.sql.ExecContext(ctx,
		`UPDATE imports SET status=CASE WHEN status='never' THEN 'never' ELSE 'failed' END,
			error_code='import_failed', updated_at=? WHERE id=?`,
		time.Now().Unix(), importID); err != nil {
		return fmt.Errorf("librarydb: mark import failed: %w", err)
	}
	return nil
}

// ImportByKey returns the imports row for one remote collection, or a zero
// record with status "never" when the collection was never imported.
func (d *DB) ImportByKey(ctx context.Context, profileID, source, kind, remoteID string) (ImportRecord, bool, error) {
	if profileID == "" {
		profileID = DefaultProfile
	}
	row := d.sql.QueryRowContext(ctx, `
		SELECT id,profile_id,COALESCE(account_id,''),source,kind,remote_collection_id,
		       COALESCE(local_playlist_id,''),status,added_count,updated_count,last_success_at
		FROM imports WHERE profile_id=? AND kind=? AND remote_collection_id=?`,
		profileID, kind, RemoteKey(source, remoteID))
	var rec ImportRecord
	var lastSuccess sql.NullInt64
	err := row.Scan(&rec.ID, &rec.ProfileID, &rec.AccountID, &rec.Source, &rec.Kind,
		&rec.RemoteCollectionID, &rec.LocalPlaylistID, &rec.Status, &rec.AddedCount, &rec.UpdatedCount, &lastSuccess)
	if errors.Is(err, sql.ErrNoRows) {
		return ImportRecord{ProfileID: profileID, Source: source, Kind: kind, Status: ImportNever}, false, nil
	}
	if err != nil {
		return ImportRecord{}, false, fmt.Errorf("librarydb: read import: %w", err)
	}
	if lastSuccess.Valid {
		rec.LastSuccessAt = time.Unix(lastSuccess.Int64, 0).UTC()
	}
	return rec, true, nil
}

// LastCommittedRun reports the account and time of the newest committed run for
// one import. The UI compares it with the current account to warn before an
// imported playlist or favorites snapshot is overwritten.
func (d *DB) LastCommittedRun(ctx context.Context, importID string) (string, time.Time, bool, error) {
	var accountID string
	var finished sql.NullInt64
	err := d.sql.QueryRowContext(ctx, `
		SELECT account_id, finished_at FROM import_runs
		WHERE import_id=? AND state='committed'
		ORDER BY finished_at DESC, started_at DESC LIMIT 1`, importID).Scan(&accountID, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, fmt.Errorf("librarydb: read last run: %w", err)
	}
	if finished.Valid {
		return accountID, time.Unix(finished.Int64, 0).UTC(), true, nil
	}
	return accountID, time.Time{}, true, nil
}
