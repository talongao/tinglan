package main

import (
	"fmt"
	"math"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"huawei-music-desktop/native/internal/huawei"
	"huawei-music-desktop/native/internal/icons"
	"huawei-music-desktop/native/internal/lyrics"
	"huawei-music-desktop/native/internal/visual"
)

var (
	shell             = tree.Feature("shell")
	nav               = tree.Sub(shell, tree.Name("navigation"))
	rows              = tree.Sub(shell, tree.Name("tracks"))
	rowPlay           = tree.Sub(shell, tree.Name("track-play"))
	hearts            = tree.Sub(shell, tree.Name("favorites"))
	queueRows         = tree.Sub(shell, tree.Name("queue"))
	tabs              = tree.Sub(shell, tree.Name("settings-tabs"))
	accents           = tree.Sub(shell, tree.Name("accents"))
	refreshID         = tree.ID(shell, "refresh")
	allID             = tree.ID(shell, "play-all")
	moreID            = tree.ID(shell, "load-more")
	queueID           = tree.ID(shell, "queue-toggle")
	lyricsID          = tree.ID(shell, "lyrics-toggle")
	closePanelID      = tree.ID(shell, "close-panel")
	toggleID          = tree.ID(shell, "play-toggle")
	nextID            = tree.ID(shell, "next")
	previousID        = tree.ID(shell, "previous")
	shuffleID         = tree.ID(shell, "shuffle")
	repeatID          = tree.ID(shell, "repeat")
	qualityID         = tree.ID(shell, "quality")
	volumeID          = tree.ID(shell, "volume")
	volumeSliderID    = tree.ID(shell, "volume-slider")
	settingsQualityID = tree.ID(shell, "settings-quality")
	settingsRepeatID  = tree.ID(shell, "settings-repeat")
	progressID        = tree.ID(shell, "progress")
	opacityID         = tree.ID(shell, "opacity")
	radiusID          = tree.ID(shell, "radius")
	fontScaleID       = tree.ID(shell, "font-scale")
	resetID           = tree.ID(shell, "reset-appearance")
	searchID          = tree.ID(shell, "search")
	submitID          = tree.ID(shell, "submit-search")
	playlistRows      = tree.Sub(shell, tree.Name("playlists"))
	backID            = tree.ID(shell, "back-playlists")
	historySourceID   = tree.ID(shell, "history-source")
	stageID           = tree.ID(shell, "lyrics-stage")
	offsetMinusID     = tree.ID(shell, "lyrics-earlier")
	offsetPlusID      = tree.ID(shell, "lyrics-later")
	lyricRows         = tree.Sub(shell, tree.Name("lyric-lines"))
)
var navLabels = []string{"我的收藏", "我的歌单", "搜索", "播放历史", "设置"}
var eyebrows = []string{"LIBRARY", "COLLECTIONS", "SEARCH", "HISTORY", "PREFERENCES"}
var navIcons = []ui.Icon{icons.Heart, icons.ListMusic, icons.Search, icons.History, icons.Settings}
var accentNames = []string{"red", "blue", "green", "amber", "violet"}
var accentLabels = []string{"红色", "蓝色", "绿色", "琥珀", "紫色"}
var accentColors = []uint32{0xc8202e, 0x2563eb, 0x15803d, 0xb45309, 0x6d28d9}

func color(hex uint32, alpha float32) ui.Color {
	return ui.Color{Red: float32((hex>>16)&255) / 255, Green: float32((hex>>8)&255) / 255, Blue: float32(hex&255) / 255, Alpha: alpha}
}

