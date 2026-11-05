package librarydb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"tinglan/native/internal/music"
)

// SplitIdentity gives one source identity its own canonical track again.
//
// Import merges a new source track into an existing row when the matcher says
// they are the same recording. When that judgement was wrong (a chorus cut, a
// re-recorded edition, a different artist), the identity and its binding can be
// moved to a fresh track without touching the other source links.
func (d *DB) SplitIdentity(ctx context.Context, profileID string, identityID int64) (string, error) {
	if profileID == "" {
		profileID = DefaultProfile
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var (
		source, sourceTrackID, homeID string
		importSeq                     int64
		title, artistsJSON, albumJSON string
		versionJSON, versionKey       string
		coverURL                      string
		durationMS                    sql.NullInt64
	)
	err = tx.QueryRowContext(ctx, `SELECT source, source_track_id, home_track_id, import_seq,
		title, artists_json, album_json, version_json, version_key, COALESCE(cover_url,''), duration_ms
		FROM source_identities WHERE id=?`, identityID).
		Scan(&source, &sourceTrackID, &homeID, &importSeq, &title, &artistsJSON, &albumJSON, &versionJSON, &versionKey, &coverURL, &durationMS)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("librarydb: identity %d not found", identityID)
	}
	if err != nil {
		return "", err
	}
	var artists []music.Artist
	_ = json.Unmarshal([]byte(artistsJSON), &artists)
	var album *music.Album
	_ = json.Unmarshal([]byte(albumJSON), &album)
	var version music.Version
	_ = json.Unmarshal([]byte(versionJSON), &version)
	var albumTitle string
	if album != nil {
		albumTitle = album.Title
	}
	if detected := music.DetectVersion(title, albumTitle); detected.Key() != version.Key() {
		version = detected
	}
	now := time.Now().UTC()
	track := music.Track{
		Source: music.SourceID(source), ID: sourceTrackID, Title: title,
		Artists: artists, Album: album, CoverURL: coverURL, Version: version,
	}
	if durationMS.Valid {
		seconds := float64(durationMS.Int64) / 1000
		track.Duration = &seconds
	}
	newID := music.CanonicalID(track)
	if newID == homeID {
		return homeID, nil
	}
	row := music.NewCanonical(track, importSeq, now)
	row.ID = newID
	row.Links = []music.SourceLink{music.Link(track, importSeq, now)}
	if err := upsertTrack(ctx, tx, row); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE source_identities SET home_track_id=?, updated_at=? WHERE id=?`,
		newID, now.Format(time.RFC3339Nano), identityID); err != nil {
		return "", err
	}
	var position int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position)+1,0) FROM favorites WHERE profile_id=?`, profileID).Scan(&position); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO favorites(profile_id, track_id, position, added_at)
		VALUES(?,?,?,?) ON CONFLICT(profile_id,track_id) DO NOTHING`,
		profileID, newID, position, now.Format(time.RFC3339Nano)); err != nil {
		return "", err
	}
	if _, err := BumpRevision(ctx, tx); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return newID, nil
}

// SplitIncompatibleMerges finds tracks that carry source identities with
// different versions (studio vs chorus vs reborn) and splits every identity
// after the first one into its own track. It returns the number of splits.
func (d *DB) SplitIncompatibleMerges(ctx context.Context, profileID string) (int, error) {
	if profileID == "" {
		profileID = DefaultProfile
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT i.id, i.home_track_id, i.title, i.album_json
		FROM source_identities i
		JOIN tracks t ON t.id=i.home_track_id
		WHERE t.merged_into IS NULL
		ORDER BY i.home_track_id, i.import_seq, i.id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	first := map[string]string{}
	var split []int64
	for rows.Next() {
		var id int64
		var home, title, albumJSON string
		if err := rows.Scan(&id, &home, &title, &albumJSON); err != nil {
			return 0, err
		}
		var album *music.Album
		_ = json.Unmarshal([]byte(albumJSON), &album)
		albumTitle := ""
		if album != nil {
			albumTitle = album.Title
		}
		versionKey := music.DetectVersion(title, albumTitle).Key()
		if want, ok := first[home]; ok {
			if want != versionKey {
				split = append(split, id)
			}
			continue
		}
		first[home] = versionKey
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range split {
		if _, err := d.SplitIdentity(ctx, profileID, id); err != nil {
			return 0, err
		}
	}
	return len(split), nil
}
