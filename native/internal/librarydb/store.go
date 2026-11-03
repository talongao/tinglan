package librarydb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"tinglan/native/internal/music"
)

// Row is one canonical track as stored in the library.
type Row struct {
	Track music.CanonicalTrack
	// Favorite is true when the row is in the favorites list.
	Favorite bool
	// Position is the favorites display order.
	Position int
}

// Load returns every canonical row with its source links, ordered by favorites
// position first and then by creation time for rows outside the favorites list.
func (d *DB) Load(ctx context.Context, profileID string) ([]Row, int64, error) {
	if profileID == "" {
		profileID = DefaultProfile
	}
	_, seq, err := d.Clock(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := d.sql.QueryContext(ctx, `
		SELECT t.id,t.title,t.artists_json,t.album_json,t.version_json,t.cover_url,t.duration_ms,
		       t.created_at,t.updated_at,
		       COALESCE(f.position,-1),
		       COALESCE(a.state,'')
		FROM tracks t
		LEFT JOIN favorites f ON f.track_id=t.id AND f.profile_id=?
		LEFT JOIN source_accounts a ON 0
		WHERE t.merged_into IS NULL
		ORDER BY CASE WHEN f.position IS NULL THEN 1 ELSE 0 END, f.position, t.created_at, t.id`,
		profileID)
	if err != nil {
		return nil, 0, fmt.Errorf("librarydb: load tracks: %w", err)
	}
	defer rows.Close()
	var out []Row
	index := make(map[string]int)
	for rows.Next() {
		var row Row
		var artistsJSON, albumJSON, versionJSON string
		var coverURL string
		var duration sql.NullInt64
		var created, updated int64
		var position int
		var accountState string
		if err := rows.Scan(&row.Track.ID, &row.Track.Title, &artistsJSON, &albumJSON, &versionJSON,
			&coverURL, &duration, &created, &updated, &position, &accountState); err != nil {
			return nil, 0, fmt.Errorf("librarydb: scan track: %w", err)
		}
		if err := decodeJSON(artistsJSON, &row.Track.Artists); err != nil {
			return nil, 0, err
		}
		if err := decodeJSON(albumJSON, &row.Track.Album); err != nil {
			return nil, 0, err
		}
		row.Track.Favorite = position >= 0
		if err := decodeJSON(versionJSON, &row.Track.Version); err != nil {
			return nil, 0, err
		}
		row.Track.CoverURL = coverURL
		row.Track.Duration = seconds(duration)
		row.Track.CreatedAt = time.Unix(created, 0).UTC()
		row.Track.UpdatedAt = time.Unix(updated, 0).UTC()
		row.Favorite = position >= 0
		row.Position = position
		index[row.Track.ID] = len(out)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := d.attachLinks(ctx, out, index); err != nil {
		return nil, 0, err
	}
	return out, seq, nil
}

// attachLinks fills each row's source links from identities, bindings and the
// owning account state. A link whose account is signed out stays in place but
// is marked Disabled so playback skips it without deleting local data.
func (d *DB) attachLinks(ctx context.Context, rows []Row, index map[string]int) error {
	links, err := d.sql.QueryContext(ctx, `
		SELECT i.home_track_id,i.source,i.source_track_id,i.import_seq,i.first_imported_at,
		       i.title,i.artists_json,i.album_json,i.version_json,i.cover_url,i.duration_ms,
		       COALESCE(b.access,'unknown'),COALESCE(b.quality,''),COALESCE(a.state,''),b.id IS NOT NULL
		FROM source_identities i
		LEFT JOIN source_bindings b ON b.identity_id=i.id
		LEFT JOIN source_accounts a ON a.id=b.account_id
		ORDER BY i.import_seq,i.source,i.source_track_id,COALESCE(a.is_current,0) DESC,b.id DESC`)
	if err != nil {
		return fmt.Errorf("librarydb: load links: %w", err)
	}
	defer links.Close()
	seenIdentity := make(map[string]bool)
	for links.Next() {
		var homeID string
		var link music.SourceLink
		var artistsJSON, albumJSON, versionJSON, coverURL string
		var duration sql.NullInt64
		var importedAt int64
		var access, quality, state string
		var hasBinding bool
		if err := links.Scan(&homeID, &link.Source, &link.TrackID, &link.ImportSeq, &importedAt,
			&link.Title, &artistsJSON, &albumJSON, &versionJSON, &coverURL, &duration,
			&access, &quality, &state, &hasBinding); err != nil {
			return fmt.Errorf("librarydb: scan link: %w", err)
		}
		if err := decodeJSON(artistsJSON, &link.Artists); err != nil {
			return err
		}
		if err := decodeJSON(albumJSON, &link.Album); err != nil {
			return err
		}
		if err := decodeJSON(versionJSON, &link.Version); err != nil {
			return err
		}
		link.CoverURL = coverURL
		link.Duration = seconds(duration)
		link.ImportedAt = time.Unix(importedAt, 0).UTC()
		link.Access = music.ParseAccess(access)
		link.Quality = quality
		// No binding means the current account can no longer reach this source
		// row; the canonical track and its favorite membership stay.
		link.Disabled = !hasBinding || (state != "" && state != "logged_in")
		// One link per identity: a track imported under two accounts has two
		// bindings, and the current account's row sorts first.
		identityKey := homeID + "\x00" + link.Source.String() + "\x00" + link.TrackID
		if seenIdentity[identityKey] {
			continue
		}
		seenIdentity[identityKey] = true
		if position, ok := index[homeID]; ok {
			rows[position].Track.Links = append(rows[position].Track.Links, link)
		}
	}
	return links.Err()
}

// SaveRow writes one canonical row (metadata, links, favorite membership).
func (d *DB) SaveRow(ctx context.Context, profileID string, row music.CanonicalTrack, favorite bool, position int) error {
	if profileID == "" {
		profileID = DefaultProfile
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := upsertTrack(ctx, tx, row); err != nil {
		return err
	}
	for _, link := range row.Links {
		if err := d.putLink(ctx, tx, row.ID, link, originRef{}); err != nil {
			return err
		}
	}
	row.Favorite = favorite
	if err := saveFavoriteTx(ctx, tx, profileID, row, position); err != nil {
		return err
	}
	return tx.Commit()
}

// saveFavoriteTx adds or clears the favorites membership for one row. Only
// favorites rows live in 我的收藏; 收录音乐 rows stay behind.
func saveFavoriteTx(ctx context.Context, tx *sql.Tx, profileID string, row music.CanonicalTrack, position int) error {
	if !row.Favorite {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM favorites WHERE profile_id=? AND track_id=?`, profileID, row.ID); err != nil {
			return fmt.Errorf("librarydb: clear favorite: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM member_refs WHERE owner_kind='favorites' AND owner_id=? AND ref_key=?`,
			profileID, "favorite:"+row.ID); err != nil {
			return fmt.Errorf("librarydb: clear member ref: %w", err)
		}
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO favorites(profile_id,track_id,position,added_at) VALUES(?,?,?,?)
		 ON CONFLICT(profile_id,track_id) DO UPDATE SET position=excluded.position`,
		profileID, row.ID, position, row.CreatedAt.Unix()); err != nil {
		return fmt.Errorf("librarydb: save favorite: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO member_refs(owner_kind,owner_id,ref_key,anchor_track_id,position,added_at,revision)
		 VALUES('favorites',?,?,?,?,?,1)
		 ON CONFLICT(owner_kind,owner_id,ref_key) DO UPDATE SET position=excluded.position`,
		profileID, "favorite:"+row.ID, row.ID, position, row.CreatedAt.Unix()); err != nil {
		return fmt.Errorf("librarydb: save member ref: %w", err)
	}
	return nil
}

// upsertTrack writes the canonical metadata and the artist index.
func upsertTrack(ctx context.Context, tx *sql.Tx, row music.CanonicalTrack) error {
	artists, err := json.Marshal(nonNilArtists(row.Artists))
	if err != nil {
		return err
	}
	album, err := json.Marshal(row.Album)
	if err != nil {
		return err
	}
	version, err := json.Marshal(row.Version)
	if err != nil {
		return err
	}
	normArtists, err := json.Marshal(music.ArtistSet(row.Artists))
	if err != nil {
		return err
	}
	created := row.CreatedAt.Unix()
	if created <= 0 {
		created = time.Now().Unix()
	}
	updated := row.UpdatedAt.Unix()
	if updated <= 0 {
		updated = created
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tracks(id,title,artists_json,album_json,version_json,version_key,cover_url,duration_ms,
			norm_title,norm_artists,merged_into,revision,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,NULL,1,?,?)
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title,artists_json=excluded.artists_json,album_json=excluded.album_json,
			version_json=excluded.version_json,version_key=excluded.version_key,cover_url=excluded.cover_url,
			duration_ms=excluded.duration_ms,norm_title=excluded.norm_title,norm_artists=excluded.norm_artists,
			revision=tracks.revision+1,updated_at=excluded.updated_at`,
		row.ID, row.Title, string(artists), string(album), string(version), row.Version.Key(),
		row.CoverURL, durationMillis(row.Duration), music.TitleKey(row.Title), string(normArtists),
		created, updated); err != nil {
		return fmt.Errorf("librarydb: upsert track: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM track_artists WHERE track_id=?`, row.ID); err != nil {
		return err
	}
	for _, name := range music.ArtistSet(row.Artists) {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO track_artists(track_id,artist_norm) VALUES(?,?) ON CONFLICT DO NOTHING`,
			row.ID, name); err != nil {
			return fmt.Errorf("librarydb: index artist: %w", err)
		}
	}
	return nil
}

// putLink upserts one source identity, its binding and the local origin marker.
func (d *DB) putLink(ctx context.Context, tx *sql.Tx, homeID string, link music.SourceLink, origin originRef) error {
	if !link.Valid() {
		return fmt.Errorf("librarydb: invalid link for %s", homeID)
	}
	accountID, err := d.bindingAccountTx(ctx, tx, link.Source)
	if err != nil {
		return err
	}
	artists, err := json.Marshal(nonNilArtists(link.Artists))
	if err != nil {
		return err
	}
	album, err := json.Marshal(link.Album)
	if err != nil {
		return err
	}
	version, err := json.Marshal(link.Version)
	if err != nil {
		return err
	}
	digest := metadataDigest(link)
	imported := link.ImportedAt.Unix()
	if imported <= 0 {
		imported = time.Now().Unix()
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO source_identities(source,source_track_id,home_track_id,import_seq,first_imported_at,
			title,artists_json,album_json,version_json,version_key,cover_url,duration_ms,metadata_digest,revision,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,1,?)
		ON CONFLICT(source,source_track_id) DO UPDATE SET
			home_track_id=excluded.home_track_id,title=excluded.title,artists_json=excluded.artists_json,
			album_json=excluded.album_json,version_json=excluded.version_json,version_key=excluded.version_key,
			cover_url=excluded.cover_url,duration_ms=excluded.duration_ms,metadata_digest=excluded.metadata_digest,
			revision=source_identities.revision+1,updated_at=excluded.updated_at`,
		link.Source.String(), link.TrackID, homeID, link.ImportSeq, imported,
		link.Title, string(artists), string(album), string(version), link.Version.Key(),
		link.CoverURL, durationMillis(link.Duration), digest, time.Now().Unix()); err != nil {
		return fmt.Errorf("librarydb: upsert identity: %w", err)
	}
	var identityID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM source_identities WHERE source=? AND source_track_id=?`,
		link.Source.String(), link.TrackID).Scan(&identityID); err != nil {
		return fmt.Errorf("librarydb: read identity: %w", err)
	}
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO source_bindings(identity_id,account_id,access,quality,evidence_json,checked_generation,checked_at,expires_at,last_seen_at)
		VALUES(?,?,?,?,'{}',NULL,NULL,NULL,?)
		ON CONFLICT(identity_id,account_id) DO UPDATE SET
			access=excluded.access,quality=excluded.quality,last_seen_at=excluded.last_seen_at`,
		identityID, accountID, link.Access.String(), link.Quality, now); err != nil {
		return fmt.Errorf("librarydb: upsert binding: %w", err)
	}
	var bindingID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM source_bindings WHERE identity_id=? AND account_id=?`,
		identityID, accountID).Scan(&bindingID); err != nil {
		return fmt.Errorf("librarydb: read binding: %w", err)
	}
	if origin.Kind == "" {
		origin = originRef{Kind: "local", Key: originKey("local", DefaultProfile)}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO binding_origins(binding_id,origin_kind,origin_key,active,last_seen_run,updated_at)
		VALUES(?, ?, ?, 1, ?, ?)
		ON CONFLICT(binding_id,origin_kind,origin_key) DO UPDATE SET active=1,
			last_seen_run=COALESCE(excluded.last_seen_run,binding_origins.last_seen_run),
			updated_at=excluded.updated_at`,
		bindingID, origin.Kind, origin.Key, nullString(origin.Run), now); err != nil {
		return fmt.Errorf("librarydb: upsert origin: %w", err)
	}
	return nil
}

