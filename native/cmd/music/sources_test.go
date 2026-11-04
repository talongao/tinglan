package main

import (
	"strings"
	"testing"

	"tinglan/native/internal/music"
)

func TestFavoriteButtonAddsAndRemovesLocalRow(t *testing.T) {
	m := newModel()
	openProfileForTest(t, m, t.TempDir())
	track := music.Track{
		ID: "hw-1", Title: "测试歌曲",
		Artists: []music.Artist{{ID: "a1", Name: "测试歌手"}},
	}
	m.toggleFavorite(track)
	if m.local == nil || m.local.Len() != 1 || len(m.tracks) != 1 {
		t.Fatalf("local add failed: len=%d tracks=%d", m.local.Len(), len(m.tracks))
	}
	if !m.favoriteKnown[track.ID] {
		t.Fatal("source id not marked favorite")
	}
	m.toggleFavorite(track)
	if m.local.Len() != 0 || len(m.tracks) != 0 {
		t.Fatalf("local remove failed: len=%d tracks=%d", m.local.Len(), len(m.tracks))
	}
	if m.favoriteKnown[track.ID] {
		t.Fatal("heart stayed filled after local removal")
	}
}

func TestLogoutKeepsLocalFavorites(t *testing.T) {
	m := newModel()
	openProfileForTest(t, m, t.TempDir())
	m.toggleFavorite(music.Track{ID: "hw-keep", Title: "退出后仍在"})
	if m.local.Len() != 1 {
		t.Fatal("setup failed")
	}
	m.logout()
	if m.local.Len() != 1 || len(m.tracks) != 1 {
		t.Fatalf("logout dropped local favorites: len=%d tracks=%d", m.local.Len(), len(m.tracks))
	}
	if !strings.Contains(m.status, "本地收藏仍可用") {
		t.Fatalf("status = %q", m.status)
	}
	row, ok := m.local.RowForLink(music.SourceHuawei, "hw-keep")
	if !ok || len(row.Links) != 1 || !row.Links[0].Disabled {
		t.Fatalf("binding was not deactivated: %+v", row)
	}
}

func TestRestrictToSourceKeepsOnlyImportedPlaylistPlatform(t *testing.T) {
	row := music.CanonicalTrack{
		ID: "track-1", Title: "测试歌曲",
		Links: []music.SourceLink{
			{Source: music.SourceHuawei, TrackID: "hw-1", ImportSeq: 1},
			{Source: music.SourceNetEase, TrackID: "ne-1", ImportSeq: 2},
		},
	}
	kept := restrictToSource(row, "huawei")
	if len(kept.Links) != 1 || kept.Links[0].TrackID != "hw-1" {
		t.Fatalf("imported playlist kept foreign bindings: %+v", kept.Links)
	}
	if len(row.Links) != 2 {
		t.Fatal("restrictToSource mutated the original row")
	}
	if open := restrictToSource(row, ""); len(open.Links) != 2 {
		t.Fatalf("unrestricted row lost bindings: %+v", open.Links)
	}
	if none := restrictToSource(row, "qq"); len(none.Links) != 0 {
		t.Fatalf("unavailable platform should leave no binding: %+v", none.Links)
	}
}
