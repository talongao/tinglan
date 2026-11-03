package librarydb

import (
	"context"
	"testing"
	"time"

	"tinglan/native/internal/music"
)

func TestLocalPlaylistLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	playlist, err := db.CreateLocalPlaylist(ctx, DefaultProfile, "通勤", "早上听")
	if err != nil {
		t.Fatal(err)
	}
	if !playlist.Local() {
		t.Fatalf("kind = %q", playlist.Kind)
	}
	row := music.NewCanonical(music.Track{Source: music.SourceHuawei, ID: "h1", Title: "七里香",
		Artists: []music.Artist{{ID: "a1", Name: "周杰伦"}}}, 1, time.Now())
	if err := db.SaveRows(ctx, DefaultProfile, []music.CanonicalTrack{row}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddPlaylistTrack(ctx, playlist.ID, row.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.AddPlaylistTrack(ctx, playlist.ID, row.ID); err != nil {
		t.Fatal(err)
	}
	ids, err := db.PlaylistTracks(ctx, playlist.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != row.ID {
		t.Fatalf("members = %v", ids)
	}
	updated, err := db.Playlist(ctx, playlist.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.TrackCount != 1 {
		t.Fatalf("count = %d", updated.TrackCount)
	}
	if err := db.DeletePlaylist(ctx, playlist.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Playlist(ctx, playlist.ID); err == nil {
		t.Fatal("playlist survived delete")
	}
}

func TestImportedPlaylistIsSingleCopyAndReadOnly(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	first, err := db.EnsureImportedPlaylist(ctx, DefaultProfile, "huawei", "remote-1", "旧名", "", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.EnsureImportedPlaylist(ctx, DefaultProfile, "huawei", "remote-1", "新名", "简介", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("second import created a copy: %s vs %s", first.ID, second.ID)
	}
	if second.Name != "新名" {
		t.Fatalf("metadata not refreshed: %q", second.Name)
	}
	if err := db.AddPlaylistTrack(ctx, first.ID, "track-1"); err == nil {
		t.Fatal("imported playlist accepted a local edit")
	}
	for _, id := range []string{"a", "b"} {
		if err := db.SaveRow(ctx, DefaultProfile, music.CanonicalTrack{ID: id, Title: id, Version: music.Version{}}, false, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.ReplacePlaylistMembers(ctx, first.ID, []string{"a", "b"}, "新名", "简介", ""); err != nil {
		t.Fatal(err)
	}
	ids, err := db.PlaylistTracks(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("mirrored members = %v", ids)
	}
}

func TestReplaceImportedMembersDeduplicates(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	add := func(id string) {
		if err := db.SaveRow(ctx, DefaultProfile, music.CanonicalTrack{ID: id, Title: id, Version: music.Version{}}, false, 0); err != nil {
			t.Fatal(err)
		}
	}
	add("dup")
	pl, err := db.EnsureImportedPlaylist(ctx, DefaultProfile, "huawei", "remote1", "p", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReplacePlaylistMembers(ctx, pl.ID, []string{"dup", "dup"}, "p", "", ""); err != nil {
		t.Fatalf("duplicate members must not fail: %v", err)
	}
	ids, err := db.PlaylistTracks(ctx, pl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 {
		t.Fatalf("want one deduped member, got %d", len(ids))
	}
}

// A remote listing refreshes the local copy's cover without creating a copy
// for playlists the user never imported.
func TestUpdateImportedPlaylistMetaKeepsCover(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	p, err := db.EnsureImportedPlaylist(ctx, DefaultProfile, "huawei", "remote-1", "歌单", "说明", "https://example.test/a.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateImportedPlaylistMeta(ctx, DefaultProfile, "huawei", "remote-1", "歌单", "说明", ""); err != nil {
		t.Fatal(err)
	}
	got, err := db.Playlist(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CoverURL != "https://example.test/a.jpg" {
		t.Fatalf("cover = %q, want the stored one kept", got.CoverURL)
	}
	if err := db.UpdateImportedPlaylistMeta(ctx, DefaultProfile, "huawei", "remote-1", "新名字", "", "https://example.test/b.jpg"); err != nil {
		t.Fatal(err)
	}
	got, _ = db.Playlist(ctx, p.ID)
	if got.Name != "新名字" || got.CoverURL != "https://example.test/b.jpg" || got.Description != "说明" {
		t.Fatalf("meta = %q/%q/%q", got.Name, got.Description, got.CoverURL)
	}
	// A remote-only playlist must not be created by a metadata refresh.
	if err := db.UpdateImportedPlaylistMeta(ctx, DefaultProfile, "huawei", "remote-2", "x", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ImportedPlaylist(ctx, DefaultProfile, "huawei", "remote-2"); err == nil {
		t.Fatal("metadata refresh created a local copy")
	}
}
