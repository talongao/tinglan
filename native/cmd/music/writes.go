package main

import (
	"context"
	"time"

	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"tinglan/native/internal/huawei"
	"tinglan/native/internal/music"
)

var editPlaylistID = tree.ID(shell, "edit-description")
var deletePlaylistID = tree.ID(shell, "delete-playlist")
var confirmWriteID = tree.ID(shell, "confirm-write")
var cancelWriteID = tree.ID(shell, "cancel-write")
var writeFieldID = tree.ID(shell, "write-field")

type writeResult struct {
	epoch    uint64
	id, kind string
	favorite bool
	err      error
}
type writeState struct {
	writer       *huawei.Writer
	writeBusy    bool
	writeCancel  context.CancelFunc
	writeResults []writeResult
	dialog       string
	dialogID     string
	writeField   flex.Field
	writeView    flex.View
}

func (m *model) initWriter() {
	if m.session == nil {
		return
	}
	var save func(bool) error
	var load func() (bool, error)
	if m.accountStore != nil {
		save = m.accountStore.SetCreationPending
		load = m.accountStore.CreationPending
	}
	m.writer = huawei.NewWriter(huawei.NewClient(), m.session, save, load)
	if m.accountStore != nil {
		m.writer.WithCreationJournal(m.accountStore.LoadCreationIntent, m.accountStore.SaveCreationIntent)
	}
}
func (m *model) toggleFavorite(track huawei.Track) {
	if m.demo {
		m.favoriteKnown[track.ID] = true
		m.favoriteOff[track.ID] = !m.favoriteOff[track.ID]
		return
	}
	if m.local == nil {
		return
	}
	if _, ok := m.canonicalRow(track.ID); ok {
		m.removeLocalFavorite(track.ID)
		return
	}
	if _, ok := m.local.RowForLink(music.SourceHuawei, track.ID); ok {
		if err := m.local.RemoveLink(music.SourceHuawei, track.ID); err != nil {
			m.setStatus("移除本地收藏失败")
			return
		}
		m.installLocalLibrary()
		m.setStatus("已从本地收藏移除")
		return
	}
	judge, policy := m.matchJudge()
	added, err := m.local.Add(context.Background(), huawei.MusicTrack(track), judge, policy)
	if err != nil {
		m.setStatus("加入本地收藏失败")
		return
	}
	m.installLocalLibrary()
	if added {
		m.setStatus("已加入本地收藏")
	} else {
		m.setStatus("已在本地收藏中")
	}
}

