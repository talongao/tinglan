package main

import (
	"fmt"
	"github.com/vivaui/viva/ui"
	"math"
	"strings"
	"time"
	"tinglan/native/internal/icons"
	"tinglan/native/internal/lyrics"
)

func stageProgressBounds(size ui.Size) ui.Rect {
	return rect(244, size.Height-57, max(60, size.Width-244-120), 20)
}

func (m *model) centerText(c *ui.Canvas, text string, b ui.Rect, size, weight float32, tint ui.Color) {
	m.centeredText = true
	defer func() { m.centeredText = false }()
	m.text(c, text, b, size, weight, tint)
}

func (m *model) paintStage(c *ui.Canvas, size ui.Size) {
	m.resumeLyricFollow(time.Now())
	c.DrawBackdrop(m.background, rect(0, 0, float32(m.background.Width), float32(m.background.Height)), rect(0, 0, size.Width, size.Height))
	track := m.queue.Current()
	t := m.stageAmount(time.Now())
	closeBounds := rect(size.Width-58, 44, 34, 34)
	c.Rounded(closeBounds, 17, m.line(.09))
	if m.iconButton(c, closePanelID, closeBounds, icons.X, "退出沉浸歌词", false) {
		m.setStage(false)
		m.lyricsMenuOpen = false
	}
	cover := stageCoverBounds(size)
	side, pad, column := cover.Width, cover.X, cover.Width
	gap := float32(70)
	bodyHeight := size.Height - 160
	origin := transportBounds(size, 0).cover
	// Only the cover expands; the transport keeps the same anchors in both views.
	cover = rect(origin.X+(cover.X-origin.X)*t, origin.Y+(cover.Y-origin.Y)*t, origin.Width+(cover.Width-origin.Width)*t, origin.Height+(cover.Height-origin.Height)*t)
	c.Shadow(cover, 17, ui.ShadowStyle{OffsetY: 28, Blur: 36}, color(0, .16))
	c.Rounded(cover, 17, m.line(.08))
	c.DrawIcon(icons.Music2, rect(cover.X+side/2-22, cover.Y+side/2-22, 44, 44), m.muted())
	if track != nil {
		c.PushLayer(1)
		m.trackCover(c, *track, cover)
		c.PopLayer()
		if hit(c, stageID, cover, "收起歌词封面") {
			m.setStage(false)
		}
	}
	if track != nil && t > .95 {
		m.text(c, track.Title, rect(pad, cover.Y+side+22, column, 33), 26, 660, color(m.palette().text, 1))
		m.text(c, track.ArtistText(), rect(pad, cover.Y+side+60, column, 20), 13, 400, color(m.palette().text, .62))
		if track.Album != nil {
			m.text(c, track.Album.Title, rect(pad, cover.Y+side+84, column, 18), 11, 400, color(m.palette().text, .4))
		}
		m.text(c, m.lyricSource, rect(pad, cover.Y+side+114, column, 16), 10, 400, color(m.palette().text, .34))
	}
	area := rect(pad+column+gap, 46, size.Width-2*pad-column-gap, bodyHeight)
	active := lyrics.Active(m.lyricLines, m.audioState.Position+m.lyricOffset)
	offset := m.stageLineOffset(max(0, active))
	if m.lyricManual {
		offset = m.lyricScroll
	}
	if len(m.lyricLines) == 0 {
		hint := "暂无歌词"
		if track != nil {
			hint = "暂无歌词 · 可在设置中开启第三方歌词匹配，或导入本地 .lrc / .ttml"
		}
		m.centerText(c, hint, rect(area.X, area.Y+area.Height/2-12, area.Width, 24), 13, 400, color(m.palette().text, .5))
	}
	c.Clip(area)
	for i, line := range m.lyricLines {
		if strings.TrimSpace(line.Text) == "" {
			continue
		}
		y := area.Y + size.Height*.32 + 8 + 24 + m.stageLineOffset(i) - offset - 24
		subtitles := line.Subtitles()
		rowHeight := float32(48 + 26*len(subtitles))
		if y+rowHeight < area.Y || y > area.Y+area.Height {
			continue
		}
		fade := min(float32(1), max(float32(0), (y-area.Y)/(area.Height*.16)), max(float32(0), (area.Y+area.Height-y)/(area.Height*.2)))
		// While the user scrolls manually they are reading the other lines, so
		// dimming them to .38 makes the very thing they asked for unreadable.
		inactive := float32(.38)
		if m.lyricManual || m.lyricDragging {
			inactive = .68
		}
		font, alpha := float32(20.16), inactive*fade
		if size.Width <= 1100 {
			font = 18
		}
		distance := float32(math.Abs(float64(m.stageLineOffset(i) - offset)))
		focus := max(float32(0), 1-distance/65)
		font += float32(math.Round(float64(8*focus))) / 2
		if i == active {
			alpha = fade
		}

		bounds := rect(area.X+10, y, area.Width-20, 32)
		tint := color(m.palette().text, alpha)
		if i == active {
			tint = m.accent()
			tint.Alpha = fade
		}
		m.centerText(c, line.Text, bounds, font, 600, tint)
		if i == active && len(line.Words) > 0 {
			prefix := ""
			for _, word := range line.Words {
				if m.audioState.Position+m.lyricOffset < word.Time {
					break
				}
				prefix += word.Text
			}
			full := m.textWidth(c, line.Text, font, ui.WeightSemibold) + 4
			passed := m.textWidth(c, prefix, font, ui.WeightSemibold)
			c.Clip(rect(bounds.X+(bounds.Width-full)/2, bounds.Y, passed, bounds.Height))
			m.centerText(c, line.Text, bounds, font, 600, color(m.palette().accent, fade))
			c.Unclip()
		}
		for n, subtitle := range subtitles {
			m.centerText(c, subtitle, rect(bounds.X, y+34+float32(n)*26, bounds.Width, 20), 13, 400, color(m.palette().text, .48*fade))
		}
		if hit(c, lyricRows.At(i), rect(bounds.X, y, bounds.Width, rowHeight), line.Text) && !m.lyricDragged && m.player != nil && line.Time >= 0 {
			m.seekPlayback(line.Time - m.lyricOffset)
		}
	}
	c.Unclip()
	m.paintDockSurface(c, size, t)
	m.paintTransport(c, size, t)
	if m.queueOpen {
		m.paintDrawer(c, size)
	}
}