func (m *model) accent() ui.Color {
	return color(m.palette().accent, 1)
}
func (m *model) text(c *ui.Canvas, value string, b ui.Rect, size, weight float32, tint ui.Color) {
	c.Clip(b)
	w := textWeight(weight)
	if !m.customText(c, value, b, size*float32(m.settings.Appearance.FontScale), w, tint) {
		c.Label(value, b, ui.TextStyle{Size: size * float32(m.settings.Appearance.FontScale), Weight: w, Color: tint})
	}
	c.Unclip()
}
func textWeight(weight float32) float32 {
	w := float32(0)
	if weight >= 500 {
		w = ui.WeightMedium
	}
	if weight >= 600 {
		w = ui.WeightSemibold
	}
	if weight >= 700 {
		w = ui.WeightBold
	}
	return w
}
func rect(x, y, w, h float32) ui.Rect { return ui.Rect{X: x, Y: y, Width: w, Height: h} }
func (m *model) panel(c *ui.Canvas, b ui.Rect, radius, alpha float32) {
	c.Shadow(b, radius, ui.ShadowStyle{OffsetY: 1, Blur: 2}, color(0x101216, .04))
	c.Shadow(b, radius, ui.ShadowStyle{OffsetY: 14, Blur: 40}, color(0x101216, .07))
	visual.Border(c, b, radius, 1, m.line(.08), m.surface(alpha))
}
func hit(c *ui.Canvas, id ui.ID, b ui.Rect, label string) bool {
	c.HitWithLabel(id, b, label)
	return c.Clicked(id)
}
func (m *model) pill(c *ui.Canvas, id ui.ID, b ui.Rect, label string, primary bool) bool {
	fill, tint := m.surface(.85), m.secondary()
	if primary {
		fill, tint = m.accent(), color(0xffffff, 1)
		if m.palette().dark {
			tint = color(m.palette().base, 1)
		}
	}
	if c.Hovered() == id || c.FocusVisible() == id {
		fill = m.controlHoverFill()
		tint = m.ink()
	}
	if c.Pressed() == id {
		tint = m.accent()
	}
	visual.Border(c, b, b.Height/2, 1, m.line(.085), fill)
	m.centerText(c, label, b, 12, 500, tint)
	return hit(c, id, b, label)
}
func (m *model) iconButton(c *ui.Canvas, id ui.ID, b ui.Rect, icon ui.Icon, label string, active bool) bool {
	tint := m.muted()
	if active {
		tint = m.accent()
	}
	if c.Hovered() == id || c.FocusVisible() == id {
		c.Rounded(b, b.Height/2, m.controlHoverFill())
		tint = m.accent()
	}
	size := float32(19)
	c.DrawIcon(icons.RoundedStroke(icon), rect(b.X+(b.Width-size)/2, b.Y+(b.Height-size)/2, size, size), tint)
	return hit(c, id, b, label)
}
func sidebarWidth(width float32) float32 {
	if width <= 1020 {
		return 76
	}
	if width <= 1100 {
		return 200
	}
	return 234
}

// The three CSS radial washes are sampled only on resize, not on every paint.
// This reproduces the background field; it is not a substitute for backdrop blur.
func (m *model) prepareBackground(size ui.Size) {
	if m.backgroundSize == size {
		return
	}
	m.backgroundSize = size
	w, h := int(size.Width), int(size.Height)
	pixels := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			base := color(m.palette().base, 1)
			r, g, b := float64(base.Red*255), float64(base.Green*255), float64(base.Blue*255)
			layers := [][8]float64{{.06, -.10, 1.2, .8, .46, 200, 32, 46}, {.96, .04, .9, .7, .48, 88, 120, 190}, {.60, 1.1, .9, .9, .55, 215, 180, 120}}
			for i, l := range layers {
				dx := (float64(x)/float64(w) - l[0]) / l[2]
				dy := (float64(y)/float64(h) - l[1]) / l[3]
				a := max(0, 1-math.Hypot(dx, dy)/l[4]) * .1
				if i == 2 {
					a *= 1.4
				}
				r = r*(1-a) + l[5]*a
				g = g*(1-a) + l[6]*a
				b = b*(1-a) + l[7]*a
			}
			at := (y*w + x) * 4
			pixels[at], pixels[at+1], pixels[at+2], pixels[at+3] = uint8(b), uint8(g), uint8(r), 255
		}
	}
	m.background = ui.Bitmap{Pix: pixels, Width: w, Height: h, Stride: w * 4, ID: 1, Version: m.background.Version + 1}
	m.backgroundGlass = ui.Bitmap{}
	m.backgroundGlass.ID = 2
	m.backgroundGlass.Version = m.background.Version
}

