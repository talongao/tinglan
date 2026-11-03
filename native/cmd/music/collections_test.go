package main

import (
	"testing"
	"time"
	"tinglan/native/internal/huawei"
	"tinglan/native/internal/storage"
)

func TestCollectionCacheIncludesEmptyResults(t *testing.T) {
	for _, empty := range []bool{false, true} {
		m := newModel()
		m.session = &huawei.Session{}
		m.accountStore = storage.New(t.TempDir())
		value := storage.Collection{}
		if !empty {
			value.Playlists = []huawei.Playlist{{ID: "synthetic", Title: "cached"}}
		}
		if err := m.accountStore.SaveCollection("playlists", "", value); err != nil {
			t.Fatal(err)
		}
		m.loadCollections("playlists")
		deadline := time.Now().Add(time.Second)
		for {
			m.mu.Lock()
			ready := len(m.results) > 0
			m.mu.Unlock()
			if ready {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("cache did not publish promptly")
			}
			time.Sleep(time.Millisecond)
		}
		m.applyLibraryResults()
		if m.collectionLoading || len(m.playlists) != len(value.Playlists) {
			t.Fatal("cache not applied")
		}
	}
}

func TestCollectionFreshnessAndLateResults(t *testing.T) {
	now := time.Now()
	for _, age := range []time.Duration{-time.Minute, 5 * time.Minute, 24 * time.Hour} {
		if collectionFresh(now.Add(-age), now) {
			t.Fatal("stale or future cache accepted")
		}
	}
	if !collectionFresh(now.Add(-time.Minute), now) {
		t.Fatal("fresh cache rejected")
	}
	m := newModel()
	m.collectionGeneration = 3
	m.results = []libraryResult{{kind: "playlists", epoch: 2, playlists: []huawei.Playlist{{ID: "old"}}}, {kind: "playlists", epoch: 3, cached: true, refreshing: true, playlists: []huawei.Playlist{{ID: "cache"}}}}
	m.applyLibraryResults()
	if !m.collectionLoading || len(m.playlists) != 1 || m.playlists[0].ID != "cache" {
		t.Fatal("cache refresh or generation handling failed")
	}
	m.results = []libraryResult{{kind: "playlists", epoch: 3, playlists: []huawei.Playlist{{ID: "fresh"}}}}
	m.applyLibraryResults()
	if m.collectionLoading || m.playlists[0].ID != "fresh" {
		t.Fatal("network refresh not applied")
	}
}
