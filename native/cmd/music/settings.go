package main

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"tinglan/native/internal/appmeta"
	"tinglan/native/internal/icons"
	"tinglan/native/internal/jev"
	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
	"tinglan/native/internal/storage"
	"tinglan/native/internal/typography"
	"tinglan/native/internal/visual"
)

var settingOptions = tree.Sub(shell, tree.Name("setting-options"))
var clearLocalLyricsID = tree.ID(shell, "clear-local-lyrics")
var importFavoritesID = tree.ID(shell, "import-favorites")
var jevEnableID = tree.ID(shell, "jev-enable")
var jevFetchModelsID = tree.ID(shell, "jev-fetch-models")
var jevSaveID = tree.ID(shell, "jev-save")
var jevTestID = tree.ID(shell, "jev-test")
var jevAddModelID = tree.ID(shell, "jev-add-model")
var jevEndpointID = tree.ID(shell, "jev-endpoint")
var jevKeyID = tree.ID(shell, "jev-key")
var jevModelInputID = tree.ID(shell, "jev-model-input")
var jevModelChoices = tree.Sub(shell, tree.Name("jev-model-choices"))
var jevThresholdChoices = tree.Sub(shell, tree.Name("jev-threshold-choices"))
var sourceSettings = tree.Sub(shell, tree.Name("source-settings"))
var sourceLoginID = tree.Sub(shell, tree.Name("source-login"))
var sourceLogoutID = tree.Sub(shell, tree.Name("source-logout"))
var pluginSettings = tree.Sub(shell, tree.Name("plugin-settings"))
var openPluginsID = tree.ID(shell, "open-plugins")
var importPluginID = tree.ID(shell, "import-plugin")
var importPluginFolderID = tree.ID(shell, "import-plugin-folder")

func (m *model) settingsHeight() float32 {
	if m.settingsTab == 0 {
		return 500
	}
	if m.settingsTab == 1 {
		return 640
	}
	if m.settingsTab == 3 {
		return m.sourcesSettingsHeight()
	}
	if m.settingsTab == 4 {
		return 720
	}
	return 300
}

func (m *model) settingSliderBounds(size ui.Size, id ui.ID) ui.Rect {
	pad := float32(30)
	if size.Width <= 1100 {
		pad = 22
	}
	x := sidebarWidth(size.Width) + 20 + pad + 1 + 209
	y := float32(178.8+20+16) - m.settingsScroll

	return rect(x+148, y, max(60, size.Width-10-pad-9-23-x-148), 24)
}

func (m *model) settingToggle(c *ui.Canvas, id ui.ID, b ui.Rect, label string, value *bool) {
	m.text(c, label, rect(b.X+2, b.Y+15, b.Width-60, 24), 12, 400, m.ink())
	r := rect(b.X+b.Width-40, b.Y+18, 36, 21)
	tint := color(0x16181c, .16)
	if *value {
		tint = m.accent()
	}
	c.Rounded(r, 10.5, tint)
	dx := float32(3)
	if *value {
		dx += 15
	}
	c.Shadow(rect(r.X+dx, r.Y+3, 15, 15), 7.5, ui.ShadowStyle{OffsetY: 1, Blur: 3}, color(0, .24))
	c.Rounded(rect(r.X+dx, r.Y+3, 15, 15), 7.5, color(0xffffff, 1))
	if hit(c, id, b, label) {
		*value = !*value
		m.saveSettings()
	}
}