func (m *model) postWrite(r writeResult) {
	m.mu.Lock()
	if !m.closed {
		m.writeResults = append(m.writeResults, r)
	}
	m.mu.Unlock()
	m.requestUpdate()
}
func (m *model) applyWrites() {
	m.mu.Lock()
	results := m.writeResults
	m.writeResults = nil
	m.mu.Unlock()
	for _, r := range results {
		if r.epoch != m.accountEpoch {
			continue
		}
		m.writeBusy = false
		m.writeCancel = nil
		if r.err != nil {
			if m.expireSession(r.err) {
				continue
			}
			m.setStatus("操作未确认，请刷新核对后重试。")
			continue
		}
		m.setStatus("操作成功，已读取服务端确认。")
		if r.kind == "favorite" {
			m.favoriteKnown[r.id] = true
			m.favoriteOff[r.id] = !r.favorite
			if m.catalog != nil {
				if m.loadCancel != nil {
					m.loadCancel()
				}
				m.epoch++
				m.loading = false
				m.refreshLibrary()
			}
		} else {
			if m.accountStore != nil {
				_ = m.accountStore.ClearCollections()
			}
			m.detail = nil
			m.loadCollections("playlists", true)
		}
	}
}
func (m *model) confirmWrite() {
	if m.writeBusy {
		return
	}
	kind, id, text := m.dialog, m.dialogID, m.writeField.Text()
	if kind == "delete-local" {
		m.dialog = ""
		m.deleteLocalPlaylist(id)
		return
	}
	if kind == "import-remote" {
		m.dialog = ""
		if m.remotePendingOK {
			p := m.remotePending
			m.remotePendingOK = false
			m.importPlaylist(p)
		}
		return
	}
	if kind == "clear-queue" {
		m.dialog = ""
		m.cancelPlayback()
		m.queue.Replace(nil, 0)
		m.audioState.Position = 0
		m.audioState.Duration = 0
		m.saveQueue()
		m.notify("队列已清空")
		return
	}
	if kind == "clear-local-lyrics" {
		if m.store != nil {
			if err := m.store.ClearLyrics(true); err != nil {
				m.setStatus("本地导入清理失败")
				return
			}
			m.lyricStats = m.store.LyricsInfo()
		}
		m.lyricLines = nil
		m.lyricSource = ""
		m.setStatus("本地导入歌词已清除")
		m.dialog = ""
		return
	}
	if kind == "description" && len([]rune(text)) > 200 {
		m.setStatus("歌单简介最多 200 个字。")
		return
	}
	m.dialog = ""
	if m.demo {
		m.setStatus("合成模式：未向真实账号发送写请求。")
		if kind == "description" && m.detail != nil {
			m.detail.Description = text
		}
		if kind == "delete" {
			for i, p := range m.playlists {
				if p.ID == id {
					m.playlists = append(m.playlists[:i], m.playlists[i+1:]...)
					break
				}
			}
			m.detail = nil
		}
		return
	}
	if m.writer == nil {
		return
	}
	writer, epoch := m.writer, m.accountEpoch
	m.writeBusy = true
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	m.writeCancel = cancel
	go func() {
		defer cancel()
		var err error
		switch kind {
		case "description":
			err = writer.UpdateDescription(ctx, id, text)
		case "delete":
			err = writer.DeletePlaylist(ctx, id)
		}
		m.postWrite(writeResult{epoch: epoch, id: id, kind: kind, err: err})
	}()
}
func (m *model) paintWriteDialog(c *ui.Canvas, size ui.Size) {
	initial := writeFieldID
	if m.dialog == "delete" || m.dialog == "delete-local" || m.dialog == "clear-local-lyrics" || m.dialog == "clear-queue" || m.dialog == "import-remote" {
		initial = cancelWriteID
	}
	modalFocus(c, writeDialogScope, initial, cancelWriteID)
	defer c.PopFocusScope()
	c.PushLayer(3)
	defer c.PopLayer()
	c.Fill(rect(0, 0, size.Width, size.Height), color(0x16181c, .35))
	c.Block(rect(0, 0, size.Width, size.Height))
	b := rect(size.Width/2-230, size.Height/2-130, 460, 260)
	m.glass(c, b, 18, .92)
	c.PushLayer(4)
	defer c.PopLayer()
	title := map[string]string{"description": "修改歌单简介", "delete": "确认删除自建歌单", "delete-local": "删除本地歌单", "clear-local-lyrics": "清除全部本地导入歌词", "clear-queue": "确认清空播放队列", "import-remote": "导入歌单"}[m.dialog]
	m.text(c, title, rect(b.X+24, b.Y+24, 410, 34), 22, 600, m.ink())
	if m.dialog == "clear-local-lyrics" {
		m.text(c, "清除应用内导入副本，原始歌词文件不受影响。", rect(b.X+24, b.Y+92, 410, 50), 13, 400, m.secondary())
	} else if m.dialog == "clear-queue" {
		m.text(c, "清空后当前播放会停止，播放历史与收藏不受影响。", rect(b.X+24, b.Y+92, 410, 50), 13, 400, m.secondary())
	} else if m.dialog == "import-remote" {
		name := m.remotePending.Title
		m.text(c, "导入「"+name+"」为本机的只读副本。不会修改平台上的歌单，之后可随时删除。", rect(b.X+24, b.Y+92, 410, 50), 13, 400, m.secondary())
	} else if m.dialog == "delete" {
		m.text(c, "删除后无法撤销。仅删除当前选中的自建歌单。", rect(b.X+24, b.Y+92, 410, 50), 13, 400, m.secondary())
	} else if m.dialog == "delete-local" {
		m.text(c, "删除后无法撤销。只删除这个歌单，收录音乐与收藏不受影响。", rect(b.X+24, b.Y+92, 410, 50), 13, 400, m.secondary())
	} else {
		submitted := false
		m.writeView.Draw(c, m.fieldTheme(), rect(b.X+24, b.Y+90, 410, 38), func(f *flex.Frame) {
			submitted = f.Field(&m.writeField, writeFieldID, title, "请输入内容").Fill().Submitted()
		})
		if submitted {
			m.confirmWrite()
		}
	}
	if m.pill(c, cancelWriteID, rect(b.X+235, b.Y+200, 90, 34), "取消", false) {
		m.dialog = ""
		m.remotePendingOK = false
	}
	confirmLabel := "确认"
	if m.dialog == "import-remote" {
		confirmLabel = "导入"
	}
	if m.pill(c, confirmWriteID, rect(b.X+339, b.Y+200, 90, 34), confirmLabel, true) {
		m.confirmWrite()
	}
}
