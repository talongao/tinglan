package main

import (
	"context"
	"testing"
	"time"

	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
	"tinglan/native/internal/source/sourcetest"
)

// A remote collection that comes back empty while the local copy has members
// must not be published: the plugin may be running with a session whose
// account no longer returns data, and mirroring that would erase the copy.
func TestPlaylistUpdateKeepsCopyWhenRemoteEmpty(t *testing.T) {
	m := newModel()
	openProfileForTest(t, m, t.TempDir())
	m.openLocalLibrary()
	if m.local == nil {
		t.Fatal("library not open")
	}
	ctx := context.Background()
	added, err := m.local.AddToLibrary(ctx, music.Track{
		Source:  music.SourceHuawei,
		ID:      "tr-1",
		Title:   "晴天",
		Artists: []music.Artist{{Name: "周杰伦"}},
	}, nil, music.MatchPolicy{})
	if err != nil {
		t.Fatalf("add track: %v", err)
	}
	pl, err := m.local.EnsureImportedPlaylist(ctx, music.SourceHuawei, "remote-1", "测试歌单", "", "")
	if err != nil {
		t.Fatalf("import playlist: %v", err)
	}
	if err := m.local.ReplaceImportedMembers(ctx, pl.ID, []string{added}, "测试歌单", "", ""); err != nil {
		t.Fatalf("members: %v", err)
	}
	before, err := m.local.Playlist(ctx, pl.ID)
	if err != nil || before.TrackCount != 1 {
		t.Fatalf("members before = %d (%v)", before.TrackCount, err)
	}

	fake := &sourcetest.Fake{
		SourceID: music.SourceHuawei,
		Pages:    []source.ImportPage{{Tracks: nil}},
	}
	if err := m.registry.RegisterSource(fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := m.local.EnsureAccount(ctx, music.SourceHuawei, "acct", "华为音乐账号"); err != nil {
		t.Fatalf("account: %v", err)
	}

	m.runPlaylistUpdate(before)

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
	if len(results) != 1 {
		t.Fatalf("results = %d", len(results))
	}
	if results[0].err == nil {
		t.Fatal("empty remote accepted as success")
	}
	after, err := m.local.Playlist(ctx, pl.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.TrackCount != 1 {
		t.Fatalf("local copy lost members: %d", after.TrackCount)
	}
	if after.Kind != "imported" {
		t.Fatalf("kind = %q", after.Kind)
	}
}