func (m *model) settingChoice(c *ui.Canvas, id ui.ID, b ui.Rect, key, label string, options []string, selected int, change func(int)) {
	m.text(c, label, rect(b.X+2, b.Y+17, b.Width-210, 24), 12, 400, m.ink())
	w := float32(132)
	if key == "mode" {
		w = 188
	}
	r := rect(b.X+b.Width-w-2, b.Y+13, w, 33)
	visual.Border(c, r, 8, 1, m.line(.085), m.surface(1))
	m.text(c, options[selected], rect(r.X+12, r.Y+5, w-38, 23), 12, 400, m.ink())
	m.text(c, "⌄", rect(r.X+w-24, r.Y+4, 15, 23), 15, 500, m.secondary())
	if hit(c, id, r, label) {
		if m.settingsSelect == key {
			m.settingsSelect = ""
		} else {
			m.settingsSelect = key
		}
	}
	if m.settingsSelect != key {
		return
	}
	c.PushLayer(2)
	defer c.PopLayer()
	popup := rect(r.X, r.Y+r.Height+3, w, float32(len(options))*34+8)
	c.Block(popup)
	m.panel(c, popup, 8, 1)
	for i, option := range options {
		item := rect(popup.X+4, popup.Y+4+float32(i)*34, w-8, 34)
		if i == selected {
			tint := m.accent()
			tint.Alpha = .08
			c.Rounded(item, 5, tint)
		}
		m.text(c, option, rect(item.X+8, item.Y+5, item.Width-16, 24), 12, 400, m.ink())
		if hit(c, settingOptions.At(i), item, option) {
			change(i)
			m.settingsSelect = ""
			m.saveSettings()
		}
	}
}

