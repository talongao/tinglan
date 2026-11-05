package main

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"testing"
)

func TestInputFocusSubmissionAndModalTraversal(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	click(t, d, nav.At(2))
	if d.Snapshot().Focused != searchID {
		t.Fatal("search not focused on entry")
	}
	if err := d.Type("fixture"); err != nil {
		t.Fatal(err)
	}
	if err := d.Key(ui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if m.searchQuery != "fixture" {
		t.Fatal("Enter did not search")
	}
	click(t, d, nav.At(4))
	click(t, d, tabs.At(1))
	click(t, d, fontChoiceID)
	if d.Snapshot().Focused != fontFilterID {
		t.Fatal("font search not focused")
	}
	m.fontFamilies = []string{"Fixture Sans", "Fixture Serif"}
	if err := d.Type("Fixture"); err != nil {
		t.Fatal(err)
	}
	if err := d.Key(ui.KeyDown, 0); err != nil {
		t.Fatal(err)
	}
	if err := d.Key(ui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if m.fontListOpen || m.settings.Appearance.FontFamily != "Fixture Serif" {
		t.Fatal("keyboard font selection failed", m.fontIndex, m.settings.Appearance.FontFamily)
	}
	click(t, d, fontChoiceID)
	m.fontFamilies = make([]string, 80)
	for i := range m.fontFamilies {
		m.fontFamilies[i] = "Fixture"
	}
	m.scrollEvent(ui.ScrollEvent{DeltaY: 200, Viewport: ui.Size{Width: 1180, Height: 760}})
	m.invalidator.Invalidate()
	if err := d.Pump(); err != nil {
		t.Fatal(err)
	}
	if m.fontScroll != 200 {
		t.Fatal("font list snapped back while scrolling")
	}
	for i := 0; i < 25; i++ {
		if err := d.Key(ui.KeyTab, 0); err != nil {
			t.Fatal(err)
		}
		focused := d.Snapshot().Focused
		inside := focused == fontFilterID || focused == closePanelID
		if _, ok := focused.In(fontItems); ok {
			inside = true
		}
		if !inside {
			t.Fatal("Tab escaped modal", focused)
		}
	}
	if err := d.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if m.fontListOpen || d.Snapshot().Focused != fontChoiceID {
		t.Fatal("Escape did not dismiss/restore")
	}
}

func TestCompositionDoesNotConfirmOrDismiss(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	click(t, d, nav.At(4))
	click(t, d, tabs.At(1))
	click(t, d, nav.At(1))
	// Demo has no source registry, so enter the browser directly; the entry
	// dialog itself is covered by the import-dialog tests.
	m.browseRemote = true
	m.loadCollections("playlists", true)
	click(t, d, playlistRows.At(0))
	click(t, d, confirmWriteID)
	click(t, d, editPlaylistID)
	input := m.textInput(app.TextInputContext{ID: writeFieldID})
	input.Editor.SetMarkedText("候选", text.Range{Start: 0, End: 2})
	for _, key := range []ui.Key{ui.KeyEnter, ui.KeyEscape} {
		if err := d.Key(key, 0); err != nil {
			t.Fatal(err)
		}
		if m.dialog != "description" {
			t.Fatal("composition key dismissed dialog")
		}
	}
	input.Editor.Unmark()
	if err := d.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if m.dialog != "" {
		t.Fatal("Escape after composition did not dismiss")
	}
	m.dialog = "delete"
	m.invalidator.Invalidate()
	if err := d.Pump(); err != nil {
		t.Fatal(err)
	}
	if d.Snapshot().Focused != cancelWriteID {
		t.Fatal("destructive dialog did not focus cancel")
	}
	if err := d.Key(ui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if m.dialog != "" || len(m.playlists) != 1 {
		t.Fatal("default Enter deleted data")
	}
}