func config(m *model) app.Config {
	m.prepareBackground(ui.Size{Width: 1180, Height: 760})
	return app.Config{WindowMode: app.WindowCustomTitlebar, TitlebarHeight: 28,
		WindowControls: func() app.WindowControlsStyle {
			tint := m.ink()

			return app.WindowControlsStyle{Foreground: tint, Minimize: "最小化", Maximize: "最大化", Restore: "还原", CloseName: "关闭窗口"}
		}, Title: appDisplayName, Tree: &tree, Width: 1180, Height: 760,
		Invalidator: &m.invalidator, OnWake: m.wake, OnClosed: m.close, Paint: m.paint,
		OnResize: m.prepareBackground, TextInput: m.textInput, OnScroll: m.scrollEvent, OnPointer: m.pointerEvent,
		OnKey: func(e ui.KeyEvent) ui.EventResult {
			if m.composing(e.Target) && (e.Key == ui.KeyEnter || e.Key == ui.KeyEscape || e.Key == ui.KeyUp || e.Key == ui.KeyDown) {
				return ui.EventResult{Handled: true}
			}
			if m.fontListOpen && e.Target == fontFilterID && (e.Key == ui.KeyUp || e.Key == ui.KeyDown) {
				names := m.filteredFonts()
				delta := 1
				if e.Key == ui.KeyUp {
					delta = -1
				}
				m.fontIndex = max(0, min(len(names)-1, m.fontIndex+delta))
				m.fontReveal = true
				return ui.EventResult{Handled: true, Repaint: true}
			}

			if e.Key == ui.KeyEscape && m.transportMenu != "" {
				m.transportMenu = ""
				return ui.EventResult{Handled: true, Repaint: true}
			}
			if e.Key == ui.KeySpace && e.Modifiers == 0 && e.Target != searchID && e.Target != writeFieldID && e.Target != fontFilterID && m.dialog == "" && !m.fontListOpen {
				if !e.Repeat && !e.Release {
					m.togglePlayback()
				}
				return ui.EventResult{Handled: true, Repaint: true}
			}
			if e.Target == progressID && m.player != nil {
				delta := 0.0
				if e.Key == ui.KeyLeft {
					delta = -5
				}
				if e.Key == ui.KeyRight {
					delta = 5
				}
				if delta != 0 {
					m.seekPlayback(m.audioState.Position + delta)
					return ui.EventResult{Handled: true, Repaint: true}
				}
			}
			if e.Target == volumeSliderID {
				if next, ok := components.SliderKeyDown(float32(m.settings.Volume), 0, 1, .05, components.LTR, e.Key, e.Modifiers); ok {
					m.settings.Volume = float64(next)
					m.saveSettings()
					return ui.EventResult{Handled: true, Repaint: true}
				}
			}
			var value, low, high, step float32
			switch e.Target {
			case opacityID:
				value = float32(m.settings.Appearance.PanelOpacity)
				low = .55
				high = 1
				step = .01
			case radiusID:
				value = float32(m.settings.Appearance.CornerRadius)
				high = 18
				step = 1
			case fontScaleID:
				value = float32(m.settings.Appearance.FontScale)
				low = .85
				high = 1.2
				step = .01
			}
			if step > 0 {
				if next, ok := components.SliderKeyDown(value, low, high, step, components.LTR, e.Key, e.Modifiers); ok {
					switch e.Target {
					case opacityID:
						m.settings.Appearance.PanelOpacity = float64(next)
					case radiusID:
						m.settings.Appearance.CornerRadius = float64(next)
					case fontScaleID:
						m.settings.Appearance.FontScale = float64(next)
					}
					m.saveSettings()
					return ui.EventResult{Handled: true, Repaint: true}
				}
			}
			if e.Key == ui.KeyEscape {
				// Modal Escape is routed through Canvas so IME and focus restoration work.
				if m.fontListOpen || m.dialog != "" {
					return ui.EventResult{}
				}
				if m.settingsSelect != "" {
					m.settingsSelect = ""
					return ui.EventResult{Handled: true, Repaint: true}
				}
				if m.queueOpen {
					m.queueOpen = false
					return ui.EventResult{Handled: true, Repaint: true}
				}
				if m.lyricsMenuOpen {
					m.lyricsMenuOpen = false
					return ui.EventResult{Handled: true, Repaint: true}
				}
				m.settingsSelect = ""
				m.fontListOpen = false
				m.dialog = ""
				m.setStage(false)
				m.queueOpen = false
				m.lyricsMenuOpen = false
				return ui.EventResult{Handled: true, Repaint: true}
			}
			return ui.EventResult{}
		},
	}
}