func (m *model) paintSettings(c *ui.Canvas, b ui.Rect) {
	c.Clip(b)
	defer c.Unclip()
	m.paintSettingsScrollbar(c, b)

	for i, label := range []string{"播放", "外观", "关于", "音源", "高级"} {
		r := rect(b.X+23, b.Y+21+float32(i)*38, 145, 36)
		weight := float32(500)
		if m.settingsTab == i {
			c.Shadow(r, 11, ui.ShadowStyle{OffsetY: 4, Blur: 12}, color(0x101216, .07))
			c.Rounded(r, 11, m.surface(1))
			weight = 650
		}
		m.text(c, label, rect(r.X+12, r.Y+7, 120, 22), 13, weight, m.secondary())
		if hit(c, tabs.At(i), r, label+"设置") {
			m.settingsTab = i
			m.settingsScroll = 0
			m.settingsSelect = ""
		}
	}
	x, y, w := b.X+207, b.Y+20-m.settingsScroll, b.Width-230
	switch m.settingsTab {
	case 0:
		closeChoice := 0
		if m.settings.CloseToTray {
			closeChoice = 1
		}
		m.settingChoice(c, traySettingID, rect(x, y, w, 60), "close", "关闭窗口时", []string{"退出程序", "隐藏窗口"}, closeChoice, func(i int) { m.settings.CloseToTray = i == 1 })
		quality := 0
		if m.settings.Quality == "lossless" {
			quality = 1
		}
		m.settingChoice(c, settingsQualityID, rect(x, y+62, w, 60), "quality", "默认音质", []string{"标准", "无损"}, quality, func(i int) { m.settings.Quality = []string{"standard", "lossless"}[i] })
		mode := map[string]int{"off": 0, "all": 1, "one": 2}[m.settings.RepeatMode]
		if m.settings.Shuffle {
			mode = 3
		}
		m.settingChoice(c, settingsRepeatID, rect(x, y+124, w, 60), "mode", "播放模式", []string{"顺序播放", "列表循环", "单曲循环", "随机播放（打乱队列顺序）"}, mode, func(i int) {
			m.settings.Shuffle = i == 3
			m.queue.SetShuffle(i == 3)
			m.settings.RepeatMode = []string{"off", "all", "one", "off"}[i]
		})
		m.settingToggle(c, thirdPartyLyricsID, rect(x, y+186, w, 54), "第三方歌词匹配", &m.settings.ThirdPartyLyrics)
		m.settingHint(c, rect(x, y+242, w, 60), "音源没有返回歌词时，按歌名、艺人、时长匹配酷狗音乐和 LRCLIB。", "只请求歌词文本，不涉及音源，不上传账号信息。")
		m.text(c, "歌词缓存", rect(x+2, y+314, w-4, 22), 12, 400, m.ink())
		stats := m.lyricStats
		m.text(c, fmt.Sprintf("网络 %d 条 · 本地 %d 条 · %.1f KB", stats.Network, stats.Local, float64(stats.Bytes)/1024), rect(x+2, y+342, w-4, 22), 11, 400, m.muted())
		if m.pill(c, clearLyricsID, rect(x, y+376, 120, 32), "清除网络缓存", false) && m.store != nil {
			if err := m.store.ClearLyrics(false); err != nil {
				m.setStatus("缓存清理失败")
			} else {
				m.setStatus("网络歌词缓存已清除")
			}
			m.lyricStats = m.store.LyricsInfo()
		}
		if m.pill(c, clearLocalLyricsID, rect(x+132, y+376, 120, 32), "清除本地导入", false) {
			m.dialog = "clear-local-lyrics"
		}
		if m.pill(c, importLyricsID, rect(x, y+426, 120, 32), "导入本地歌词", false) {
			m.importLyrics()
		}
		if m.pill(c, removeLyricsID, rect(x+132, y+426, 120, 32), "移除本曲导入", false) {
			m.removeLyrics()
		}
	case 1:
		for i, item := range []struct {
			id               ui.ID
			label            string
			value, low, high float32
		}{{fontScaleID, "字体大小", float32(m.settings.Appearance.FontScale), .85, 1.2}} {
			ry := y + 16 + float32(i)*53
			m.text(c, item.label, rect(x+2, ry, 128, 24), 12, 400, m.ink())
			value := fmt.Sprintf("%.0f%%", item.value*100)
			if item.id == radiusID {
				value = fmt.Sprintf("%.0fpx", item.value)
			}
			m.text(c, value, rect(x+102, ry, 36, 24), 11, 400, m.muted())
			slider := rect(x+150, ry, w-150, 24)
			c.Rounded(rect(slider.X, ry+11, slider.Width, 3), 1.5, m.line(.14))
			c.Rounded(rect(slider.X, ry+11, slider.Width*(item.value-item.low)/(item.high-item.low), 3), 1.5, m.accent())
			c.HitWithOptions(item.id, slider, item.label, ui.HitOptions{Role: ui.RoleSlider})
			if c.Hovered() == item.id || c.FocusVisible() == item.id {
				c.Rounded(rect(slider.X+slider.Width*(item.value-item.low)/(item.high-item.low)-4, ry+8, 8, 8), 4, m.accent())
			}
		}
		m.text(c, "当前字体", rect(x+2, y+86, 100, 24), 12, 400, m.ink())
		family := m.settings.Appearance.FontFamily
		if family == "" {
			family = typography.DefaultFamily()
		}
		m.text(c, family, rect(x+112, y+86, w-248, 24), 12, 400, m.secondary())
		if m.pill(c, fontChoiceID, rect(x+w-126, y+82, 126, 32), "选择系统字体", false) {
			m.fontFamilies = typography.Families()
			m.fontFilter.SetText("")
			m.fontFilterText = ""
			m.fontScroll = 0
			m.fontIndex = 0
			m.fontListOpen = true
		}

		m.text(c, "主题", rect(x, y+146, w, 24), 12, 500, m.ink())
		for i, name := range themeNames {
			r := rect(x+float32(i%3)*(w/3), y+182+float32(i/3)*44, w/3-8, 34)
			selected := m.settings.Appearance.Theme == name || (name == "light" && m.settings.Appearance.Theme == "")
			if m.themeTile(c, themes.At(i), r, name, themeLabels[i], selected) {
				m.settings.Appearance.Theme = name
				size := m.backgroundSize
				m.backgroundSize = ui.Size{}
				m.prepareBackground(size)
				m.saveSettings()
			}
		}
		if m.pill(c, resetID, rect(x, y+376, 104, 34), "恢复默认外观", false) {
			m.settings.Appearance = storage.Defaults().Appearance
			size := m.backgroundSize
			m.backgroundSize = ui.Size{}
			m.prepareBackground(size)
			m.saveSettings()
		}

	case 2:
		m.text(c, appDisplayName, rect(x+2, y+16, w-4, 30), 15, 600, m.ink())
		m.text(c, "版本 "+appmeta.Version, rect(x+2, y+56, w-4, 24), 12, 400, m.muted())
		// A commit timestamp is not a packaging timestamp: reporting one as the
		// other made a freshly built app look two days old.
		revision := "代码修订 " + appmeta.Build()
		if at := appmeta.CommitAt(); at != "" {
			revision += " · " + at
		}
		packaged := appmeta.PackagedAt()
		if packaged == "" {
			packaged = "未知"
		}
		m.text(c, "打包时间 "+packaged, rect(x+2, y+82, w-4, 22), 12, 400, m.muted())
		m.text(c, revision, rect(x+2, y+106, w-4, 22), 11, 400, m.muted())
		m.text(c, "非官方 · AGPL-3.0 · Viva 原生", rect(x+2, y+134, w-4, 22), 12, 400, m.muted())
		m.text(c, "OAuth 身份不属于本项目。", rect(x+2, y+158, w-4, 22), 12, 400, m.muted())
		m.text(c, appmeta.BundleID, rect(x+2, y+190, w-4, 20), 10, 400, m.muted())
	case 3:
		m.paintSourcesSettings(c, x, y, w)
	case 4:
		m.paintAdvancedSettings(c, x, y, w)
	}
}

