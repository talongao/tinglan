package main

import (
	"errors"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
	"tinglan/native/internal/lyrics"
)

func TestCoverBothPresentationsAndTransportMenus(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	click(t, d, allID)
	click(t, d, repeatID)
	if m.settings.RepeatMode != "off" || m.transportMenu != "mode" {
		t.Fatal("opening menu changed mode")
	}
	click(t, d, transportOptions.At(3))
	if !m.settings.Shuffle || m.transportMenu != "" {
		t.Fatal("selection failed")
	}
	click(t, d, qualityID)
	click(t, d, transportOptions.At(1))
	if m.settings.Quality != "lossless" {
		t.Fatal("quality selection failed")
	}
	click(t, d, dockCoverID)
	m.stageChanged = time.Time{}
	m.invalidator.Invalidate()
	if err = d.Pump(); err != nil {
		t.Fatal(err)
	}
	big, small := false, false
	for _, label := range d.Snapshot().Labels {
		if label.BitmapID == 500+(1<<40) {
			big = true
		}
		if label.BitmapID == 500 {
			small = true
		}
	}
	if !big || !small {
		t.Fatal("large and dock cover must coexist", big, small)
	}
}
func TestRefreshKeepsCompleteLibraryAndCollectionErrorsAreLocal(t *testing.T) {
	m := newModel()
	m.enableDemo(965)
	defer m.close()
	m.tracks = syntheticTracks(965)
	m.complete = true
	m.results = []libraryResult{{kind: "playlists", epoch: m.collectionGeneration, err: errors.New("fixture error")}}
	status := m.status
	m.applyLibraryResults()
	if m.status != status || !m.collectionFailed {
		t.Fatal("collection error leaked into global notice")
	}
	m.refreshLibrary()
	if len(m.tracks) != 965 || !m.complete {
		t.Fatal("refresh erased known complete count")
	}
	deadline := time.Now().Add(time.Second)
	for m.loading && time.Now().Before(deadline) {
		m.applyLibraryResults()
		time.Sleep(time.Millisecond)
	}
	if len(m.tracks) != 965 || !m.complete || m.loading {
		t.Fatal("refresh did not atomically install full index")
	}
}
func TestCenteredPillUsesVisibleGlyphBounds(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var b ui.Rect
	for _, hit := range d.Snapshot().Hits {
		if hit.ID == lyricsID {
			b = hit.Bounds
		}
	}
	found := false
	for _, label := range d.Snapshot().Labels {
		if label.BitmapWidth == 0 || label.Bounds.X < b.X || label.Bounds.X+label.Bounds.Width > b.X+b.Width || label.Bounds.Y < b.Y || label.Bounds.Y+label.Bounds.Height > b.Y+b.Height {
			continue
		}
		if math.Abs(float64(label.Bounds.X+label.Bounds.Width/2-b.X-b.Width/2)) > 1 || math.Abs(float64(label.Bounds.Y+label.Bounds.Height/2-b.Y-b.Height/2)) > 1 {
			t.Fatal("pill glyph not centered")
		}
		found = true
	}
	if !found {
		t.Fatal("missing pill glyph")
	}
}

func TestVolumePopoverInteraction(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	initial := m.settings.Volume
	click(t, d, volumeID)
	if m.transportMenu != "volume" || m.settings.Volume != initial {
		t.Fatal("opening volume must not mute or change volume")
	}
	size := ui.Size{Width: 1180, Height: 760}
	b := volumeTrackBounds(size)
	for _, fraction := range []float32{.25, .75, 0, 1} {
		result := m.pointerEvent(ui.PointerEvent{Kind: ui.PointerDown, Target: volumeSliderID, X: b.X + b.Width*fraction, Y: b.Y + 12, Viewport: size})
		if !result.Handled || math.Abs(m.settings.Volume-float64(fraction)) > .001 || m.transportMenu != "volume" {
			t.Fatal("volume adjustment closed popup or used wrong bounds")
		}
	}
	m.pointerEvent(ui.PointerEvent{Kind: ui.PointerMove, Captured: true, Target: volumeSliderID, X: b.X + b.Width*.5, Viewport: size})
	if m.settings.Volume != .5 {
		t.Fatal("drag did not update volume")
	}
	config(m).OnKey(ui.KeyEvent{Key: ui.KeyRight, Target: volumeSliderID})
	if math.Abs(m.settings.Volume-.55) > .001 {
		t.Fatal("keyboard did not update volume")
	}
	if err := d.Key(ui.KeyEscape, 0); err != nil || m.transportMenu != "" {
		t.Fatal("escape did not close volume", err)
	}
	click(t, d, volumeID)
	click(t, d, repeatID)
	if m.transportMenu != "mode" {
		t.Fatal("menus must be exclusive")
	}
	click(t, d, volumeID)
	m.pointerEvent(ui.PointerEvent{Kind: ui.PointerDown, X: 400, Y: 100, Viewport: size})
	if m.transportMenu != "" {
		t.Fatal("outside click did not close popup")
	}
}