func (m *model) paint(c *ui.Canvas, size ui.Size) {
	defer m.paintTitlebar(c, size)
	if !m.stageChanged.IsZero() && time.Since(m.stageChanged) < 320*time.Millisecond {
		if m.stageTimer != nil {
			m.stageTimer.Stop()
		}
		m.stageTimer = time.AfterFunc(16*time.Millisecond, m.requestUpdate)
	}
	if m.stageOpen || m.stageAmount(time.Now()) > .001 {
		m.paintStage(c, size)
		return
	}
	c.DrawBackdrop(m.background, rect(0, 0, float32(m.background.Width), float32(m.background.Height)), rect(0, 0, size.Width, size.Height))
	rail := sidebarWidth(size.Width)
	x := rail + 20
	c.Fill(rect(0, 0, x, size.Height), m.surface(.32))
	c.Fill(rect(x, 0, size.Width-x, size.Height), m.surface(.54))
	c.Fill(rect(x, 0, 1, size.Height), m.line(.10))
	m.paintSidebar(c, size, rail)
	pad := float32(30)
	if size.Width <= 1100 {
		pad = 22
	}
	left, right := x+pad+1, size.Width-10-pad-9
	headerHeight := float32(123.8)
	eyebrow, title := eyebrows[m.view], navLabels[m.view]
	if m.entity != nil {
		headerHeight -= 18
		eyebrow, title = "ALBUM", "专辑"
		if m.entityKind == "artist" {
			eyebrow, title = "ARTIST", "歌手"
		}
	}
	m.text(c, eyebrow, rect(left, 37, 280, 14), 10, 700, m.muted())
	m.text(c, title, rect(left, 59, 330, 34.8), 30, 680, m.ink())
	subtitle := "收藏的全部歌曲，向下滚动自动加载"
	if m.view == 1 {
		subtitle = "收藏与自建歌单"
	}
	if m.view == 4 {
		subtitle = "播放、界面与隐私偏好"
	}
	if m.view == 2 {
		subtitle = "搜索歌曲与艺人"
	}
	if m.entity == nil {
		m.text(c, subtitle, rect(left, 100, 350, 18), 12, 400, m.muted())
	}
	if m.demo {
		m.text(c, "离线合成数据", rect(right-150, 96, 150, 20), 12, 400, m.muted())
	} else {
		label := "扫码登录"
		if m.session != nil {
			label = "重新登录"
		}
		if m.busy {
			label = "取消登录"
		}
		if m.pill(c, loginID, rect(right-110, 78, 110, 36), label, false) {
			if m.busy {
				m.cancelLogin()
			} else {
				m.startLogin()
			}
		}
	}
	m.text(c, m.status, rect(left, 16.2+headerHeight, right-left, 22), 12, 400, m.muted())
	content := rect(left, 55+headerHeight, right-left, size.Height-181-headerHeight)
	if m.entity != nil {
		m.paintEntity(c, content)
	} else {
		switch m.view {
		case 0:
			m.paintLibrary(c, content, m.tracks, false)
		case 2:
			m.paintSearch(c, content)
		case 4:
			m.paintSettings(c, content)
		case 1:
			m.paintCollections(c, content)
		case 3:
			m.paintHistory(c, content)
		}
	}
	m.paintDock(c, size)
	if m.queueOpen {
		m.paintDrawer(c, size)
	}
	if m.dialog != "" {
		m.paintWriteDialog(c, size)
	}
	if m.fontListOpen {
		m.paintFontList(c, size)
	}
}

func (m *model) paintSidebar(c *ui.Canvas, size ui.Size, rail float32) {
	brand := rect(33, 33, 36, 36)
	shadow := m.accent()
	shadow.Alpha = .25
	c.Shadow(brand, 12, ui.ShadowStyle{OffsetY: 6, Blur: 14}, shadow)
	if m.brandBitmap.Width == 0 || m.brandTint != m.accent() {
		m.brandTint = m.accent()
		m.brandBitmap = icons.ThemedBrand(m.brandTint, m.brandBitmap.Version+1)
	}
	c.DrawBitmap(m.brandBitmap, rect(31, 31, 40, 40))
	if rail > 76 {
		m.text(c, appBrandLabel, rect(79, 40, rail-78, 28), 15, 650, m.ink())
		m.text(c, "音乐库", rect(35, 99, 120, 14), 10, 700, m.muted())
	}
	for i, label := range navLabels {
		b := rect(25, 123+float32(i)*41, rail-30, 38)
		if m.view == i && m.entity == nil {
			tint := m.accent()
			tint.Alpha = .08
			c.Rounded(b, 12, tint)
			if rail > 76 {
				c.Rounded(rect(27, b.Y+12, 3, 16), 1.5, m.accent())
			}
		} else if c.Hovered() == nav.At(i) {
			c.Rounded(b, 11, color(0x16181c, .042))
		}
		tint := m.muted()
		if m.view == i && m.entity == nil {
			tint = m.accent()
		}
		badge := rect(32, b.Y+5, 28, 28)
		badgeFill := m.surface(.65)
		if m.view == i && m.entity == nil {
			badgeFill = m.ink()
			tint = m.surface(1)
		}
		c.Rounded(badge, 9, badgeFill)
		c.DrawIcon(icons.RoundedStroke(navIcons[i]), rect(38, b.Y+11, 16, 16), tint)
		if rail > 76 {
			weight := float32(500)
			tint := m.secondary()
			if m.view == i && m.entity == nil {
				weight = 650
				tint = m.ink()
			}
			m.text(c, label, rect(72, b.Y+10, rail-81, 18), 13, weight, tint)
		}
		if hit(c, nav.At(i), b, label) {
			m.entity = nil
			m.entityGeneration++
			m.view = i
			m.settingsSelect = ""
			m.transportMenu = ""
			if i == 4 && m.store != nil {
				m.lyricStats = m.store.LyricsInfo()
			}
			if i == 1 {
				m.loadCollections("playlists")
			}
		}
	}

}

