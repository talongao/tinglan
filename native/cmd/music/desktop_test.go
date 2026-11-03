package main

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"tinglan/native/internal/lyrics"
)

func click(t *testing.T, d *app.Driver, id ui.ID) {
	t.Helper()
	for _, h := range d.Snapshot().Hits {
		if h.ID == id {
			if err := d.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("missing control %v", id)
}

func TestManualLyricsScrollPreservesLibrary(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	m.stageOpen = true
	m.scroll[0] = 123
	m.lyricLines = lyrics.Parse("[00:00]one\n[00:01]two\n[00:02]three\n[00:03]four")
	m.audioState.Position = 1.5
	m.scrollEvent(ui.ScrollEvent{X: 700, Y: 300, DeltaY: 65, Viewport: ui.Size{Width: 1180, Height: 760}})
	if !m.lyricManual || m.lyricScroll != 113 || m.scroll[0] != 123 {
		t.Fatal("lyrics scroll affected library or lost active position")
	}
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	m.resumeLyricFollow(m.lyricResumeAt.Add(time.Millisecond))
	if m.lyricManual {
		t.Fatal("follow not restored")
	}
}

func TestNavigationQueueAppearanceAndRestart(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	m.openProfile(t.TempDir())
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	click(t, d, allID)
	if len(m.queue.Tracks) != 2 {
		t.Fatal("play all omitted tracks")
	}
	click(t, d, queueID)
	if !m.queueOpen {
		t.Fatal("queue did not open")
	}
	click(t, d, lyricsID)
	if !m.stageOpen || m.queueOpen {
		t.Fatal("drawers are not exclusive")
	}
	click(t, d, closePanelID)
	m.stageChanged = time.Time{}
	m.invalidator.Invalidate()
	if err := d.Pump(); err != nil {
		t.Fatal(err)
	}
	click(t, d, nav.At(4))
	click(t, d, tabs.At(1))
	click(t, d, themes.At(1))
	settings, err := m.store.LoadSettings()
	if err != nil || settings.Appearance.Theme != "ocean" {
		t.Fatal("appearance not persisted")
	}
	click(t, d, nav.At(0))
	if len(m.queue.Tracks) != 2 {
		t.Fatal("page switch cleared queue")
	}
	queue, err := m.store.LoadQueue()
	if err != nil || len(queue.Tracks) != 2 {
		t.Fatal("queue not persisted")
	}
	if err := d.Resize(960, 640); err != nil {
		t.Fatal(err)
	}
	click(t, d, nav.At(1))
	// Demo has no source registry, so enter the browser directly; the entry
	// dialog itself is covered by the import-dialog tests.
	m.browseRemote = true
	m.loadCollections("playlists", true)
	click(t, d, playlistRows.At(0))
	click(t, d, confirmWriteID)
	if len(m.detailTracks) != 2 {
		t.Fatal("playlist detail did not load")
	}
}

func TestAll965AndPagePosition(t *testing.T) {
	m := newModel()
	m.enableDemo(965)
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if len(m.tracks) != 50 || m.complete {
		t.Fatal("first page claimed total")
	}
	m.scroll[0] = 222
	click(t, d, nav.At(4))
	click(t, d, nav.At(0))
	if m.scroll[0] != 222 {
		t.Fatal("scroll position lost")
	}
	click(t, d, allID)
	deadline := time.Now().Add(time.Second)
	for m.loading && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		m.wake()
	}
	if len(m.queue.Tracks) != 965 || !m.complete {
		t.Fatalf("play all: tracks=%d complete=%v", len(m.queue.Tracks), m.complete)
	}
}

func BenchmarkLibraryPaint(b *testing.B) {
	m := newModel()
	m.enableDemo(965)
	cfg := config(m)
	var canvas ui.Canvas
	size := ui.Size{Width: 1180, Height: 760}
	cfg.Paint(&canvas, size)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		canvas.Reset()
		cfg.Paint(&canvas, size)
	}
}

func TestSettingsSelectionsAndDetailScroll(t *testing.T) {
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
	m.scroll[0] = 120
	*m.trackScroll() = 240
	if err := d.Resize(1180, 760); err != nil {
		t.Fatal(err)
	}
	click(t, d, backID)
	if m.scroll[0] != 120 {
		t.Fatal("detail replaced parent scroll")
	}
	click(t, d, nav.At(4))
	click(t, d, settingsQualityID)
	click(t, d, settingOptions.At(1))
	click(t, d, settingsRepeatID)
	click(t, d, settingOptions.At(3))
	s, err := m.store.LoadSettings()
	if err != nil || s.Quality != "lossless" || !s.Shuffle || !m.queue.Shuffle {
		t.Fatal("settings choices not applied")
	}
	click(t, d, nav.At(1))

}

