package main

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

// A dialog text field that is not registered with the keyboard router still
// opens and validates, but typing is silently dropped. Drive the real canvas:
// open 我的歌单 -> 新建歌单 -> focus the field -> type.
func TestNewPlaylistDialogAcceptsTyping(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	click(t, d, nav.At(1))
	click(t, d, newPlaylistID)
	if m.dialog != "new-playlist" {
		t.Fatalf("dialog = %q, want new-playlist", m.dialog)
	}
	click(t, d, newPlaylistFieldID)
	if err := d.Type("two words"); err != nil {
		t.Fatal(err)
	}
	if got := m.newPlaylist.Text(); got != "two words" {
		t.Fatalf("new playlist field = %q, want %q", got, "two words")
	}
}