func durationText(duration *float64) string {
	if duration == nil {
		return "—"
	}
	return fmt.Sprintf("%d:%02d", int(*duration)/60, int(*duration)%60)
}

func (m *model) paintLibrary(c *ui.Canvas, b ui.Rect, tracks []huawei.Track, search bool) {
	m.paintTrackList(c, b, tracks, search, false)
}
func (m *model) paintTrackList(c *ui.Canvas, b ui.Rect, tracks []huawei.Track, search, detail bool) {
	boxHeight := min(b.Height, float32(len(tracks))*58+155)
	if detail {
		boxHeight = b.Height
	}
	searchResults := search && m.view == 2 && m.entity == nil
	if !detail && !searchResults {
		count := fmt.Sprintf("已加载 %d 首", len(tracks))
		if m.complete && !search {
			count = fmt.Sprintf("共 %d 首收藏", len(tracks))
		}
		m.text(c, count, rect(b.X+21, b.Y+19, 240, 21), 15, 650, m.ink())
		detailLabel := fmt.Sprintf("已加载 %d 首", len(tracks))
		if !search && m.libraryRefreshing {
			detailLabel = "正在更新，保留上次完整列表"
		}
		m.text(c, detailLabel, rect(b.X+21, b.Y+42, 260, 16), 11, 400, m.muted())
		playAllBounds := rect(b.X+b.Width-171, b.Y+22, 64, 34)
		m.text(c, "播放全部", playAllBounds, 12, 600, m.accent())
		if hit(c, allID, playAllBounds, "播放全部") {
			if search || m.complete {
				m.startTracks(tracks, 0)
			} else {
				m.playAllPending = true
				m.loadLibrary(true)
			}
		}
		if !search && m.pill(c, refreshID, rect(b.X+b.Width-101, b.Y+21, 80, 34), "刷新收藏", false) {
			if m.catalog != nil && !m.loading {
				m.refreshLibrary()
			}
		}
	}
	viewport := rect(b.X+21, b.Y+70, b.Width-42, max(0, boxHeight-90))
	if detail {
		viewport = b
	}
	if searchResults {
		viewport = rect(b.X+21, b.Y, b.Width-42, max(0, b.Height-20))
	}
	c.Clip(viewport)
	offset := *m.trackScroll()
	for i, t := range tracks {
		y := viewport.Y + float32(i)*58 - offset
		if y+58 < viewport.Y || y > viewport.Y+viewport.Height {
			continue
		}
		r := rect(viewport.X, y, viewport.Width, 58)

		current := m.queue.Current() != nil && m.queue.Current().ID == t.ID
		if current {
			tint := m.accent()
			tint.Alpha = .09
			c.Rounded(r, 11, tint)
		} else if c.Hovered() == rows.At(i) {
			c.Rounded(r, 11, color(0x16181c, .042))
		}
		if i == m.selected {
			visual.Border(c, r, 11, 1, m.line(.14), ui.Color{})
		}
		if hit(c, rows.At(i), r, t.Title+"，"+t.ArtistText()) {
			m.selected = i
			if c.ClickedByKey(rows.At(i)) {
				m.playListTrack(tracks, i)
			}
		}
		if current || c.Hovered() == rows.At(i) || c.Hovered() == rowPlay.At(i) {
			playBounds := rect(r.X+10, y+16, 26, 26)
			c.Rounded(playBounds, 13, m.accent())
			icon := icons.PlayFilled
			if current && m.audioRunning {
				icon = icons.Pause
			}
			c.DrawIcon(icon, rect(playBounds.X+7, playBounds.Y+7, 12, 12), color(0xffffff, 1))
			if hit(c, rowPlay.At(i), playBounds, "播放 "+t.Title) {
				if current {
					m.togglePlayback()
				} else {
					m.playListTrack(tracks, i)
				}
			}
		} else {
			m.text(c, fmt.Sprintf("%02d", i+1), rect(r.X+17, y+20, 26, 18), 11, 400, color(0xb6bac1, 1))
		}
		titleX := r.X + 42
		if b.Width > 740 {
			c.Rounded(rect(titleX+10, y+9, 40, 40), 11, color(0xb6bac1, .25))
			c.DrawIcon(icons.Music2, rect(titleX+20, y+19, 20, 20), color(0xb6bac1, 1))
			m.trackCover(c, t, rect(titleX+10, y+9, 40, 40))
			titleX += 62
		}
		nameTint := m.ink()
		if current {
			nameTint = m.accent()
		}
		m.text(c, t.Title, rect(titleX, y+10, r.Width*.4-30, 22), 13, 550, nameTint)
		m.text(c, t.ArtistText(), rect(titleX, y+32, r.Width*.4-30, 18), 11, 400, m.muted())
		if len(t.Artists) > 0 && t.Artists[0].ID != "" && hit(c, artistRows.At(i), rect(titleX, y+32, r.Width*.4-30, 18), "艺人 "+t.ArtistText()) {
			m.openEntity("artist", t.Artists[0].ID, t.Artists[0].Name)
		}
		album := "—"
		if t.Album != nil {
			album = t.Album.Title
		}
		m.text(c, album, rect(r.X+104+(r.Width-236)*.6+12, y+20, (r.Width-236)*.4, 18), 11, 400, m.muted())
		if t.Album != nil && t.Album.ID != "" && hit(c, albumRows.At(i), rect(r.X+104+(r.Width-236)*.6+12, y+20, (r.Width-236)*.4, 18), "专辑 "+album) {
			m.openEntity("album", t.Album.ID, t.Album.Title)
		}
		m.text(c, durationText(t.Duration), rect(r.X+r.Width-66, y+20, 42, 18), 11, 400, m.muted())
		favorite := (!search || m.favoriteKnown[t.ID] || m.demo) && !m.favoriteOff[t.ID]
		heart := icons.Heart
		if favorite {
			heart = icons.HeartFilled
		}
		if m.iconButton(c, hearts.At(i), rect(r.X+r.Width-43, y+14, 30, 30), heart, "切换收藏 "+t.Title, favorite) {
			m.toggleFavorite(t)
		}
	}
	endY := viewport.Y + float32(len(tracks))*58 - offset + 18
	if !detail && endY < viewport.Y+viewport.Height {
		if searchResults && (m.searchMore || m.searchError || m.searchLoading) {
			label := "加载更多搜索结果"
			if m.searchError {
				label = "重试搜索"
			}
			if m.searchLoading {
				label = "正在搜索…"
			}
			if m.pill(c, moreID, rect(viewport.X+viewport.Width/2-80, endY, 160, 32), label, false) {
				if m.searchNext == 0 && !m.searchLoading {
					m.search()
				} else {
					m.moreSearch()
				}
			}
		} else if m.complete || search {
			label := "已加载全部内容"
			if !search {
				label = "已加载全部收藏"
			} else if searchResults && len(tracks) == 0 {
				label = "没有匹配的歌曲"
				if m.searchQuery == "" {
					label = "输入歌名或艺人搜索"
				}
			}
			m.text(c, label, rect(viewport.X+viewport.Width/2-38, endY+4, 150, 20), 11, 400, m.muted())
		} else {
			label := "加载更多"
			if m.loadError {
				label = "重试加载"
			}
			if m.loading {
				label = "正在读取…"
			}
			if m.pill(c, moreID, rect(viewport.X+viewport.Width/2-55, endY, 110, 32), label, false) {
				m.loadLibrary(false)
			}
		}
	}
	c.Unclip()
}

