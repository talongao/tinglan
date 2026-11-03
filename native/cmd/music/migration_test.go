package main

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"strings"
	"testing"
	"tinglan/native/internal/huawei"
)

func TestAccountResetDiscardsPendingUIResults(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	m.accountEpoch = 9
	m.writeBusy = true
	m.writeResults = []writeResult{{epoch: 9, kind: "favorite", id: "old", favorite: true}}
	m.coverResults = []coverResult{{epoch: 9, url: "https://fixture.dbankcdn.cn/cover"}}
	m.entity = &huawei.Detail{ID: "old"}
	m.entityResults = []entityResult{{generation: m.entityGeneration, detail: huawei.Detail{ID: "old"}}}
	m.clearLibrary()
	m.wake()
	if m.entity != nil || len(m.covers) != 0 || m.favoriteOff["old"] || m.writeBusy {
		t.Fatal("old account results survived reset")
	}
}

func TestSearchResultsDoNotConsumeLibraryLoading(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	m.loading = true
	m.searchLoading = true
	m.searchGeneration = 3
	m.searchQuery = "match"
	m.results = []libraryResult{{search: true, epoch: 3, query: "match", tracks: syntheticTracks(2), complete: true}}
	m.applyLibraryResults()
	if !m.loading || m.searchLoading || len(m.filtered) != 2 {
		t.Fatal("search changed favorites request")
	}
	m.searchLoading = true
	m.query.SetText("")
	m.search()
	m.results = []libraryResult{{search: true, epoch: 3, query: "match", tracks: syntheticTracks(2)}}
	m.applyLibraryResults()
	if m.searchLoading || m.searchQuery != "" {
		t.Fatal("cleared search accepted old completion")
	}
}
func TestCustomAccentQueueRemovalAndEntityNavigation(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	m.openProfile(t.TempDir())
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	click(t, d, albumRows.At(0))
	if m.entity == nil || len(m.entity.Tracks) != 2 {
		t.Fatal("album did not open")
	}
	click(t, d, nav.At(4))
	if m.entity != nil {
		t.Fatal("sidebar did not dismiss detail")
	}
	click(t, d, tabs.At(1))
	click(t, d, themes.At(1))
	settings, err := m.store.LoadSettings()
	if err != nil || settings.Appearance.Theme != "ocean" {
		t.Fatal("custom accent not persisted")
	}
	click(t, d, nav.At(0))
	click(t, d, allID)
	click(t, d, queueID)
	click(t, d, queueRemove.At(0))
	if len(m.queue.Tracks) != 1 {
		t.Fatal("queue removal failed")
	}
	click(t, d, clearQueueID)
	if m.dialog != "clear-queue" {
		t.Fatal("queue clear did not ask for confirmation")
	}
	click(t, d, confirmWriteID)
	if len(m.queue.Tracks) != 0 {
		t.Fatal("queue clear failed")
	}
}

func TestSearchPlayKeepsExistingQueue(t *testing.T) {
	m := newModel()
	m.enableDemo(3)
	m.startTracks(m.tracks[:2], 0)
	m.view = 2
	m.filtered = m.tracks[2:]
	m.playListTrack(m.filtered, 0)
	if len(m.queue.Tracks) != 3 || m.queue.Current().ID != m.filtered[0].ID {
		t.Fatal("search replaced queue")
	}
	m.playListTrack(m.filtered, 0)
	if len(m.queue.Tracks) != 3 {
		t.Fatal("search duplicated queued track")
	}
}

func TestSearchPaginationDeduplicatesAndStopsRepeatedPages(t *testing.T) {
	m := newModel()
	m.enableDemo(3)
	m.filtered = m.tracks[:2]
	m.searchQuery = "fixture"
	m.searchGeneration = 4
	m.results = []libraryResult{{search: true, epoch: 4, query: "fixture", tracks: m.tracks[1:], searchNext: 4}}
	m.applyLibraryResults()
	if len(m.filtered) != 3 || !m.searchMore || m.searchNext != 4 {
		t.Fatal("next page lost")
	}
	m.results = []libraryResult{{search: true, epoch: 4, query: "fixture", tracks: m.tracks[1:], searchNext: 6}}
	m.applyLibraryResults()
	if m.searchMore {
		t.Fatal("repeated page would keep loading")
	}
}

func TestSearchListHasNoBulkControlsAndScrollsToLastRow(t *testing.T) {
	m := newModel()
	m.enableDemo(40)
	m.view = 2
	m.filtered = m.tracks
	m.searchQuery = "fixture"
	m.searchLoading = true // Keep this geometry test offline.
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, h := range d.Snapshot().Hits {
		if h.ID == allID {
			t.Fatal("search exposes play all")
		}
	}
	for _, label := range d.Snapshot().Labels {
		if strings.Contains(label.Value, "已加载") {
			t.Fatal("search exposes loaded count")
		}
	}
	m.scrollEvent(ui.ScrollEvent{X: 700, Y: 400, DeltaY: 10000, Viewport: ui.Size{Width: 1180, Height: 760}})
	m.invalidator.Invalidate()
	if err := d.Pump(); err != nil {
		t.Fatal(err)
	}
	for _, h := range d.Snapshot().Hits {
		if h.ID == rows.At(39) {
			return
		}
	}
	t.Fatal("last search result is not reachable")
}
