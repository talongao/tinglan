package librarydb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"tinglan/native/internal/music"
)

// Playlist is one local or imported playlist row.
type Playlist struct {
	ID          string
	ProfileID   string
	Kind        string // local|imported
	Source      string
	RemoteID    string
	Name        string
	Description string
	CoverURL    string
	TrackCount  int
	Revision    int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	SyncedAt    time.Time
}

// Local reports whether the playlist accepts local edits.
func (p Playlist) Local() bool { return p.Kind == "local" }

// CreateLocalPlaylist inserts one user-created playlist.
func (d *DB) CreateLocalPlaylist(ctx context.Context, profileID, name, description string) (Playlist, error) {
	if profileID == "" {
		profileID = DefaultProfile
	}
	now := time.Now().Unix()
	id := newID("pl")
	if _, err := d.sql.ExecContext(ctx, `
		INSERT INTO playlists(id,profile_id,kind,source,source_playlist_id,name,description,cover_url,
			track_count,revision,created_at,updated_at,last_synced_at)
		VALUES(?,?,'local',NULL,NULL,?,?,'',0,1,?,?,NULL)`,
		id, profileID, name, description, now, now); err != nil {
		return Playlist{}, fmt.Errorf("librarydb: create playlist: %w", err)
	}
	return d.Playlist(ctx, id)
}

// EnsureImportedPlaylist returns the single local copy of one remote playlist,
// creating it when missing. The copy is shared across accounts: the last update
// mirrors whichever account is signed in.
func (d *DB) EnsureImportedPlaylist(ctx context.Context, profileID, source, remoteID, name, description, coverURL string) (Playlist, error) {
	if profileID == "" {
		profileID = DefaultProfile
	}
	now := time.Now().Unix()
	if _, err := d.sql.ExecContext(ctx, `
		INSERT INTO playlists(id,profile_id,kind,source,source_playlist_id,name,description,cover_url,
			track_count,revision,created_at,updated_at,last_synced_at)
		VALUES(?,?,'imported',?,?,?,?,?,0,1,?,?,NULL)
		ON CONFLICT(profile_id,source,source_playlist_id) WHERE kind='imported' DO UPDATE SET
			name=excluded.name,description=excluded.description,cover_url=excluded.cover_url,updated_at=excluded.updated_at`,
		newID("pl"), profileID, source, remoteID, name, description, coverURL, now, now); err != nil {
		return Playlist{}, fmt.Errorf("librarydb: ensure imported playlist: %w", err)
	}
	return d.ImportedPlaylist(ctx, profileID, source, remoteID)
}

// ImportedPlaylist looks up the local copy of a remote playlist.
func (d *DB) ImportedPlaylist(ctx context.Context, profileID, source, remoteID string) (Playlist, error) {
	row := d.sql.QueryRowContext(ctx, `
		SELECT id,profile_id,kind,COALESCE(source,''),COALESCE(source_playlist_id,''),name,description,
		       cover_url,track_count,revision,created_at,updated_at,COALESCE(last_synced_at,0)
		FROM playlists WHERE profile_id=? AND kind='imported' AND source=? AND source_playlist_id=?`,
		profileID, source, remoteID)
	return scanPlaylist(row)
}

// Playlist loads one playlist by local id.
func (d *DB) Playlist(ctx context.Context, id string) (Playlist, error) {
	row := d.sql.QueryRowContext(ctx, `
		SELECT id,profile_id,kind,COALESCE(source,''),COALESCE(source_playlist_id,''),name,description,
		       cover_url,track_count,revision,created_at,updated_at,COALESCE(last_synced_at,0)
		FROM playlists WHERE id=?`, id)
	return scanPlaylist(row)
}

// ErrPlaylistNotFound reports a missing playlist row.
var ErrPlaylistNotFound = errors.New("librarydb: playlist not found")

