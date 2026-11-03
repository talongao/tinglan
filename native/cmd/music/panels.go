package main

import (
	"fmt"
	"time"

	"github.com/vivaui/viva/ui"
	"tinglan/native/internal/icons"
	"tinglan/native/internal/music"
	"tinglan/native/internal/visual"
)

var traySettingID = tree.ID(shell, "close-to-tray")
var historySettingID = tree.ID(shell, "sync-history")
var queueRemove = tree.Sub(shell, tree.Name("remove-from-queue"))
var clearQueueID = tree.ID(shell, "clear-queue")
var customAccentID = tree.ID(shell, "custom-accent")
var refreshCollectionsID = tree.ID(shell, "refresh-collections")
var thirdPartyLyricsID = tree.ID(shell, "third-party-lyrics")
var lyricsMenuID = tree.ID(shell, "lyrics-menu")
var dockFavoriteID = tree.ID(shell, "dock-favorite")
var dockCoverID = tree.ID(shell, "dock-cover")

func (m *model) paintCollections(c *ui.Canvas, b ui.Rect) {
	if m.localDetail != nil {
		m.paintLocalPlaylistDetail(c, b)
		return
	}
	if m.detail != nil {
		if m.pill(c, backID, rect(b.X+b.Width-100, b.Y-40, 100, 30), "← 返回歌单", false) {
			m.detail = nil
			return
		}
		extraRows := 0
		if m.detail.Kind == music.PlaylistOwned {
			extraRows = 1
		}
		contentH := entityContentHeight(b.Width, b.Height, extraRows, len(m.detailTracks))
		off := max(0, min(max(0, contentH-b.Height), m.detailScroll))
		m.detailScroll = off
		c.Clip(b)
		hero := rect(b.X, b.Y-off, b.Width, b.Height)

		kind := map[string]string{"owned": "自建歌单", "favorite": "收藏歌单", "chart": "榜单"}[string(m.detail.Kind)]
		listY := m.detailHero(c, hero, kind, m.detail.Name, m.detail.CoverURL, fmt.Sprintf("%d 首", len(m.detailTracks)), m.detailTracks)
		if m.pill(c, refreshCollectionsID, rect(b.X+b.Width-110, listY-50, 90, 32), "刷新详情", false) {
			m.loadCollections("detail", true)
		}
		if m.detail.Kind == music.PlaylistOwned {
			m.text(c, m.detail.Description, rect(b.X+23, listY, b.Width-265, 32), 12, 400, m.secondary())
			if m.pill(c, editPlaylistID, rect(b.X+b.Width-240, listY, 100, 32), "修改简介", false) {
				m.dialog = "description"
				m.dialogID = m.detail.ID
				m.writeField.SetText(m.detail.Description)
			}
			if m.pill(c, deletePlaylistID, rect(b.X+b.Width-130, listY, 100, 32), "删除歌单", false) {
				m.dialog = "delete"
				m.dialogID = m.detail.ID
			}
			listY += 44
		}
		saved := m.detailScroll
		m.detailScroll = 0
		m.paintTrackList(c, rect(b.X+21, listY, b.Width-42, contentH), m.detailTracks, true, true)
		m.detailScroll = saved
		c.Unclip()
		return
	}
	if m.browseRemote {
		m.paintRemoteCollections(c, b)
		return
	}
	m.paintLocalPlaylists(c, b)
}

