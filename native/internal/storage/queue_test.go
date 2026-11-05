package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tinglan/native/internal/music"
)

func readQueueDoc(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, queueFileName))
	if err != nil {
		t.Fatalf("queue document missing: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("queue document is not JSON: %v", err)
	}
	return doc
}

func TestQueueWritesVersionTwoDocument(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	collected := music.Track{ID: strings.Repeat("a", 64), Title: "Collected", Collected: true}
	transient := music.Track{ID: "song-42", Title: "Transient", OnlySource: "huawei"}
	if err := store.SaveQueue(SavedQueue{Tracks: []music.Track{collected, transient}, Index: 1, Position: 12.5}); err != nil {
		t.Fatal(err)
	}
	doc := readQueueDoc(t, dir)
	if doc["schemaVersion"] != float64(queueSchemaVersion) {
		t.Fatalf("schemaVersion = %v", doc["schemaVersion"])
	}
	items, _ := doc["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	first, _ := items[0].(map[string]any)
	if first["trackId"] != collected.ID {
		t.Fatalf("collected track id = %v", first["trackId"])
	}
	second, _ := items[1].(map[string]any)
	if second["onlySource"] != "huawei" {
		t.Fatalf("onlySource = %v", second["onlySource"])
	}
	if _, ok := second["trackId"]; ok {
		t.Fatal("transient entry must not claim a local track id")
	}
	if raw, _ := json.Marshal(doc); strings.Contains(string(raw), "://") {
		t.Fatal("queue document must not persist a playback URL")
	}
}

func TestQueueRoundTripsSourceRestrictionAndTransientRows(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	tracks := []music.Track{
		{ID: strings.Repeat("b", 64), Title: "Collected", Collected: true},
		{ID: "song-7", Title: "Transient", OnlySource: "huawei"},
	}
	if err := store.SaveQueue(SavedQueue{Tracks: tracks, Index: 0, Position: 3}); err != nil {
		t.Fatal(err)
	}
	saved, err := New(dir).LoadQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Tracks) != 2 || saved.Position != 3 {
		t.Fatalf("queue = %+v", saved)
	}
	if !saved.Tracks[0].Collected || saved.Tracks[0].ID != tracks[0].ID {
		t.Fatalf("collected row lost its id: %+v", saved.Tracks[0])
	}
	if saved.Tracks[1].Collected || saved.Tracks[1].ID != "song-7" {
		t.Fatalf("transient row = %+v", saved.Tracks[1])
	}
	if saved.Tracks[1].OnlySource != "huawei" {
		t.Fatalf("source restriction lost: %+v", saved.Tracks[1])
	}
}

func TestLegacyQueueMigratesToVersionTwo(t *testing.T) {
	dir := t.TempDir()
	legacy := []byte(`{"tracks":[{"id":"old-1","title":"Legacy","artists":[{"name":"A"}]}],"index":0,"positionSeconds":9}`)
	if err := os.WriteFile(filepath.Join(dir, legacyQueueFileName), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	store := New(dir)
	saved, err := store.LoadQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Tracks) != 1 || saved.Tracks[0].Title != "Legacy" {
		t.Fatalf("legacy queue = %+v", saved)
	}
	if err := store.SaveQueue(saved); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, legacyQueueFileName+".migrated")); err != nil {
		t.Fatal("legacy queue should be archived after the v2 publish")
	}
	if _, err := os.Stat(filepath.Join(dir, legacyQueueFileName)); !os.IsNotExist(err) {
		t.Fatal("legacy queue must not stay readable next to the v2 document")
	}
}

func TestNewFormatWinsOverStaleLegacyQueue(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	if err := store.SaveQueue(SavedQueue{Tracks: []music.Track{{ID: strings.Repeat("c", 64), Title: "Fresh", Collected: true}}}); err != nil {
		t.Fatal(err)
	}
	stale := []byte(`{"tracks":[{"id":"old-1","title":"Stale"}],"index":0}`)
	if err := os.WriteFile(filepath.Join(dir, legacyQueueFileName), stale, 0o600); err != nil {
		t.Fatal(err)
	}
	saved, err := New(dir).LoadQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Tracks) != 1 || saved.Tracks[0].Title != "Fresh" {
		t.Fatalf("stale legacy queue won: %+v", saved)
	}
}

func TestClearQueueRemovesBothFormats(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	if err := store.SaveQueue(SavedQueue{Tracks: []music.Track{{ID: strings.Repeat("d", 64), Title: "Row", Collected: true}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, legacyQueueFileName), []byte(`{"tracks":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearQueue(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{queueFileName, legacyQueueFileName} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s survived ClearQueue", name)
		}
	}
}