func scanPlaylist(row *sql.Row) (Playlist, error) {
	var p Playlist
	var created, updated, synced int64
	if err := row.Scan(&p.ID, &p.ProfileID, &p.Kind, &p.Source, &p.RemoteID, &p.Name, &p.Description,
		&p.CoverURL, &p.TrackCount, &p.Revision, &created, &updated, &synced); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Playlist{}, ErrPlaylistNotFound
		}
		return Playlist{}, fmt.Errorf("librarydb: scan playlist: %w", err)
	}
	p.CreatedAt = time.Unix(created, 0).UTC()
	p.UpdatedAt = time.Unix(updated, 0).UTC()
	if synced > 0 {
		p.SyncedAt = time.Unix(synced, 0).UTC()
	}
	return p, nil
}

// Playlists lists every playlist of one profile, local first then imported.
func (d *DB) Playlists(ctx context.Context, profileID string) ([]Playlist, error) {
	if profileID == "" {
		profileID = DefaultProfile
	}
	rows, err := d.sql.QueryContext(ctx, `
		SELECT id,profile_id,kind,COALESCE(source,''),COALESCE(source_playlist_id,''),name,description,
		       cover_url,track_count,revision,created_at,updated_at,COALESCE(last_synced_at,0)
		FROM playlists WHERE profile_id=?
		ORDER BY CASE kind WHEN 'local' THEN 0 ELSE 1 END, updated_at DESC, id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("librarydb: list playlists: %w", err)
	}
	defer rows.Close()
	var out []Playlist
	for rows.Next() {
		var p Playlist
		var created, updated, synced int64
		if err := rows.Scan(&p.ID, &p.ProfileID, &p.Kind, &p.Source, &p.RemoteID, &p.Name, &p.Description,
			&p.CoverURL, &p.TrackCount, &p.Revision, &created, &updated, &synced); err != nil {
			return nil, fmt.Errorf("librarydb: scan playlist: %w", err)
		}
		p.CreatedAt = time.Unix(created, 0).UTC()
		p.UpdatedAt = time.Unix(updated, 0).UTC()
		if synced > 0 {
			p.SyncedAt = time.Unix(synced, 0).UTC()
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PlaylistTracks returns the canonical IDs of one playlist in order.
func (d *DB) PlaylistTracks(ctx context.Context, playlistID string) ([]string, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT track_id FROM playlist_tracks WHERE playlist_id=? ORDER BY position, added_at, track_id`, playlistID)
	if err != nil {
		return nil, fmt.Errorf("librarydb: playlist tracks: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// Repair refs written before un-favoriting stopped deleting canonical rows:
	// follow merged_into and drop IDs whose track no longer exists, so the card
	// count and the detail list cannot disagree.
	resolved, err := d.resolveTrackIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	valid := make([]string, 0, len(resolved))
	seen := make(map[string]bool, len(resolved))
	changed := false
	for i, id := range resolved {
		if seen[id] {
			changed = true
			continue
		}
		var exists int
		err := d.sql.QueryRowContext(ctx, `SELECT 1 FROM tracks WHERE id=?`, id).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			changed = true
			continue
		}
		if err != nil {
			return nil, err
		}
		if id != ids[i] {
			changed = true
		}
		valid = append(valid, id)
		seen[id] = true
	}
	if changed {
		if err := d.repairPlaylistRefs(ctx, playlistID, valid); err != nil {
			return nil, err
		}
	}
	return valid, nil
}

// repairPlaylistRefs rewrites one playlist's member rows to the resolved track
// IDs and keeps track_count in step.
func (d *DB) repairPlaylistRefs(ctx context.Context, playlistID string, ids []string) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_tracks WHERE playlist_id=?`, playlistID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM member_refs WHERE owner_kind='playlist' AND owner_id=?`, playlistID); err != nil {
		return err
	}
	now := time.Now().Unix()
	for position, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO playlist_tracks(playlist_id,track_id,position,added_at) VALUES(?,?,?,?)`,
			playlistID, id, position, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO member_refs(owner_kind,owner_id,ref_key,anchor_track_id,position,added_at,revision)
			 VALUES('playlist',?,?,?,?,?,1)`,
			playlistID, "track:"+id, id, position, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE playlists SET track_count=?, revision=revision+1, updated_at=? WHERE id=?`,
		len(ids), now, playlistID); err != nil {
		return err
	}
	return tx.Commit()
}

// AddPlaylistTrack appends one track to a local playlist. Imported playlists
// are read-only mirrors and reject local edits.
func (d *DB) AddPlaylistTrack(ctx context.Context, playlistID, trackID string) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kind string
	if err := tx.QueryRowContext(ctx, `SELECT kind FROM playlists WHERE id=?`, playlistID).Scan(&kind); err != nil {
		return fmt.Errorf("librarydb: add to playlist: %w", err)
	}
	if kind != "local" {
		return errors.New("librarydb: imported playlists are read-only")
	}
	var next int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(position)+1,0) FROM playlist_tracks WHERE playlist_id=?`, playlistID).Scan(&next); err != nil {
		return err
	}
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO playlist_tracks(playlist_id,track_id,position,added_at) VALUES(?,?,?,?)
		 ON CONFLICT(playlist_id,track_id) DO NOTHING`, playlistID, trackID, next, now); err != nil {
		return fmt.Errorf("librarydb: insert playlist track: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO member_refs(owner_kind,owner_id,ref_key,anchor_track_id,position,added_at,revision)
		 VALUES('playlist',?,?,?,?,?,1)
		 ON CONFLICT(owner_kind,owner_id,ref_key) DO NOTHING`,
		playlistID, "track:"+trackID, trackID, next, now); err != nil {
		return fmt.Errorf("librarydb: insert playlist ref: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE playlists SET track_count=(SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id=?),
			revision=revision+1, updated_at=? WHERE id=?`, playlistID, now, playlistID); err != nil {
		return err
	}
	return tx.Commit()
}

// RemovePlaylistTrack removes one member from a local playlist.
func (d *DB) RemovePlaylistTrack(ctx context.Context, playlistID, trackID string) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kind string
	if err := tx.QueryRowContext(ctx, `SELECT kind FROM playlists WHERE id=?`, playlistID).Scan(&kind); err != nil {
		return err
	}
	if kind != "local" {
		return errors.New("librarydb: imported playlists are read-only")
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM playlist_tracks WHERE playlist_id=? AND track_id=?`, playlistID, trackID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM member_refs WHERE owner_kind='playlist' AND owner_id=? AND ref_key=?`,
		playlistID, "track:"+trackID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE playlists SET track_count=(SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id=?),
			revision=revision+1, updated_at=? WHERE id=?`, playlistID, time.Now().Unix(), playlistID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeletePlaylist hard-deletes a playlist and its members. Canonical tracks and
// favorites are untouched.
func (d *DB) DeletePlaylist(ctx context.Context, playlistID string) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_tracks WHERE playlist_id=?`, playlistID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM member_refs WHERE owner_kind='playlist' AND owner_id=?`, playlistID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE imports SET local_playlist_id=NULL, status='deleted', updated_at=? WHERE local_playlist_id=?`,
		time.Now().Unix(), playlistID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM playlists WHERE id=?`, playlistID); err != nil {
		return err
	}
	return tx.Commit()
}