// bindingAccountTx returns the account a new binding belongs to. When the user
// is not signed in, a local placeholder account keeps the binding addressable.
func (d *DB) bindingAccountTx(ctx context.Context, tx *sql.Tx, source music.SourceID) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM source_accounts WHERE source=? AND is_current=1 LIMIT 1`, source.String()).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("librarydb: read account: %w", err)
	}
	id = AccountID(source.String(), "")
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO source_accounts(id,source,account_key,account_label,state,is_current,session_generation,credential_ref,expires_at,last_verified_at,updated_at)
		 VALUES(?,?,'','本机','logged_in',0,1,'',NULL,NULL,?)
		 ON CONFLICT(id) DO NOTHING`, id, source.String(), now); err != nil {
		return "", fmt.Errorf("librarydb: create local account: %w", err)
	}
	return id, nil
}

// RemoveLink detaches one source binding and its origin markers.
func (d *DB) RemoveLink(ctx context.Context, source music.SourceID, trackID string) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var identityID int64
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM source_identities WHERE source=? AND source_track_id=?`,
		source.String(), trackID).Scan(&identityID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM binding_origins WHERE binding_id IN (SELECT id FROM source_bindings WHERE identity_id=?)`,
		identityID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM source_bindings WHERE identity_id=?`, identityID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM source_identities WHERE id=?`, identityID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteTrack removes a canonical row, its links and its favorite membership.
