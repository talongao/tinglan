package main

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"tinglan/native/internal/librarydb"
)

// The album page scrolls as a whole page; the last track must stay reachable
// and the cover must move with it (regression: scrolling stopped at 5 rows).
func TestAlbumDetailScrollsToLastTrack(t *testing.T) {
	m := newModel()
	m.enableDemo(40)
	m.openEntity("album", "album-1", "测试专辑")
	m.entity.Tracks = syntheticTracks(20)
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	m.scrollEvent(ui.ScrollEvent{X: 700, Y: 500, DeltaY: 10000, Viewport: ui.Size{Width: 1180, Height: 760}})
	m.invalidator.Invalidate()
	if err := d.Pump(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range d.Snapshot().Hits {
		if h.ID == rows.At(19) {
			found = true
		}
	}
	if !found {
		t.Fatalf("last track unreachable, entityScroll=%v", m.entityScroll)
	}
}

func TestLocalPlaylistDetailScrollsToLastTrack(t *testing.T) {
	m := newModel()
	m.enableDemo(40)
	m.view = 1
	m.localDetail = &librarydb.Playlist{ID: "pl_1", Kind: "local", Name: "测试歌单"}
	m.localTracks = syntheticTracks(20)
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	m.scrollEvent(ui.ScrollEvent{X: 700, Y: 500, DeltaY: 10000, Viewport: ui.Size{Width: 1180, Height: 760}})
	m.invalidator.Invalidate()
	if err := d.Pump(); err != nil {
		t.Fatal(err)
	}
	for _, h := range d.Snapshot().Hits {
		if h.ID == rows.At(19) {
			return
		}
	}
	t.Fatalf("last playlist track unreachable, scroll=%v", m.scroll[1])
}