// themeTile draws one appearance choice with a real swatch so users can see
// the background, text and accent colours instead of guessing from the name.
func (m *model) themeTile(c *ui.Canvas, id ui.ID, r ui.Rect, name, label string, selected bool) bool {
	p := paletteFor(name)
	fill := color(p.base, 1)
	hovered := c.Hovered() == id
	if hovered {
		fill = color(p.surface, 1)
	}
	c.Rounded(r, 10, fill)
	visual.Border(c, r, 10, 1, color(p.text, .12), ui.Color{})
	if selected {
		visual.Border(c, r, 10, 2, color(p.accent, 1), ui.Color{})
	}
	c.Rounded(rect(r.X+10, r.Y+10, 14, 14), 7, color(p.accent, 1))
	m.text(c, label, rect(r.X+32, r.Y, r.Width-40, r.Height), 11, 500, color(p.text, .92))
	if selected {
		c.DrawIcon(icons.Check, rect(r.X+r.Width-24, r.Y+10, 13, 13), color(p.accent, 1))
	}
	return hit(c, id, r, label)
}

func (m *model) settingHint(c *ui.Canvas, b ui.Rect, first, second string) {
	m.text(c, first, rect(b.X+2, b.Y+12, b.Width-4, 18), 11, 400, m.muted())
	m.text(c, second, rect(b.X+2, b.Y+30, b.Width-4, 18), 11, 400, m.muted())
}

func (m *model) settingsInput(c *ui.Canvas, id ui.ID, b ui.Rect, label, placeholder string, field *flex.Field, submitted func()) {
	m.fieldView.Draw(c, m.fieldTheme(), b, func(f *flex.Frame) {
		if f.Field(field, id, label, placeholder).Submitted() && submitted != nil {
			submitted()
		}
	})
}

func (m *model) builtinSources() []source.Source {
	if m.registry == nil {
		return nil
	}
	out := make([]source.Source, 0, 2)
	for _, src := range m.registry.SourceList() {
		if src == nil || m.isPluginSource(src.ID()) {
			continue
		}
		out = append(out, src)
	}
	return out
}

// sourceRows lists every registered source in registry order together with the
// state the sources settings page needs.
type sourceRow struct {
	id        music.SourceID
	name      string
	version   string
	enabled   bool
	loginable bool
}