// RenamePlaylist updates a local playlist's name and description.
func (d *DB) RenamePlaylist(ctx context.Context, playlistID, name, description string) error {
	if _, err := d.sql.ExecContext(ctx,
		`UPDATE playlists SET name=?,description=?,revision=revision+1,updated_at=?
		 WHERE id=? AND kind='local'`, name, description, time.Now().Unix(), playlistID); err != nil {
		return fmt.Errorf("librarydb: rename playlist: %w", err)
	}
	return nil
}

// ReplacePlaylistMembers mirrors an imported playlist snapshot in one
// transaction: members are replaced, the count and sync time updated.
func (d *DB) ReplacePlaylistMembers(ctx context.Context, playlistID string, trackIDs []string, name, description, coverURL string) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kind string
	if err := tx.QueryRowContext(ctx, `SELECT kind FROM playlists WHERE id=?`, playlistID).Scan(&kind); err != nil {
		return fmt.Errorf("librarydb: replace members: %w", err)
	}
	if kind != "imported" {
		return errors.New("librarydb: replace members requires an imported playlist")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_tracks WHERE playlist_id=?`, playlistID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM member_refs WHERE owner_kind='playlist' AND owner_id=?`, playlistID); err != nil {
		return err
	}
	now := time.Now().Unix()
	// A remote playlist may list the same song twice (or two remote tracks may
	// merge into one canonical row). Member refs are keyed by track, so keep the
	// first occurrence instead of failing the whole mirror.
	seen := make(map[string]bool, len(trackIDs))
	unique := trackIDs[:0:0]
	for _, id := range trackIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	trackIDs = unique
	for position, trackID := range trackIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO playlist_tracks(playlist_id,track_id,position,added_at) VALUES(?,?,?,?)
			 ON CONFLICT(playlist_id,track_id) DO NOTHING`, playlistID, trackID, position, now); err != nil {
			return fmt.Errorf("librarydb: mirror member: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO member_refs(owner_kind,owner_id,ref_key,anchor_track_id,position,added_at,revision)
			 VALUES('playlist',?,?,?,?,?,1)`,
			playlistID, "track:"+trackID, trackID, position, now); err != nil {
			return fmt.Errorf("librarydb: mirror ref: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE playlists SET name=?,description=?,cover_url=?,track_count=?,revision=revision+1,
			updated_at=?,last_synced_at=? WHERE id=?`,
		name, description, coverURL, len(trackIDs), now, now, playlistID); err != nil {
		return fmt.Errorf("librarydb: update playlist meta: %w", err)
	}
	return tx.Commit()
}

// TracksByID returns canonical rows for the given IDs, preserving the input
// order and skipping missing rows.
func (d *DB) TracksByID(ctx context.Context, ids []string) ([]music.CanonicalTrack, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, _, err := d.Load(ctx, DefaultProfile)
	if err != nil {
		return nil, err
	}
	index := make(map[string]music.CanonicalTrack, len(rows))
	for _, row := range rows {
		index[row.Track.ID] = row.Track
	}
	// Resolve historic IDs before looking them up: a track that lost a merge
	// still has rows in playlist_tracks/favorites, and dropping those silently
	// made a 5-song playlist render 3 rows.
	resolved, err := d.resolveTrackIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]music.CanonicalTrack, 0, len(resolved))
	seen := make(map[string]bool, len(resolved))
	for _, id := range resolved {
		if seen[id] {
			continue
		}
		if track, ok := index[id]; ok {
			out = append(out, track)
			seen[id] = true
		}
	}
	return out, nil
}

// UpdateImportedPlaylistMeta refreshes the local copy's display metadata from
// a remote listing. It never creates a row: playlists the user has not
// imported stay remote-only. Passing an empty cover keeps the stored one, so a
// listing without artwork cannot erase a previously known cover.
func (d *DB) UpdateImportedPlaylistMeta(ctx context.Context, profileID, source, remoteID, name, description, coverURL string) error {
	if profileID == "" {
		profileID = DefaultProfile
	}
	if remoteID == "" {
		return nil
	}
	_, err := d.sql.ExecContext(ctx, `
		UPDATE playlists SET
			name=CASE WHEN ?<>'' THEN ? ELSE name END,
			description=CASE WHEN ?<>'' THEN ? ELSE description END,
			cover_url=CASE WHEN ?<>'' THEN ? ELSE cover_url END,
			updated_at=?
		WHERE profile_id=? AND kind='imported' AND source=? AND source_playlist_id=?`,
		name, name, description, description, coverURL, coverURL, time.Now().Unix(),
		profileID, source, remoteID)
	if err != nil {
		return fmt.Errorf("librarydb: update imported playlist meta: %w", err)
	}
	return nil
}
