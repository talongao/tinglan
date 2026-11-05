package main

import (
	"context"
	"testing"
	"time"

	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
	"tinglan/native/internal/source/sourcetest"
)

// Mirroring a playlist collects its members into 收录音乐; it must never put
// them into 我的收藏. Otherwise 我的收藏 grows with songs the account never
// favorited, which is exactly what users reported (944 favorites showing a
// playlist-only song).
func TestPlaylistUpdateDoesNotFavoriteMembers(t *testing.T) {
	m := newModel()
	openProfileForTest(t, m, t.TempDir())
	m.openLocalLibrary()
	if m.local == nil {
		t.Fatal("library not open")
	}
	t.Cleanup(func() {
		if m.local != nil {
			_ = m.local.Close()
		}
	})
	ctx := context.Background()
	pl, err := m.local.EnsureImportedPlaylist(ctx, music.SourceHuawei, "remote-9", "测试歌单", "", "")
	if err != nil {
		t.Fatalf("import playlist: %v", err)
	}
	favoritesBefore := m.local.Len()

	fake := &sourcetest.Fake{
		SourceID: music.SourceHuawei,
		Pages: []source.ImportPage{{Tracks: []music.Track{{
			Source:  music.SourceHuawei,
			ID:      "tr-9",
			Title:   "广东爱情故事",
			Artists: []music.Artist{{Name: "广东雨神"}},
		}}}},
	}
	if err := m.registry.RegisterSource(fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := m.local.EnsureAccount(ctx, music.SourceHuawei, "acct", "华为音乐账号"); err != nil {
		t.Fatalf("account: %v", err)
	}

	m.runPlaylistUpdate(pl)

	deadline := time.Now().Add(3 * time.Second)
	for {
		m.mu.Lock()
		pending := len(m.sourceResults)
		m.mu.Unlock()
		if pending > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("playlist update produced no result")
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.mu.Lock()
	results := m.sourceResults
	m.sourceResults = nil
	m.mu.Unlock()
	if len(results) != 1 || results[0].err != nil {
		t.Fatalf("results = %+v", results)
	}
	after, err := m.local.Playlist(ctx, pl.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.TrackCount != 1 {
		t.Fatalf("members = %d", after.TrackCount)
	}
	if got := m.local.Len(); got != favoritesBefore {
		t.Fatalf("favorites grew from %d to %d after mirroring a playlist", favoritesBefore, got)
	}
	row, ok := m.local.RowForLink(music.SourceHuawei, "tr-9")
	if !ok {
		t.Fatal("member missing from 收录音乐")
	}
	if row.Favorite {
		t.Fatal("playlist member was added to 我的收藏")
	}
}

// Adding a song to a local playlist must also collect it only.
func TestAddToPlaylistDoesNotFavorite(t *testing.T) {
	m := newModel()
	openProfileForTest(t, m, t.TempDir())
	m.openLocalLibrary()
	if m.local == nil {
		t.Fatal("library not open")
	}
	t.Cleanup(func() {
		if m.local != nil {
			_ = m.local.Close()
		}
	})
	ctx := context.Background()
	pl, err := m.local.CreatePlaylist(ctx, "本地歌单", "")
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	m.addTrack = music.Track{Source: music.SourceHuawei, ID: "tr-7", Title: "十九岁", Artists: []music.Artist{{Name: "赵雷"}}}
	m.addToPlaylist(pl.ID)
	row, ok := m.local.RowForLink(music.SourceHuawei, "tr-7")
	if !ok {
		t.Fatal("track missing from 收录音乐")
	}
	if row.Favorite {
		t.Fatal("adding to a playlist favorited the song")
	}
	detail, err := m.local.Playlist(ctx, pl.ID)
	if err != nil {
		t.Fatalf("playlist: %v", err)
	}
	if detail.TrackCount != 1 {
		t.Fatalf("playlist members = %d", detail.TrackCount)
	}
}