func (m *model) paintSearch(c *ui.Canvas, b ui.Rect) {
	if m.dialog == "" && !m.fontListOpen {
		c.RequestFocus(searchID)
	}
	m.searchView.Draw(c, m.fieldTheme(), rect(b.X, b.Y, b.Width-110, 38), func(f *flex.Frame) {
		if f.Field(&m.query, searchID, "搜索歌曲", "搜索歌曲或艺人").Fill().Submitted() {
			m.search()
		}
	})
	if m.pill(c, submitID, rect(b.X+b.Width-90, b.Y, 90, 36), "搜索", true) {
		m.search()
	}
	m.paintLibrary(c, rect(b.X, b.Y+58, b.Width, b.Height-58), m.filtered, true)
}

func (m *model) scrollEvent(e ui.ScrollEvent) ui.EventResult {
	if m.fontListOpen {
		m.fontScroll = max(0, min(float32(len(m.filteredFonts()))*36-(e.Viewport.Height-264), m.fontScroll+e.DeltaY))
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if m.dialog != "" {
		return ui.EventResult{Handled: true}
	}
	if m.stageOpen {
		if m.lyricsMenuOpen {
			return ui.EventResult{Handled: true}
		}
		if !m.lyricManual {
			active := max(0, lyrics.Active(m.lyricLines, m.audioState.Position+m.lyricOffset))
			m.lyricScroll = m.stageLineOffset(active)
		}
		m.lyricScroll = max(0, min(m.stageLineOffset(len(m.lyricLines)-1), m.lyricScroll+e.DeltaY))
		m.pauseLyricFollow(time.Now())
		return ui.EventResult{Handled: true, Repaint: true}
	}

	if m.queueOpen && e.X > e.Viewport.Width-378 && e.Y > 155 && e.Y < e.Viewport.Height-125 {
		m.queueScroll = max(0, min(max(0, float32(len(m.queue.Tracks))*48-(e.Viewport.Height-356)), m.queueScroll+e.DeltaY))
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if e.X < sidebarWidth(e.Viewport.Width)+20 || e.Y < 177 || e.Y > e.Viewport.Height-118 {
		return ui.EventResult{Handled: true}
	}
	if m.view == 1 && m.detail == nil && m.entity == nil {
		_, rowHeight := collectionCardSize(e.Viewport.Width - sidebarWidth(e.Viewport.Width) - 64)
		m.scroll[1] = max(0, min(max(0, float32((len(m.playlists)+2)/3)*rowHeight-(e.Viewport.Height-351)), m.scroll[1]+e.DeltaY))
		return ui.EventResult{Handled: true, Repaint: true}
	}
	tracks := m.tracks
	if m.view == 2 {
		tracks = m.filtered
	}
	if m.view == 1 {
		tracks = m.detailTracks
	}
	if m.view == 3 {
		tracks = m.localHistory
		if m.remoteHistoryOpen {
			tracks = m.remoteHistory
		}
	}
	if m.entity != nil {
		tracks = m.entity.Tracks
	}
	if m.view == 4 && m.entity == nil {
		m.settingsSelect = ""
		m.settingsScroll = max(0, min(max(0, m.settingsHeight()-(e.Viewport.Height-304.8)), m.settingsScroll+e.DeltaY))
		return ui.EventResult{Handled: true, Repaint: true}
	}
	maxOffset := max(0, float32(len(tracks))*58+154-(e.Viewport.Height-303))
	if m.view == 2 && m.entity == nil {
		maxOffset = max(0, float32(len(tracks))*58+60-(e.Viewport.Height-303-58-20))
	}
	if m.entity != nil || (m.view == 1 && m.detail != nil) {
		extra := float32(183)
		if e.Viewport.Width <= 1100 {
			extra = 145
		}
		if m.detail != nil && m.detail.Kind == "owned" {
			extra += 44
		}
		if m.entity != nil && len(m.entity.Albums) > 0 {
			extra += 44
		}
		maxOffset = max(0, float32(len(tracks))*58+64-(e.Viewport.Height-304.8-extra-20))
	}
	offset := m.trackScroll()
	*offset = max(0, min(maxOffset, *offset+e.DeltaY))
	if m.entity == nil && m.view == 0 && maxOffset-m.scroll[0] < 180 && !m.complete && !m.loading && !m.loadError {
		m.loadLibrary(false)
	}
	if m.entity == nil && m.view == 2 && maxOffset-m.scroll[2] < 180 && !m.searchError {
		m.moreSearch()
	}
	return ui.EventResult{Handled: true, Repaint: true}
}

func (m *model) trackScroll() *float32 {
	if m.entity != nil {
		return &m.entityScroll
	}
	if m.view == 1 && m.detail != nil {
		return &m.detailScroll
	}
	return &m.scroll[m.view]
}

func (m *model) pointerEvent(e ui.PointerEvent) ui.EventResult {
	if m.transportMenu != "" && e.Kind == ui.PointerDown && e.Target != repeatID && e.Target != qualityID && e.Target != volumeID && e.Target != volumeSliderID {
		if _, ok := e.Target.In(transportOptions); !ok {
			b := volumePopoverBounds(e.Viewport)
			if m.transportMenu != "volume" || e.X < b.X || e.X > b.X+b.Width || e.Y < b.Y || e.Y > b.Y+b.Height {
				m.transportMenu = ""
			}
		}
	}

	if m.stageOpen {
		if m.lyricsMenuOpen && e.Kind == ui.PointerDown && e.Target != lyricsMenuID && e.Target != importLyricsID && e.Target != offsetMinusID && e.Target != offsetPlusID {
			m.lyricsMenuOpen = false
			return ui.EventResult{Handled: true, Repaint: true}
		}
		_, row := e.Target.In(lyricRows)
		if row && e.Kind == ui.PointerDown {
			m.lyricDragY, m.lyricDragStart = e.Y, e.Y
			m.lyricDragging, m.lyricDragged = true, false
			return ui.EventResult{Handled: true}
		}
		if m.lyricDragging && e.Kind == ui.PointerMove && e.Captured {
			if math.Abs(float64(e.Y-m.lyricDragStart)) > 4 {
				m.lyricDragged = true
			}
			if m.lyricDragged {
				if !m.lyricManual {
					m.lyricScroll = m.stageLineOffset(max(0, lyrics.Active(m.lyricLines, m.audioState.Position+m.lyricOffset)))
				}
				m.lyricScroll = max(0, min(m.stageLineOffset(len(m.lyricLines)-1), m.lyricScroll+m.lyricDragY-e.Y))
				m.pauseLyricFollow(time.Now())
			}
			m.lyricDragY = e.Y
			return ui.EventResult{Handled: true, Repaint: true}
		}
		if m.lyricDragging && e.Kind == ui.PointerUp {
			m.lyricDragging = false
			if m.lyricDragged {
				m.pauseLyricFollow(time.Now())
			}
			return ui.EventResult{Handled: m.lyricDragged, Repaint: true}
		}
	}

	if e.Target == progressID && m.player != nil && m.audioState.Duration > 0 && (e.Kind == ui.PointerDown || e.Kind == ui.PointerUp || (e.Kind == ui.PointerMove && e.Captured)) {
		b := transportBounds(e.Viewport, m.stageAmount(time.Now())).progress
		fraction := max(float32(0), min(1, (e.X-b.X)/b.Width))
		m.seekPlayback(float64(fraction) * m.audioState.Duration)
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if e.Kind == ui.PointerDown && e.ClickCount == 2 {
		if index, ok := e.Target.In(rows); ok {
			tracks := m.tracks
			if m.view == 2 {
				tracks = m.filtered
			}
			if m.view == 1 {
				tracks = m.detailTracks
			}
			if m.view == 3 {
				tracks = m.localHistory
				if m.remoteHistoryOpen {
					tracks = m.remoteHistory
				}
			}
			if m.entity != nil {
				tracks = m.entity.Tracks
			}
			m.playListTrack(tracks, index)
			return ui.EventResult{Handled: true, Repaint: true}
		}
	}
	if e.Kind != ui.PointerDown && !(e.Kind == ui.PointerMove && e.Captured) {
		return ui.EventResult{}
	}
	if e.Target == volumeSliderID {
		b := volumeTrackBounds(e.Viewport)
		m.settings.Volume = float64(max(float32(0), min(1, (e.X-b.X)/b.Width)))
		m.saveSettings()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if e.Target == opacityID || e.Target == radiusID || e.Target == fontScaleID {
		b := m.settingSliderBounds(e.Viewport, e.Target)
		value := max(float32(0), min(1, (e.X-b.X)/b.Width))
		switch e.Target {
		case opacityID:
			m.settings.Appearance.PanelOpacity = .55 + float64(value)*.45
		case radiusID:
			m.settings.Appearance.CornerRadius = float64(value) * 18
		case fontScaleID:
			m.settings.Appearance.FontScale = .85 + float64(value)*.35
		}
		m.saveSettings()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	return ui.EventResult{}
}

// Keep native scalar controls' geometry available to pointer handling.
func scalar(c *ui.Canvas, id ui.ID, b ui.Rect, value, min, max float32, label string, accent ui.Color) {
	theme := ui.LightTheme()
	theme.Primary = accent
	components.BeginSlider(c, theme, id, b, value).Range(min, max).Paint(label)
}

func (m *model) paintTitlebar(c *ui.Canvas, size ui.Size) {
	leading, trailing := app.TitlebarInsets()
	c.PushLayer(10)
	defer c.PopLayer()
	c.MoveRegion(rect(leading, 0, max(0, size.Width-leading-trailing), 28))
}