// Native macOS hashes a texture by destination dimensions. Every changed size
// must carry pixels, even when URL, source pixels and image slot stay the same.
func TestCoverResizeUploadsPixels(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	var canvas ui.Canvas
	b := rect(40, 600, 52, 52)
	cfg := config(m)
	cfg.Paint = func(c *ui.Canvas, size ui.Size) {
		m.trackCover(c, m.tracks[0], b)
		m.trackCover(c, m.tracks[0], rect(40, 674, 52, 52))
	}
	d, err := app.NewDriver(cfg, &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, side := range []float32{70, 120, 180, 240, 300, 280, 220, 90, 52} {
		b.Width, b.Height = side, side
		m.invalidator.Invalidate()
		if err := d.Pump(); err != nil {
			t.Fatal(err)
		}
		labels := d.Snapshot().Labels
		if len(labels) != 2 || !labels[0].BitmapUpload || len(labels[0].Bitmap) == 0 {
			t.Fatalf("resized cover at %v lost its pixel upload", side)
		}
		if labels[1].BitmapUpload {
			t.Fatal("unchanged small cover must retain its texture")
		}
	}
}

func TestSidebarHasNoSessionCaption(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, label := range d.Snapshot().Labels {
		if strings.Contains(label.Value, "音乐会话已连接") || label.Value == "离线合成测试" {
			t.Fatal("sidebar session caption is still present")
		}
	}
}

func TestTransportHoverHasVisibleFeedback(t *testing.T) {
	for _, theme := range []string{"light", "midnight"} {
		m := newModel()
		m.enableDemo(2)
		m.settings.Appearance.Theme = theme
		m.queue.Replace(m.tracks, 0)
		var canvas ui.Canvas
		d, err := app.NewDriver(config(m), &canvas)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []ui.ID{toggleID, volumeID, qualityID, lyricsID, queueID} {
			if err := d.Move(600, 100); err != nil {
				t.Fatal(err)
			}
			before := d.Snapshot()
			for _, hit := range before.Hits {
				if hit.ID == id {
					if err := d.Move(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
				}
			}
			after := d.Snapshot()
			if after.Hovered != id || reflect.DeepEqual(before.Primitives, after.Primitives) {
				t.Fatal("missing hover feedback", theme, id)
			}
		}
		d.Close()
	}
}

func TestRomanizationBelowMainLyric(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	m.queue.Replace(m.tracks, 0)
	m.stageOpen = true
	m.lyricLines = lyrics.Parse("[00:01]梦里归家那一扇灯^mong lui guai ga na ya sin dang\n[00:01]The light back home\n[00:03]心中所属唯独你^san zong so su wai du nei")
	m.audioState.Position = 1
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, width := range []int{1180, 960} {
		if err := d.Resize(width, 760); err != nil {
			t.Fatal(err)
		}
		positions := map[string]ui.Rect{}
		for _, text := range d.Snapshot().Texts {
			positions[text.Value] = text.Bounds
		}
		main, phonetic, translation := positions[m.lyricLines[0].Text], positions[m.lyricLines[0].Romanization], positions[m.lyricLines[0].Translation]
		if main.Width == 0 || phonetic.Width == 0 || translation.Width == 0 || phonetic.Y < main.Y+main.Height || translation.Y < phonetic.Y+phonetic.Height {
			t.Fatal("main lyric and subtitles overlap or disappeared", width, main, phonetic, translation)
		}
		if m.stageLineOffset(1) != 100 {
			t.Fatal("follow offset must include both subtitle rows")
		}
	}
}

func TestInlineTranslationBelowEnglishLyric(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	m.queue.Replace(m.tracks, 0)
	m.stageOpen = true
	m.lyricLines = lyrics.Parse("[00:01]Sing^唱吧\n[00:03]I'm sittin' here I missed the power^坐在这儿 浑身无力")
	m.audioState.Position = 1
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, width := range []int{1180, 960} {
		if err := d.Resize(width, 760); err != nil {
			t.Fatal(err)
		}
		texts := map[string]ui.Text{}
		for _, text := range d.Snapshot().Texts {
			texts[text.Value] = text
		}
		for _, line := range m.lyricLines {
			main, sub := texts[line.Text], texts[line.Translation]
			if main.Bounds.Width == 0 || sub.Bounds.Width == 0 || sub.Bounds.Y < main.Bounds.Y+main.Bounds.Height || sub.Style.Size >= main.Style.Size {
				t.Fatal("English translation must be smaller and below its original", width)
			}
		}
		if m.stageLineOffset(1) != 74 {
			t.Fatal("translation missing from follow offset")
		}
	}
}