// paintLocalPlaylists lists local playlists plus imported copies of the current
// account. Imported entries are read-only except for 更新.
func (m *model) paintLocalPlaylists(c *ui.Canvas, b ui.Rect) {
	if m.newPlaylistButton(c, newPlaylistID, rect(b.X+b.Width-206, b.Y, 96, 32), "新建歌单") {
		m.newPlaylist.SetText("")
		m.dialog = "new-playlist"
	}
	if m.newPlaylistButton(c, browseRemoteID, rect(b.X+b.Width-104, b.Y, 104, 32), "导入歌单") {
		m.openImportDialog("playlist")
	}
	if len(m.localPlaylists) == 0 {
		m.text(c, "还没有歌单。新建一个本地歌单，或从音源导入。", rect(b.X+20, b.Y+30, b.Width-40, 28), 14, 400, m.muted())
		return
	}
	c.Clip(rect(b.X, b.Y+40, b.Width, b.Height-40))
	side, rowHeight := collectionCardSize(b.Width)
	for i, p := range m.localPlaylists {
		x := b.X + float32(i%3)*(b.Width/3)
		y := b.Y + 48 + float32(i/3)*rowHeight - m.scroll[1]
		if y+rowHeight-12 < b.Y+40 || y > b.Y+b.Height {
			continue
		}
		r := rect(x, y, side, rowHeight-12)
		c.Rounded(rect(x, y, side, side), 16, color(0xcbb8b3, .65))
		c.DrawIcon(icons.ListMusic, rect(x+r.Width/2-20, y+side/2-20, 40, 40), color(0xffffff, .9))
		m.cover(c, p.CoverURL, rect(x, y, side, side))
		m.text(c, p.Name, rect(x, y+side+12, r.Width, 24), 15, 600, m.ink())
		kind := fmt.Sprintf("本地歌单 · %d 首", p.TrackCount)
		if !p.Local() {
			kind = fmt.Sprintf("来自 %s · 只读 · %d 首", sourceLabel(p.Source), p.TrackCount)
		}
		m.text(c, kind, rect(x, y+side+38, r.Width, 20), 11, 400, m.muted())
		if hit(c, playlistRows.At(i), r, p.Name) {
			m.openPlaylist(p)
		}
	}
	c.Unclip()
}

func (m *model) paintLocalPlaylistDetail(c *ui.Canvas, b ui.Rect) {
	p := *m.localDetail
	if m.pill(c, backID, rect(b.X+b.Width-100, b.Y-40, 100, 30), "← 返回歌单", false) {
		m.localDetail = nil
		return
	}
	contentH := entityContentHeight(b.Width, b.Height, 1, len(m.localTracks))
	off := max(0, min(max(0, contentH-b.Height), m.scroll[1]))
	m.scroll[1] = off
	c.Clip(b)
	hero := rect(b.X, b.Y-off, b.Width, b.Height)
	kind := "本地歌单"
	if !p.Local() {
		kind = "来自 " + sourceLabel(p.Source) + " · 只读副本 · 更新不会写回平台"
	}
	meta := fmt.Sprintf("%d 首", len(m.localTracks))
	if !p.Local() && !p.SyncedAt.IsZero() {
		meta += " · 上次更新 " + p.SyncedAt.Local().Format("01-02 15:04")
	}
	listY := m.detailHero(c, hero, kind, "", p.CoverURL, meta, m.localTracks)
	if p.Local() {
		if m.pill(c, deletePlaylistID, rect(b.X+b.Width-130, listY, 100, 32), "删除歌单", false) {
			m.dialog = "delete-local"
			m.dialogID = p.ID
		}
	} else if m.pill(c, updatePlaylistID, rect(b.X+b.Width-130, listY, 100, 32), "更新本地副本", false) {
		m.updateImportedPlaylist(p)
	}
	listY += 44
	saved := m.scroll[1]
	m.scroll[1] = 0
	m.paintTrackList(c, rect(b.X+21, listY, b.Width-42, contentH), m.localTracks, true, true)
	m.scroll[1] = saved
	c.Unclip()
}