func (m *model) stageLineOffset(index int) float32 {
	y := float32(0)
	for i := 0; i < index && i < len(m.lyricLines); i++ {
		if strings.TrimSpace(m.lyricLines[i].Text) == "" {
			continue
		}
		y += 48
		y += 26 * float32(len(m.lyricLines[i].Subtitles()))
	}
	return y
}

func (m *model) paintLyricsMenu(c *ui.Canvas, size ui.Size) {
	c.PushLayer(3)
	defer c.PopLayer()
	b := rect(200, size.Height-222, 228, 150)
	c.Block(b)
	c.Shadow(b, 14, ui.ShadowStyle{OffsetY: 6, Blur: 20}, color(0, .35))
	c.Rounded(b, 14, m.surface(1))
	tint := color(m.palette().text, .85)
	m.text(c, "导入歌词", rect(b.X+18, b.Y+10, 190, 32), 13, 500, tint)
	if hit(c, importLyricsID, rect(b.X+8, b.Y+6, 212, 40), "导入歌词") {
		m.lyricsMenuOpen = false
		m.importLyrics()
	}
	c.Fill(rect(b.X+16, b.Y+50, 196, 1), m.line(.08))
	m.text(c, "歌词时间", rect(b.X+18, b.Y+62, 100, 22), 12, 500, tint)
	value := fmt.Sprintf("%+.1f 秒", m.lyricOffset)
	m.centerText(c, value, rect(b.X+68, b.Y+94, 92, 32), 12, 500, tint)
	for _, item := range []struct {
		id    ui.ID
		label string
		x     float32
		delta float64
	}{{offsetMinusID, "−", b.X + 18, -.5}, {offsetPlusID, "+", b.X + 178, .5}} {
		button := rect(item.x, b.Y+94, 32, 32)
		c.Rounded(button, 8, m.line(.07))
		m.centerText(c, item.label, button, 17, 500, tint)
		if hit(c, item.id, button, map[bool]string{true: "歌词提前半秒", false: "歌词延后半秒"}[item.delta > 0]) {
			m.lyricOffset = max(-10, min(10, m.lyricOffset+item.delta))
			m.saveLyricOffset()
		}
	}
}

func (m *model) pauseLyricFollow(now time.Time) {
	m.lyricManual = true
	m.lyricResumeAt = now.Add(5 * time.Second)
	if m.lyricResumeTimer != nil {
		m.lyricResumeTimer.Stop()
	}
	m.lyricResumeTimer = time.AfterFunc(5*time.Second, m.requestUpdate)
}
func (m *model) resumeLyricFollow(now time.Time) {
	if !m.lyricDragging && m.lyricManual && !m.lyricResumeAt.IsZero() && !now.Before(m.lyricResumeAt) {
		m.lyricManual = false
	}
}