func (m *model) sourceRows() []sourceRow {
	if m.registry == nil {
		return nil
	}
	rows := make([]sourceRow, 0, len(m.registry.SourceList())+len(m.pluginEntries))
	seen := make(map[music.SourceID]bool)
	for _, src := range m.registry.SourceList() {
		if src == nil {
			continue
		}
		id := src.ID()
		seen[id] = true
		rows = append(rows, sourceRow{
			id:        id,
			name:      m.sourceDisplayName(id),
			enabled:   m.sourceEnabled(id),
			loginable: m.sourceLoginable(id),
		})
	}
	// Disabled plugins are not registered, but they still need a row so they
	// can be enabled again from this page.
	for _, entry := range m.pluginEntries {
		id := music.SourceID(string(entry.ID()))
		if seen[id] {
			continue
		}
		rows = append(rows, sourceRow{
			id:        id,
			name:      entry.Name(),
			version:   entry.Manifest.Version,
			enabled:   false,
			loginable: len(entry.LoginKinds()) > 0,
		})
	}
	return rows
}

// sourceRowHeight keeps the list layout and scroll extent in one place.
func sourceRowHeight(row sourceRow) float32 { return 56 }

func (m *model) sourcesSettingsHeight() float32 {
	height := float32(96)
	for _, row := range m.sourceRows() {
		height += sourceRowHeight(row)
	}
	height += 54
	if height < 360 {
		height = 360
	}
	return height
}

// sourceShortName is the one-character tile label shown on a source row.
func sourceShortName(name string) string {
	name = strings.TrimSpace(name)
	for _, r := range name {
		return string(r)
	}
	return "?"
}

func (m *model) paintSourcesSettings(c *ui.Canvas, x, y, w float32) {
	count := 0
	if m.local != nil {
		count = m.local.Len()
	}
	m.text(c, "本地收藏", rect(x+2, y+4, w-4, 24), 13, 600, m.ink())
	m.text(c, fmt.Sprintf("%d 首 · 来自本机规范化曲库，不随账号退出删除", count), rect(x+2, y+30, w-4, 22), 11, 400, m.muted())

	m.text(c, "音源", rect(x+2, y+66, w-160, 22), 13, 600, m.ink())

	rowY := y + 96
	for i, row := range m.sourceRows() {
		rowH := sourceRowHeight(row)
		tile := rect(x+2, rowY+14, 26, 26)
		visual.Border(c, tile, 8, 1, m.line(.1), m.surface(.9))
		m.centerText(c, sourceShortName(row.name), tile, 11, 650, m.ink())

		m.text(c, row.name, rect(x+40, rowY+17, 180, 20), 13, 600, m.ink())
		if row.version != "" {
			nameW := m.textWidth(c, row.name, 13, 600)
			m.text(c, row.version, rect(x+40+nameW+6, rowY+18, 90, 18), 11, 400, m.muted())
		}

		// Account state is one quiet line, right after the name.
		account := "账号：未登录"
		if m.busy && m.loginTarget == row.id {
			account = "账号：正在登录…"
		} else if m.sourceSignedIn(row.id) {
			account = "账号：已登录"
		} else if _, ok := m.sourceAuth[row.id]; !ok {
			account = "账号：正在读取…"
		}
		if row.loginable && row.enabled {
			m.text(c, account, rect(x+228, rowY+18, w-420, 18), 11, 400, m.muted())
		}

		// Right cluster: actions, status, switch.
		right := x + w - 16
		tr := rect(right-34, rowY+18, 34, 20)
		tint := color(0x16181c, .16)
		if row.enabled {
			tint = m.accent()
		}
		c.Rounded(tr, 10, tint)
		dx := float32(2.5)
		if row.enabled {
			dx += 14
		}
		c.Shadow(rect(tr.X+dx, tr.Y+2.5, 15, 15), 7.5, ui.ShadowStyle{OffsetY: 1, Blur: 3}, color(0, .24))
		c.Rounded(rect(tr.X+dx, tr.Y+2.5, 15, 15), 7.5, color(0xffffff, 1))
		if hit(c, sourceSettings.At(i), rect(tr.X-8, tr.Y-8, tr.Width+16, tr.Height+16), row.name+" 开关") {
			m.toggleSourceRow(row)
		}

		state := "已停用"
		if row.enabled {
			state = "已启用"
		}
		stateW := m.textWidth(c, state, 11, 400)
		stateX := tr.X - 14 - stateW
		dot := color(0x6b7280, 1)
		if row.enabled {
			dot = m.accent()
		}
		c.Rounded(rect(stateX-10, rowY+23, 5, 5), 2.5, dot)
		m.text(c, state, rect(stateX, rowY+18, stateW+2, 18), 11, 400, m.muted())

		if row.loginable && row.enabled {
			btnY := rowY + 14
			edge := stateX - 18
			if m.sourceSignedIn(row.id) {
				if m.pill(c, sourceLogoutID.At(i), rect(edge-84, btnY, 84, 28), "退出账号", false) {
					m.logoutSource(row.id)
				}
				edge -= 92
			}
			label := "登录"
			btnW := float32(72)
			if m.sourceSignedIn(row.id) {
				label = "重新登录"
				btnW = 92
			}
			if m.busy && m.loginTarget == row.id {
				label = "取消登录"
			}
			if m.pill(c, sourceLoginID.At(i), rect(edge-btnW, btnY, btnW, 28), label, false) {
				if m.busy && m.loginTarget == row.id {
					m.cancelLogin()
				} else {
					m.loginTarget = row.id
					m.startLogin()
				}
			}
		}

		visual.Border(c, rect(x, rowY+rowH-1, w, 1), 1, 1, m.line(.06), m.line(.06))
		rowY += rowH
	}

	importLabel := "从音源导入收藏"
	if m.importing {
		importLabel = "正在导入…"
	}
	if m.pill(c, importFavoritesID, rect(x, rowY+12, 190, 34), importLabel, m.importing) {
		if m.importing && m.importCancel != nil {
			m.importCancel()
			m.importing = false
			m.importStatus = "已取消导入"
		} else {
			m.openImportDialog("favorites")
		}
	}
	if m.pill(c, importPluginID, rect(x+200, rowY+12, 112, 34), "添加音源", false) {
		m.choosePluginBundle()
	}
	if m.pill(c, openPluginsID, rect(x+w-132, rowY+12, 132, 34), "打开音源文件夹", false) {
		m.openPluginFolder()
	}
	folder := rect(x+200, rowY+52, 120, 20)
	m.text(c, "从文件夹添加…", rect(folder.X, folder.Y, folder.Width, folder.Height), 11, 400, m.muted())
	if hit(c, importPluginFolderID, folder, "从文件夹添加") {
		m.choosePluginFolder()
	}
	if m.importStatus != "" {
		m.text(c, m.importStatus, rect(x+2, rowY+76, w-4, 22), 11, 400, m.muted())
	}
}