func (d *DB) DeleteTrack(ctx context.Context, profileID, trackID string) error {
	if profileID == "" {
		profileID = DefaultProfile
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM binding_origins WHERE binding_id IN (
			SELECT b.id FROM source_bindings b
			JOIN source_identities i ON i.id=b.identity_id WHERE i.home_track_id=?)`, trackID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM source_bindings WHERE identity_id IN (
			SELECT id FROM source_identities WHERE home_track_id=?)`, trackID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM source_identities WHERE home_track_id=?`, trackID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM favorites WHERE profile_id=? AND track_id=?`, profileID, trackID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM member_refs WHERE owner_kind='favorites' AND owner_id=? AND anchor_track_id=?`, profileID, trackID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM track_artists WHERE track_id=?`, trackID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tracks WHERE id=?`, trackID); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveFavorite drops only the favorites membership; the canonical row and its
// source links stay in the library.
func (d *DB) RemoveFavorite(ctx context.Context, profileID, trackID string) error {
	if profileID == "" {
		profileID = DefaultProfile
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM favorites WHERE profile_id=? AND track_id=?`, profileID, trackID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM member_refs WHERE owner_kind='favorites' AND owner_id=? AND anchor_track_id=?`, profileID, trackID); err != nil {
		return err
	}
	return tx.Commit()
}

// DisableSource marks every account of one source as signed out. Bindings and
// favorites are preserved.
func (d *DB) DisableSource(ctx context.Context, source music.SourceID) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE source_accounts SET state='logged_out',is_current=0,session_generation=session_generation+1,updated_at=?
		 WHERE source=? AND state<>'logged_out'`, time.Now().Unix(), source.String()); err != nil {
		return err
	}
	return tx.Commit()
}