func TestTranslatedStageLinesAndFontPresets(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	m.lyricLines = lyrics.Parse("[00:00]one\n[00:00]translation\n[00:01]two")
	if m.stageLineOffset(1) != 74 {
		t.Fatal("translated lines overlap next verse")
	}
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	click(t, d, nav.At(4))
	click(t, d, tabs.At(1))
	click(t, d, fontChoiceID)
	m.fontFamilies = []string{"Test Sans", "Other Serif", "测试字体"}
	m.fontFilter.SetText("SANS")
	names := m.filteredFonts()
	if len(names) != 1 || names[0] != "Test Sans" {
		t.Fatal("font filtering failed", names)
	}
	m.invalidator.Invalidate()
	if err := d.Pump(); err != nil {
		t.Fatal(err)
	}
	click(t, d, fontItems.At(0))
	if m.settings.Appearance.FontFamily != "Test Sans" {
		t.Fatal("filtered font not selected")
	}
	m.fontFilter.SetText("测试")
	if len(m.filteredFonts()) != 1 {
		t.Fatal("Chinese font filtering failed")
	}
}

func TestStageCompactBlankLinesAndAutomaticFollow(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	defer m.close()
	m.lyricLines = lyrics.Parse("[00:00]one\n[00:01]\n[00:02]two\n[00:03]   \n[00:04]three")
	if m.stageLineOffset(2) != 48 || m.stageLineOffset(4) != 96 {
		t.Fatal("empty timing rows create visual gaps")
	}
	now := time.Now()
	m.pauseLyricFollow(now)
	m.resumeLyricFollow(now.Add(4 * time.Second))
	if !m.lyricManual {
		t.Fatal("follow resumed early")
	}
	m.pauseLyricFollow(now.Add(4 * time.Second))
	m.resumeLyricFollow(now.Add(6 * time.Second))
	if !m.lyricManual {
		t.Fatal("interaction did not extend timeout")
	}
	m.resumeLyricFollow(now.Add(9 * time.Second))
	if m.lyricManual {
		t.Fatal("follow did not resume")
	}
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	click(t, d, lyricsID)
	if !m.stageOpen {
		t.Fatal("lyrics did not open stage")
	}
	m.stageChanged = time.Time{}
	m.invalidator.Invalidate()
	if err := d.Pump(); err != nil {
		t.Fatal(err)
	}
	click(t, d, lyricsMenuID)
	click(t, d, offsetPlusID)
	if m.lyricOffset != .5 {
		t.Fatal("offset menu failed")
	}
	found := false
	for _, h := range d.Snapshot().Hits {
		if h.ID == importLyricsID {
			found = true
		}
	}
	if !found {
		t.Fatal("missing import lyrics action")
	}
	if config(m).WindowMode != app.WindowCustomTitlebar {
		t.Fatal("system titlebar still enabled")
	}
}

func TestLyricsDragDoesNotSeekAndResumesAfterRelease(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	defer m.close()
	m.stageOpen = true
	m.lyricLines = lyrics.Parse("[00:00]one\n[00:01]two\n[00:02]three\n[00:03]four")
	m.pointerEvent(ui.PointerEvent{Kind: ui.PointerDown, Target: lyricRows.At(0), Y: 300})
	m.pointerEvent(ui.PointerEvent{Kind: ui.PointerMove, Target: lyricRows.At(0), Y: 240, Captured: true})
	if !m.lyricManual || !m.lyricDragged || m.lyricScroll != 60 {
		t.Fatal("drag did not scroll")
	}
	m.resumeLyricFollow(time.Now().Add(time.Minute))
	if !m.lyricManual {
		t.Fatal("follow resumed during drag")
	}
	m.pointerEvent(ui.PointerEvent{Kind: ui.PointerUp, Target: lyricRows.At(0), Y: 240})
	m.resumeLyricFollow(m.lyricResumeAt)
	if m.lyricManual || m.lyricDragging {
		t.Fatal("drag follow did not resume")
	}
}

func TestWebViewGuideDialogRenders(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	m.dialog = "webview-guide"
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	click(t, d, webViewGuideCancelID)
	if m.dialog != "" {
		t.Fatal("guide dialog did not close")
	}
}

func TestNavLeavesRemotePlaylistBrowser(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	m.view = 1
	m.browseRemote = true
	click(t, d, nav.At(1))
	if m.browseRemote {
		t.Fatal("re-entering 我的歌单 kept the remote browser open")
	}
}