func (m *model) paintAdvancedSettings(c *ui.Canvas, x, y, w float32) {
	m.text(c, "决策模型（可选）", rect(x+2, y+4, w-4, 24), 13, 600, m.ink())
	m.text(c, "只用于收藏导入时辅助判断两首歌是否为同一录音；关闭时不会产生任何费用。", rect(x+2, y+26, w-4, 20), 11, 400, m.muted())
	m.settingToggle(c, jevEnableID, rect(x, y+40, w, 46), "启用模型辅助判断", &m.settings.Jev.Enabled)

	if !m.settings.Jev.Enabled {
		m.settingHint(c, rect(x, y+92, w, 50), "关闭时只按标题、艺人、时长与版本标记比较，不产生任何费用。", "开启后仅当两首歌非常接近但拿不准时才调用一次模型；音频与账号凭据不会发送。")
		return
	}
	m.text(c, "API 地址", rect(x+2, y+100, 100, 22), 11, 400, m.ink())
	m.settingsInput(c, jevEndpointID, rect(x+110, y+94, w-120, 34), "决策模型 API 地址", "https://api.typesafe.ai 或 http://127.0.0.1:8080/v1", &m.jevEndpointField, func() { m.saveJevConfig() })
	m.text(c, "API Key", rect(x+2, y+144, 100, 22), 11, 400, m.ink())
	m.settingsInput(c, jevKeyID, rect(x+110, y+138, w-400, 34), "决策模型 API Key", "输入后保存在本机加密文件", &m.jevKeyField, func() { m.saveJevConfig() })
	if m.pill(c, jevSaveID, rect(x+w-270, y+138, 130, 34), "保存配置", false) {
		m.saveJevConfig()
	}
	if m.pill(c, jevTestID, rect(x+w-132, y+138, 132, 34), "测试连接", m.jevTesting) {
		m.testJevConfig()
	}
	m.text(c, "模型", rect(x+2, y+188, 100, 22), 11, 400, m.ink())
	m.settingsInput(c, jevModelInputID, rect(x+110, y+182, w-380, 34), "模型 ID", "输入模型 ID 后回车添加", &m.jevModelField, func() { m.addJevModel() })
	if m.pill(c, jevAddModelID, rect(x+w-260, y+182, 110, 34), "添加模型", false) {
		m.addJevModel()
	}
	if m.pill(c, jevFetchModelsID, rect(x+w-140, y+182, 140, 34), "获取模型列表", m.jevFetching) {
		m.fetchJevModels()
	}
	message := m.jevError
	if m.jevTestStatus != "" {
		message = m.jevTestStatus
	}
	if message != "" {
		m.text(c, message, rect(x+2, y+222, w-4, 20), 11, 400, m.muted())
	}
	models := jevModelOptions(m)
	for i, model := range models {
		if i >= 6 {
			break
		}
		r := rect(x+float32(i%3)*(w/3), y+250+float32(i/3)*40, w/3-8, 34)
		active := m.settings.Jev.Model == model.Name
		label := model.Name
		switch model.Description {
		case "手动添加":
			label += " · 手动"
		case "当前未列出":
			label += " · 未列出"
		}
		if m.pill(c, jevModelChoices.At(i), r, label, active) {
			m.selectJevModel(model.Name)
		}
	}
	m.text(c, "调用阈值", rect(x+2, y+302, 100, 22), 11, 400, m.ink())
	thresholds := []float64{.70, .75, .80, .85, .90}
	for i, value := range thresholds {
		active := m.settings.Jev.Threshold == value
		label := fmt.Sprintf("%.2f", value)
		if m.pill(c, jevThresholdChoices.At(i), rect(x+110+float32(i)*76, y+294, 68, 32), label, active) {
			m.selectJevThreshold(value)
		}
	}
	second := "灰区请求失败时保持两首独立，不会永久判定不同。"
	if jev.InsecureEndpoint(m.settings.Jev.Endpoint) {
		second = "当前使用明文 HTTP：API Key 会发送到该地址，请只对本机或可信自部署使用。"
	}
	m.settingHint(c, rect(x, y+342, w, 60), "默认只把两首歌的标题、艺人、版本、专辑与时长发给决策模型。", second)
}

func jevModelOptions(m *model) []jev.Model {
	server := m.jevModels
	if m.jevCache != nil && len(server) == 0 {
		if cached, ok := m.jevCache.Get(m.jevIdentity); ok {
			server = cached
		}
	}
	return jev.Merge(server, manualModels(m.settings.Jev.Models), m.settings.Jev.Model)
}

// paintSettingsScrollbar shows how much of the settings page is off-screen.
// The page has always scrolled with the wheel, but at 900x600 the lower half
// looked unreachable because nothing indicated there was more below.
func (m *model) paintSettingsScrollbar(c *ui.Canvas, b ui.Rect) {
	content := m.settingsHeight()
	if content <= b.Height {
		return
	}
	track := rect(b.X+b.Width-7, b.Y+14, 3, b.Height-28)
	c.Rounded(track, 1.5, m.line(.06))
	thumb := max(28, track.Height*b.Height/content)
	maxScroll := max(1, content-b.Height)
	offset := m.settingsScroll / maxScroll
	if offset < 0 {
		offset = 0
	}
	if offset > 1 {
		offset = 1
	}
	bar := rect(track.X, track.Y+(track.Height-thumb)*offset, 3, thumb)
	c.Rounded(bar, 1.5, m.line(.24))
}
