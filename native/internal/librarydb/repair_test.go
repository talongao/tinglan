package librarydb

import (
	"context"
	"testing"
	"time"

	"tinglan/native/internal/music"
)

// Playlist mirroring used to favorite every member. The repair must drop only
// rows whose sole active origin is a playlist, and keep the canonical rows.
func TestRepairPlaylistFavoritesDropsPlaylistOnlyRows(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	profile := DefaultProfile
	now := time.Now()

	link := func(id string) music.SourceLink {
		return music.SourceLink{
			Source: music.SourceHuawei, TrackID: id, Title: "歌 " + id,
			Artists: []music.Artist{{Name: "演唱者"}}, ImportedAt: now,
		}
	}
	row := func(id string) music.CanonicalTrack {
		return music.CanonicalTrack{
			ID: id, Title: "歌", CreatedAt: now,
			Links: []music.SourceLink{link("src-" + id)},
		}
	}
	if err := db.SaveRow(ctx, profile, row("tr-fav"), true, 0); err != nil {
		t.Fatalf("save favorite: %v", err)
	}
	if err := db.SaveRow(ctx, profile, row("tr-list"), true, 1); err != nil {
		t.Fatalf("save playlist row: %v", err)
	}
	// Mark one row as imported by a real favorites import, the other one only
	// as a playlist member.
	if _, err := db.sql.ExecContext(ctx, `
		INSERT INTO binding_origins(binding_id,origin_kind,origin_key,active,last_seen_run,updated_at)
		SELECT b.id,'favorites','imp-1',1,'run-1',? FROM source_bindings b
		JOIN source_identities si ON si.id=b.identity_id WHERE si.home_track_id='tr-fav'`, now.Unix()); err != nil {
		t.Fatalf("origin favorites: %v", err)
	}
	pl, err := db.EnsureImportedPlaylist(ctx, profile, music.SourceHuawei.String(), "remote-1", "平台歌单", "", "")
	if err != nil {
		t.Fatalf("imported playlist: %v", err)
	}
	if err := db.ReplacePlaylistMembers(ctx, pl.ID, []string{"tr-list"}, "平台歌单", "", ""); err != nil {
		t.Fatalf("playlist member: %v", err)
	}
	if _, err := db.sql.ExecContext(ctx, `
		INSERT INTO imports(id,profile_id,account_id,source,kind,remote_collection_id,local_playlist_id,
			status,target_generation,last_attempt_at,last_success_at,added_count,updated_count,error_code,updated_at)
		VALUES('imp-1',?,'acct','huawei','favorites','["huawei",""]',NULL,'imported',1,?,?,2,0,'',?)`,
		profile, now.Unix(), now.Unix(), now.Unix()); err != nil {
		t.Fatalf("import row: %v", err)
	}

	dropped, ran, err := db.RepairPlaylistFavorites(ctx, profile)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if !ran {
		t.Fatal("repair did not run")
	}
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	var favorites int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM favorites`).Scan(&favorites); err != nil {
		t.Fatal(err)
	}
	if favorites != 1 {
		t.Fatalf("favorites = %d, want 1", favorites)
	}
	var tracks int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracks`).Scan(&tracks); err != nil {
		t.Fatal(err)
	}
	if tracks != 2 {
		t.Fatalf("tracks = %d, want 2 (canonical rows must survive)", tracks)
	}
	rows, _, err := db.Load(ctx, profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		switch r.Track.ID {
		case "tr-fav":
			if !r.Track.Favorite {
				t.Fatal("real favorite lost its membership")
			}
		case "tr-list":
			if r.Track.Favorite {
				t.Fatal("playlist-only row kept a favorites membership")
			}
		}
	}
	// Running twice is a no-op.
	if dropped, ran, err := db.RepairPlaylistFavorites(ctx, profile); err != nil || ran || dropped != 0 {
		t.Fatalf("second run: dropped=%d ran=%v err=%v", dropped, ran, err)
	}
}

// A profile that migrated legacy favorites JSON has rows with only a generic
// 'local' origin; the repair cannot tell those apart from playlist pollution,
// so it must stay out of the way.
func TestRepairSkipsProfilesWithLegacyMigration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	profile := DefaultProfile
	now := time.Now()
	if _, err := db.sql.ExecContext(ctx, `
		INSERT INTO data_migrations(id,migration_key,input_digest,inputs_json,state,completed_phase,
			destination_relpath,temp_relpath,output_digest,error_code,updated_at)
		VALUES('m1','canonical-favorites-json','d1','[]','done','published',NULL,NULL,NULL,'',?)`, now.Unix()); err != nil {
		t.Fatalf("legacy migration row: %v", err)
	}
	if _, err := db.sql.ExecContext(ctx, `
		INSERT INTO imports(id,profile_id,account_id,source,kind,remote_collection_id,local_playlist_id,
			status,target_generation,last_attempt_at,last_success_at,added_count,updated_count,error_code,updated_at)
		VALUES('imp-1',?,'acct','huawei','favorites','["huawei",""]',NULL,'imported',1,?,?,2,0,'',?)`,
		profile, now.Unix(), now.Unix(), now.Unix()); err != nil {
		t.Fatalf("import row: %v", err)
	}
	dropped, ran, err := db.RepairPlaylistFavorites(ctx, profile)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if ran || dropped != 0 {
		t.Fatalf("repair ran=%v dropped=%d on a legacy profile", ran, dropped)
	}
}

// Before any favorites import succeeded there is no favorites origin at all, so
// every favorite would look playlist-only.
func TestRepairWaitsForSuccessfulFavoritesImport(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	dropped, ran, err := db.RepairPlaylistFavorites(ctx, DefaultProfile)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if ran || dropped != 0 {
		t.Fatalf("repair ran=%v dropped=%d without a favorites import", ran, dropped)
	}
}