// NextSeq allocates one import sequence in its own transaction.
func (d *DB) NextSeq(ctx context.Context) (int64, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	seq, err := NextImportSeq(ctx, tx)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return seq, nil
}

func decodeJSON(text string, out any) error {
	if strings.TrimSpace(text) == "" || text == "null" {
		return nil
	}
	if err := json.Unmarshal([]byte(text), out); err != nil {
		return fmt.Errorf("librarydb: decode %q: %w", text, err)
	}
	return nil
}

func nonNilArtists(artists []music.Artist) []music.Artist {
	if artists == nil {
		return []music.Artist{}
	}
	return artists
}

func durationMillis(seconds *float64) any {
	if seconds == nil {
		return nil
	}
	ms := int64(*seconds*1000 + 0.5)
	if ms < 0 {
		return nil
	}
	return ms
}

func seconds(ms sql.NullInt64) *float64 {
	if !ms.Valid {
		return nil
	}
	value := float64(ms.Int64) / 1000
	return &value
}

func metadataDigest(link music.SourceLink) string {
	payload := struct {
		Title    string         `json:"title"`
		Artists  []music.Artist `json:"artists"`
		Album    *music.Album   `json:"album"`
		CoverURL string         `json:"coverUrl"`
		Duration *float64       `json:"durationSeconds"`
		Version  music.Version  `json:"version"`
	}{link.Title, nonNilArtists(link.Artists), link.Album, link.CoverURL, link.Duration, link.Version}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// originKey builds the fixed-shape key used by binding_origins.origin_key.
func originKey(kind, value string) string {
	data, _ := json.Marshal([]string{kind, value})
	return string(data)
}

// SaveRows writes many canonical rows in one transaction. It is used by the
// legacy favorites migration so the whole import commits or rolls back as one.
func (d *DB) SaveRows(ctx context.Context, profileID string, rows []music.CanonicalTrack) error {
	if profileID == "" {
		profileID = DefaultProfile
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var highSeq int64
	for position, row := range rows {
		if err := upsertTrack(ctx, tx, row); err != nil {
			return err
		}
		for _, link := range row.Links {
			if err := d.putLink(ctx, tx, row.ID, link, originRef{}); err != nil {
				return err
			}
			if link.ImportSeq > highSeq {
				highSeq = link.ImportSeq
			}
		}
		if !row.Favorite {
			// The row is in 收录音乐 (or a playlist) but not in 我的收藏;
			// never resurrect it into favorites just because it was saved.
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM favorites WHERE profile_id=? AND track_id=?`, profileID, row.ID); err != nil {
				return fmt.Errorf("librarydb: clear favorite: %w", err)
			}
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM member_refs WHERE owner_kind='favorites' AND owner_id=? AND ref_key=?`,
				profileID, "favorite:"+row.ID); err != nil {
				return fmt.Errorf("librarydb: clear member ref: %w", err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO favorites(profile_id,track_id,position,added_at) VALUES(?,?,?,?)
			 ON CONFLICT(profile_id,track_id) DO UPDATE SET position=excluded.position`,
			profileID, row.ID, position, row.CreatedAt.Unix()); err != nil {
			return fmt.Errorf("librarydb: save favorite: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO member_refs(owner_kind,owner_id,ref_key,anchor_track_id,position,added_at,revision)
			 VALUES('favorites',?,?,?,?,?,1)
			 ON CONFLICT(owner_kind,owner_id,ref_key) DO UPDATE SET position=excluded.position`,
			profileID, "favorite:"+row.ID, row.ID, position, row.CreatedAt.Unix()); err != nil {
			return fmt.Errorf("librarydb: save member ref: %w", err)
		}
	}
	// Imported links keep their original import_seq. Raise the clock's
	// high-water mark so the next allocation cannot collide with them.
	if highSeq > 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE library_clock SET import_seq=MAX(import_seq,?) WHERE singleton=1`, highSeq); err != nil {
			return fmt.Errorf("librarydb: raise import seq: %w", err)
		}
	}
	return tx.Commit()
}

// CountTracks reports how many canonical rows exist.
func (d *DB) CountTracks(ctx context.Context) (int, error) {
	var count int
	if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracks`).Scan(&count); err != nil {
		return 0, fmt.Errorf("librarydb: count tracks: %w", err)
	}
	return count, nil
}

// Migration journal helpers.

// MigrationState reports the recorded state for one migration input, or "" when
// the input was never seen.
func (d *DB) MigrationState(ctx context.Context, key, digest string) (string, error) {
	var state string
	err := d.sql.QueryRowContext(ctx,
		`SELECT state FROM data_migrations WHERE migration_key=? AND input_digest=?`, key, digest).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("librarydb: read migration: %w", err)
	}
	return state, nil
}

// BeginMigration records a prepared migration plan and opens an attempt.
func (d *DB) BeginMigration(ctx context.Context, id, key, digest, inputsJSON string) error {
	now := d.now().Unix()
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO data_migrations(id,migration_key,input_digest,inputs_json,state,completed_phase,
			destination_relpath,temp_relpath,output_digest,error_code,updated_at)
		VALUES(?,?,?,?,'prepared','none',NULL,NULL,NULL,'',?)
		ON CONFLICT(migration_key,input_digest) DO UPDATE SET state='prepared',updated_at=excluded.updated_at`,
		id, key, digest, inputsJSON, now); err != nil {
		return fmt.Errorf("librarydb: begin migration: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE migration_attempts SET state='interrupted',finished_at=? WHERE state='running'`, now); err != nil {
		return fmt.Errorf("librarydb: interrupt stale attempt: %w", err)
	}
	var attempt int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(attempt_no),0)+1 FROM migration_attempts WHERE migration_id=?`, id).Scan(&attempt); err != nil {
		return fmt.Errorf("librarydb: next attempt: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO migration_attempts(migration_id,attempt_no,state,start_phase,end_phase,error_code,started_at,finished_at)
		VALUES(?,?,'running','none',NULL,'',?,NULL)`, id, attempt, now); err != nil {
		return fmt.Errorf("librarydb: start attempt: %w", err)
	}
	return tx.Commit()
}

// FinishMigration records the outcome of the current attempt.
func (d *DB) FinishMigration(ctx context.Context, id, state, phase string) error {
	now := d.now().Unix()
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE data_migrations SET state=?,completed_phase=?,updated_at=? WHERE id=?`,
		state, phase, now, id); err != nil {
		return fmt.Errorf("librarydb: finish migration: %w", err)
	}
	attemptState := "succeeded"
	if state == "failed" {
		attemptState = "failed"
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE migration_attempts SET state=?,end_phase=?,finished_at=? WHERE migration_id=? AND state='running'`,
		attemptState, phase, now, id); err != nil {
		return fmt.Errorf("librarydb: finish attempt: %w", err)
	}
	return tx.Commit()
}
