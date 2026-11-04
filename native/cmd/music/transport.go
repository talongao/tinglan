package main

import (
	"github.com/vivaui/viva/ui"
	"time"
	"tinglan/native/internal/icons"
)

type transportLayout struct{ cover, play, previous, next, progress ui.Rect }

func stageCoverBounds(size ui.Size) ui.Rect {
	side := min(float32(300), size.Width*.28, (size.Height-270)*.75)
	return rect(52, max(82, (size.Height-150-side-150)/2), side, side)
}
func transportBounds(size ui.Size, t float32) transportLayout {
	x, y := size.Width*.5-10, size.Height-76
	return transportLayout{
		cover: rect(40, size.Height-86, 52, 52),
		play:  rect(x-22, y-22, 44, 44), previous: rect(x-74, y-17, 34, 34), next: rect(x+40, y-17, 34, 34),
		progress: rect(296, size.Height-38, max(100, size.Width-636), 18),
	}

}
func (m *model) setStage(open bool) {
	if m.stageOpen == open {
		return
	}
	m.stageFrom = m.stageAmount(time.Now())
	m.stageOpen = open
	m.stageChanged = time.Now()
	m.queueOpen = false
	m.transportMenu = ""
	m.lyricsMenuOpen = false
	m.requestUpdate()
}
func (m *model) stageAmount(now time.Time) float32 {
	target := float32(0)
	if m.stageOpen {
		target = 1
	}
	if m.stageChanged.IsZero() {
		return target
	}
	t := min(float32(1), float32(now.Sub(m.stageChanged).Seconds()/.32))
	t = t * t * (3 - 2*t)
	return m.stageFrom + (target-m.stageFrom)*t
}
func (m *model) paintTransport(c *ui.Canvas, size ui.Size, t float32) {
	b := transportBounds(size, t)
	track := m.queue.Current()
	c.Rounded(b.cover, 10, m.surface(1))
	c.DrawIcon(icons.Headphones, rect(b.cover.X+15, b.cover.Y+15, 22, 22), m.muted())
	if track != nil {
		m.trackCover(c, *track, b.cover)
	}
	label := "打开沉浸歌词"
	if m.stageOpen {
		label = "收起沉浸歌词"
	}
	if hit(c, dockCoverID, b.cover, label) {
		m.setStage(!m.stageOpen)
	}
	{
		title, artist := "尚未选择歌曲", "选择一首歌开始播放"
		if track != nil {
			title, artist = track.Title, track.ArtistText()
			if m.resolving && !m.audioRunning {
				artist = "正在连接音源…"
			} else if m.playbackError != "" && !m.audioRunning {
				artist = m.playbackError + " · 点右侧重试"
			}
		}
		m.text(c, title, rect(106, size.Height-85, 128, 24), 12, 600, m.ink())
		m.text(c, artist, rect(106, size.Height-59, 144, 22), 11, 400, m.muted())
	}
	if track != nil {
		favorite := m.favoriteKnown[track.ID] && !m.favoriteOff[track.ID]
		icon := icons.Heart
		if favorite {
			icon = icons.HeartFilled
		}
		hint := "加入本机收藏"
		if favorite {
			hint = "从本机收藏移除（不影响平台收藏）"
		}
		if m.iconButton(c, dockFavoriteID, rect(242, size.Height-85, 28, 28), icon, hint, favorite) {
			m.toggleFavorite(*track)
		}
	}
	if m.dockButton(c, previousID, b.previous, icons.PlayerPrevious, "上一首", false, track != nil) {
		m.stepPlayback(-1)
	}
	if m.dockButton(c, nextID, b.next, icons.PlayerNext, "下一首", false, track != nil) {
		m.stepPlayback(1)
	}
	tint := m.accent()
	tint.Alpha = .10
	if track != nil && (c.Hovered() == toggleID || c.FocusVisible() == toggleID) {
		tint.Alpha = .19
		c.Shadow(b.play, 22, ui.ShadowStyle{OffsetY: 2, Blur: 8}, m.line(.10))
	}
	if track != nil && c.Pressed() == toggleID {
		tint.Alpha = .27
	}
	c.Rounded(b.play, 22, tint)
	icon := icons.PlayFilled
	if m.audioRunning {
		icon = icons.PlayerPause
	}
	fg := m.ink()
	c.DrawIcon(icon, rect(b.play.X+11, b.play.Y+11, 22, 22), fg)
	if m.playbackError != "" && !m.audioRunning && track != nil {
		if m.pill(c, retryPlaybackID, rect(b.play.X+56, b.play.Y+7, 62, 30), "重试", false) {
			m.playCurrent()
		}
	}
	c.HitWithOptions(toggleID, b.play, "播放或暂停", ui.HitOptions{Disabled: track == nil})
	if track != nil && c.Clicked(toggleID) {
		m.togglePlayback()
	}
	position, duration := m.playbackTimes()
	m.text(c, durationText(&position), rect(b.progress.X-44, b.progress.Y-2, 38, 20), 10, 400, m.muted())
	c.Rounded(rect(b.progress.X, b.progress.Y+6, b.progress.Width, 5), 2.5, m.line(.15))
	if duration > 0 {
		c.Rounded(rect(b.progress.X, b.progress.Y+6, b.progress.Width*float32(min(1, position/duration)), 5), 2.5, m.accent())
	}
	c.HitWithOptions(progressID, b.progress, "播放进度", ui.HitOptions{Role: ui.RoleSlider, Disabled: m.player == nil})
	total := "--:--"
	if duration > 0 {
		total = durationText(&duration)
	}
	m.text(c, total, rect(b.progress.X+b.progress.Width+10, b.progress.Y-2, 38, 20), 10, 400, m.muted())
	if t > .95 {
		menu := rect(208, size.Height-53, 28, 24)
		c.Rounded(menu, 14, m.surface(.9))
		m.centerText(c, "···", menu, 14, 600, m.secondary())
		if hit(c, lyricsMenuID, menu, "歌词选项") {
			m.lyricsMenuOpen = !m.lyricsMenuOpen
		}
		if m.lyricsMenuOpen {
			m.paintLyricsMenu(c, size)
		}
	}
	y := size.Height - 74

	modeBounds := rect(size.Width-292, y, 36, 28)
	if m.iconButton(c, repeatID, modeBounds, m.modeIcon(), "播放顺序", false) {
		m.toggleTransportMenu("mode")
	}
	quality := "标准"
	if m.settings.Quality == "lossless" {
		quality = "无损"
	}
	if m.pill(c, qualityID, rect(size.Width-242, y, 48, 28), quality, false) {
		m.toggleTransportMenu("quality")
	}
	if m.pill(c, lyricsID, rect(size.Width-184, y, 34, 28), "词", false) {
		m.setStage(!m.stageOpen)
	}
	if m.iconButton(c, queueID, rect(size.Width-140, y, 32, 28), icons.ListMusic, "播放队列", m.queueOpen) {
		m.queueOpen = !m.queueOpen
		m.transportMenu = ""
		if m.queueOpen {
			m.centerQueue(size)
		}
	}
	volumeIcon := icons.Volume2
	if m.settings.Volume == 0 {
		volumeIcon = icons.VolumeX
	}
	if m.iconButton(c, volumeID, rect(size.Width-94, y, 32, 28), volumeIcon, "调节音量", m.transportMenu == "volume") {
		m.toggleTransportMenu("volume")
	}
	m.paintTransportMenu(c, size)
}
func (m *model) centerQueue(size ui.Size) {
	visible := max(float32(48), size.Height-356)
	m.queueScroll = max(0, min(float32(len(m.queue.Tracks))*48-visible, float32(m.queue.Index)*48-visible/2+24))
}
func (m *model) playMode() string {
	if m.settings.Shuffle {
		return "随机"
	}
	return map[string]string{"off": "顺序", "all": "列表循环", "one": "单曲循环"}[m.settings.RepeatMode]
}
func (m *model) cyclePlayMode() {
	if m.settings.Shuffle {
		m.settings.Shuffle = false
		m.settings.RepeatMode = "off"
	} else {
		switch m.settings.RepeatMode {
		case "off":
			m.settings.RepeatMode = "all"
		case "all":
			m.settings.RepeatMode = "one"
		default:
			m.settings.RepeatMode = "off"
			m.settings.Shuffle = true
		}
	}
	m.queue.SetShuffle(m.settings.Shuffle)
	m.saveSettings()
}
func (m *model) playbackTimes() (float64, float64) {
	duration := m.audioState.Duration
	if duration <= 0 {
		duration = m.resolvedDuration
	}
	if duration <= 0 {
		if track := m.queue.Current(); track != nil && track.Duration != nil {
			duration = *track.Duration
		}
	}
	position := max(0, m.queue.Position)
	return position, max(0, duration)
}
