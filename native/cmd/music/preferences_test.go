package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
)

func TestSpaceShortcutAndTextEntry(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	click(t, d, allID)
	m.lyricLines = nil
	if err := d.Key(ui.KeySpace, 0); err != nil {
		t.Fatal(err)
	}
	if len(m.lyricLines) == 0 {
		t.Fatal("space did not invoke playback")
	}
	for _, id := range []ui.ID{searchID, writeFieldID, fontFilterID} {
		m.lyricLines = nil
		result := config(m).OnKey(ui.KeyEvent{Key: ui.KeySpace, Target: id})
		if result.Handled || len(m.lyricLines) != 0 {
			t.Fatal("space intercepted text input", id)
		}
	}
	m.lyricLines = nil
	config(m).OnKey(ui.KeyEvent{Key: ui.KeySpace, Repeat: true})
	if len(m.lyricLines) != 0 {
		t.Fatal("held space repeats playback")
	}
	click(t, d, nav.At(2))
	click(t, d, searchID)
	if err := d.Type("two words"); err != nil {
		t.Fatal(err)
	}
	if m.query.Text() != "two words" {
		t.Fatal("space lost from search", m.query.Text())
	}
}

func TestAppearanceControlsAndTheme(t *testing.T) {
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
	for _, hit := range d.Snapshot().Hits {
		if hit.ID == opacityID || hit.ID == radiusID {
			t.Fatal("hidden control remains interactive")
		}
	}
	old := append([]byte(nil), m.brandBitmap.Pix...)
	click(t, d, themes.At(1))
	if bytes.Equal(old, m.brandBitmap.Pix) {
		t.Fatal("brand did not follow accent")
	}
}

func TestCoverFailureRecoversAndCacheKeepsRecent(t *testing.T) {
	m := newModel()
	m.demo = true
	defer m.close()
	url := "https://fixture.dbankcdn.cn/cover"
	m.coverPending = map[string]bool{url: true}
	m.coverResults = []coverResult{{epoch: m.accountEpoch, url: url}}
	m.applyCovers()
	if _, ok := m.covers[url]; ok {
		t.Fatal("failed image permanently cached")
	}
	if !m.coverRetry[url].After(time.Now()) || m.coverPending[url] {
		t.Fatal("missing bounded retry")
	}
	m.coverResults = []coverResult{{epoch: m.accountEpoch, url: url, bitmap: ui.Bitmap{Width: 1, Height: 1, Stride: 4, Pix: []byte{0, 0, 0, 255}}}}
	m.applyCovers()
	if m.covers[url].Width != 1 || !m.coverRetry[url].IsZero() {
		t.Fatal("successful retry not installed")
	}
	m.coverResults = []coverResult{{epoch: m.accountEpoch, url: "album:a", resolved: url}}
	m.applyCovers()
	if m.albumCovers["album:a"] != url {
		t.Fatal("album cover fallback missing")
	}
}

func TestExpiryClearsAccountButNotOnNetworkFailure(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	old := m.accountEpoch
	if m.expireSession(source.NewError(music.SourceHuawei, "check", source.KindNetwork, errors.New("outage"))) {
		t.Fatal("outage logs out")
	}
	if !m.expireSession(source.NewError(music.SourceHuawei, "check", source.KindLogin, errors.New("expired"))) {
		t.Fatal("expiry ignored")
	}
	if m.accountEpoch == old || len(m.queue.Tracks) != 0 || m.status != "音乐会话已过期，请重新登录。" {
		t.Fatal("account not cleared")
	}
}

func TestSourceSettingsTab(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	m.openProfile(t.TempDir())
	m.openLocalLibrary()
	var canvas ui.Canvas
	d, err := app.NewDriver(config(m), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	click(t, d, nav.At(4))
	click(t, d, tabs.At(3))
	if m.settingsTab != 3 || m.settingsHeight() != 360 {
		t.Fatalf("source tab = %d height %.0f", m.settingsTab, m.settingsHeight())
	}
	click(t, d, sourceSettings.At(0))
	if m.sourceEnabled("huawei") {
		t.Fatal("source toggle did not disable Huawei")
	}
	click(t, d, sourceSettings.At(0))
	click(t, d, importFavoritesID)
	if m.dialog != "import" {
		t.Fatalf("import dialog = %q", m.dialog)
	}
	m.dialog = ""
	click(t, d, tabs.At(4))
	if m.settingsTab != 4 || m.settingsHeight() != 720 {
		t.Fatalf("advanced tab = %d height %.0f", m.settingsTab, m.settingsHeight())
	}
	click(t, d, jevEnableID)
	if !m.settings.Jev.Enabled {
		t.Fatal("Jev toggle did not enable")
	}
	m.settings.Jev.Model = "test-model"
	click(t, d, jevTestID)
	if !strings.Contains(m.jevTestStatus, "缺少 API Key") {
		t.Fatalf("test status = %q", m.jevTestStatus)
	}
	saved, err := m.store.LoadSettings()
	if err != nil || !saved.Jev.Enabled {
		t.Fatal("Jev setting not persisted")
	}
}