// paintRemoteCollections keeps the legacy Huawei browser for choosing which
// platform playlist to import.
func (m *model) paintRemoteCollections(c *ui.Canvas, b ui.Rect) {
	if m.pill(c, backID, rect(b.X+b.Width-100, b.Y, 100, 30), "返回我的歌单", false) {
		m.browseRemote = false
		m.detail = nil
		return
	}
	refreshLabel := "刷新歌单"
	if m.collectionFailed {
		refreshLabel = "重试更新"
	}
	if m.pill(c, refreshCollectionsID, rect(b.X+b.Width-206, b.Y, 96, 32), refreshLabel, false) {
		m.loadCollections("playlists", true)
	}
	if len(m.playlists) == 0 {
		switch {
		case m.collectionLoading:
			m.text(c, "正在读取歌单…", rect(b.X+20, b.Y+40, b.Width-40, 28), 14, 400, m.muted())
		case m.session == nil && !m.demo:
			m.text(c, "登录后可以浏览并导入你在华为音乐里的歌单。", rect(b.X+20, b.Y+40, b.Width-360, 28), 14, 400, m.muted())
			if m.pill(c, remoteLoginID, rect(b.X+b.Width-140, b.Y+36, 116, 34), "去登录", true) {
				m.startLogin()
			}
		case m.collectionFailed:
			message := m.collectionMessage
			if message == "" {
				message = "读取歌单失败，请点右上角“重试更新”。"
			}
			m.text(c, message, rect(b.X+20, b.Y+40, b.Width-40, 44), 13, 400, m.muted())
		default:
			m.text(c, "这个账号下还没有歌单。", rect(b.X+20, b.Y+40, b.Width-40, 28), 14, 400, m.muted())
		}
	}
	c.Clip(rect(b.X, b.Y+40, b.Width, b.Height-40))
	side, rowHeight := collectionCardSize(b.Width)
	for i, p := range m.playlists {
		x := b.X + float32(i%3)*(b.Width/3)
		y := b.Y + 48 + float32(i/3)*rowHeight - m.scroll[1]
		if y+rowHeight-12 < b.Y+40 || y > b.Y+b.Height {
			continue
		}
		r := rect(x, y, side, rowHeight-12)
		c.Rounded(rect(x, y, side, side), 16, color(0xcbb8b3, .65))
		c.DrawIcon(icons.ListMusic, rect(x+r.Width/2-20, y+side/2-20, 40, 40), color(0xffffff, .9))
		m.cover(c, p.CoverURL, rect(x, y, side, side))
		m.text(c, p.Name, rect(x, y+side+12, r.Width, 24), 15, 600, m.ink())
		kind := map[string]string{"owned": "账号自建", "favorite": "账号收藏", "chart": "平台榜单"}[string(p.Kind)]
		m.text(c, kind, rect(x, y+side+38, r.Width, 20), 11, 400, m.muted())
		if m.remoteImportButton(c, importRemoteID.At(i), rect(x, y+side+60, r.Width, 26), p) {
			continue
		}
		if hit(c, playlistRows.At(i), r, p.Name) {
			// Clicking a card must not start a network import on its own; the
			// 导入 button below each card is the explicit action. Ask first.
			m.remotePending, m.remotePendingOK = p, true
			m.dialog = "import-remote"
		}
	}
	c.Unclip()
}

func (m *model) paintHistory(c *ui.Canvas, b ui.Rect) {
	// History is local-only: the provider's listening history is not surfaced
	// because writing it back is optional and unverified on a real account.
	m.remoteHistoryOpen = false
	tracks := m.localHistory
	if len(tracks) == 0 {
		hint := "还没有播放记录。播放一首歌后会出现在这里。"
		if m.collectionLoading {
			hint = "正在读取播放历史…"
		}
		if m.collectionMessage != "" {
			hint = m.collectionMessage
		}
		m.text(c, hint, rect(b.X+21, b.Y+24, b.Width-42, 22), 12, 400, m.muted())
		return
	}
	m.text(c, fmt.Sprintf("%d 条记录", len(tracks)), rect(b.X+21, b.Y+6, 240, 21), 12, 500, m.muted())
	m.paintLibrary(c, rect(b.X, b.Y+34, b.Width, b.Height-34), tracks, true)
}

func (m *model) paintDock(c *ui.Canvas, size ui.Size) {
	m.paintDockSurface(c, size, m.stageAmount(time.Now()))
	m.paintTransport(c, size, m.stageAmount(time.Now()))
}

func (m *model) dockButton(c *ui.Canvas, id ui.ID, b ui.Rect, icon ui.Icon, label string, active, enabled bool) bool {
	if enabled {
		return m.iconButton(c, id, b, icon, label, active)
	}
	c.DrawIcon(icons.RoundedStroke(icon), rect(b.X+7, b.Y+7, 20, 20), color(m.palette().muted, .4))
	c.HitWithOptions(id, b, label, ui.HitOptions{Disabled: true})
	return false
}

func (m *model) paintDrawer(c *ui.Canvas, size ui.Size) {
	c.PushLayer(1)
	defer c.PopLayer()
	b := rect(size.Width-378, 155, 338, size.Height-280)
	c.Block(b)
	m.glass(c, b, 16, .88)
	c.PushLayer(2)
	defer c.PopLayer()
	title := "播放队列"
	m.text(c, title, rect(b.X+20, b.Y+18, 120, 30), 18, 650, m.ink())
	if n := len(m.queue.Tracks); n > 0 {
		m.text(c, fmt.Sprintf("%d 首", n), rect(b.X+20, b.Y+50, 80, 16), 11, 400, m.muted())
	}
	if m.iconButton(c, closePanelID, rect(b.X+b.Width-46, b.Y+16, 30, 30), icons.X, "关闭面板", false) {
		m.queueOpen = false
		m.lyricsMenuOpen = false
	}
	if m.queueOpen && m.pill(c, clearQueueID, rect(b.X+165, b.Y+18, 90, 30), "清空队列", false) {
		m.dialog = "clear-queue"
	}
	c.Clip(rect(b.X+12, b.Y+68, b.Width-24, b.Height-80))
	if m.queueOpen {
		for i, t := range m.queue.Tracks {
			y := b.Y + 70 + float32(i)*48 - m.queueScroll
			if y+48 < b.Y+64 {
				continue
			}
			if y > b.Y+b.Height {
				break
			}
			r := rect(b.X+12, y, b.Width-24, 46)
			if i == m.queue.Index {
				tint := m.accent()
				tint.Alpha = .09
				c.Rounded(r, 8, tint)
				c.Rounded(rect(r.X+2, y+16, 3, 14), 2, m.accent())
			}
			m.text(c, t.Title, rect(r.X+10, y+5, r.Width-60, 22), 13, 500, m.ink())
			m.text(c, t.ArtistText(), rect(r.X+10, y+27, r.Width-60, 18), 11, 400, m.muted())
			if hit(c, queueRows.At(i), r, "队列曲目 "+t.Title) {
				m.queue.Select(i)
				m.playCurrent()
				m.saveQueue()
			}
			if m.iconButton(c, queueRemove.At(i), rect(r.X+r.Width-36, y+8, 30, 30), icons.X, "移除 "+t.Title, false) {
				current := m.queue.Remove(i)
				if current {
					m.cancelPlayback()
					if m.queue.Current() != nil {
						m.playCurrent()
					}
				}
				m.saveQueue()
				break
			}
		}
	}
	c.Unclip()
}

func collectionCardSize(width float32) (float32, float32) {
	side := min(float32(240), width/3-18)
	return side, side + 78
}

func (m *model) paintDockSurface(c *ui.Canvas, size ui.Size, t float32) {
	b := rect(24, size.Height-114, size.Width-48, 98)
	if t < 1 {
		fade := 1 - t
		shadow := float32(.14)
		if m.palette().dark {
			shadow = .30
		}
		c.Shadow(b, 24, ui.ShadowStyle{OffsetY: 6, Blur: 24}, color(0, shadow*fade))
		c.Shadow(b, 24, ui.ShadowStyle{OffsetY: 1, Blur: 4}, color(0, .09*fade))
		visual.Border(c, b, 24, 1, m.line(.09*fade), m.surface(.98*fade))
	}
}
