package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"tinglan/native/internal/canonical"
	"tinglan/native/internal/librarydb"
	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
	"tinglan/native/internal/visual"
)

var (
	importDialogScope     = tree.ID(shell, "import-dialog-scope")
	importDialogCancelID  = tree.ID(shell, "import-dialog-cancel")
	importDialogSourceIDs = tree.Sub(shell, tree.Name("import-source"))
	importDialogActionIDs = tree.Sub(shell, tree.Name("import-action"))
	newPlaylistFieldID    = tree.ID(shell, "new-playlist-field")
	newPlaylistCancelID   = tree.ID(shell, "new-playlist-cancel")
	newPlaylistConfirmID  = tree.ID(shell, "new-playlist-confirm")
	newPlaylistScope      = tree.ID(shell, "new-playlist-scope")
	importRemoteID        = tree.Sub(shell, tree.Name("import-remote-playlist"))
	newPlaylistID         = tree.ID(shell, "new-playlist")

	browseRemoteID         = tree.ID(shell, "browse-remote-playlists")
	remoteLoginID          = tree.ID(shell, "remote-login")
	refreshPlaylistsID     = tree.ID(shell, "refresh-playlists")
	updatePlaylistID       = tree.ID(shell, "update-playlist")
	playlistActionRows     = tree.Sub(shell, tree.Name("playlist-actions"))
	searchTabs             = tree.Sub(shell, tree.Name("search-source"))
	addToPlaylistScope     = tree.ID(shell, "add-playlist-scope")
	addToPlaylistRows      = tree.Sub(shell, tree.Name("add-playlist"))
	addToPlaylistCancelID  = tree.ID(shell, "add-playlist-cancel")
	webViewGuideScope      = tree.ID(shell, "webview-guide-scope")
	webViewGuideCancelID   = tree.ID(shell, "webview-guide-cancel")
	webViewGuideDownloadID = tree.ID(shell, "webview-guide-download")
	accountSwitchScope     = tree.ID(shell, "account-switch-scope")
	accountSwitchCancelID  = tree.ID(shell, "account-switch-cancel")
	accountSwitchConfirmID = tree.ID(shell, "account-switch-confirm")
)

// libraryUIState holds the v3 playlist, import-dialog and toast state. It is
// deliberately separate from the legacy Huawei collection browser so both can
// coexist while the remote browser is still reachable.
type libraryUIState struct {
	toast           string
	toastUntil      time.Time
	toastTimer      *time.Timer
	searchSource    music.SourceID
	localPlaylists  []librarydb.Playlist
	localDetail     *librarydb.Playlist
	localTracks     []music.Track
	browseRemote    bool
	importSource    music.SourceID
	importKind      string // "favorites" (default) or "playlist"
	importStates    map[string]librarydb.ImportRecord
	newPlaylist     flex.Field
	newPlaylistView flex.View

	addTrack              music.Track
	addPlaylists          []librarydb.Playlist
	pendingPlaylistUpdate string
}

func (m *model) notify(text string) {
	if text == "" {
		return
	}
	// Do not restart the countdown for a repeated message: a failing loop must
	// not pin the toast on screen forever.
	if m.toast == text && m.toastVisible() {
		return
	}
	m.toast, m.toastUntil = text, time.Now().Add(3500*time.Millisecond)
	if m.toastTimer != nil {
		m.toastTimer.Stop()
	}
	m.toastTimer = time.AfterFunc(3600*time.Millisecond, func() { m.requestUpdate() })
	m.requestUpdate()
}

func (m *model) toastVisible() bool {
	return m.toast != "" && time.Now().Before(m.toastUntil)
}

// refreshPlaylists reloads the playlist list from SQLite. Local playlists come
// first, imported copies afterwards.
func (m *model) refreshPlaylists() {
	if m.local == nil {
		return
	}
	list, err := m.local.Playlists(context.Background())
	if err != nil {
		m.notify("歌单读取失败")
		return
	}
	m.localPlaylists = list
}

func (m *model) openPlaylist(p librarydb.Playlist) {
	value := p
	if m.local == nil {
		if !m.demo {
			return
		}
		m.localDetail = &value
		m.localTracks = m.tracks[:min(2, len(m.tracks))]
		return
	}
	m.localDetail = &value
	m.localTracks = nil
	m.detail = nil
	ids, err := m.local.PlaylistTracks(context.Background(), p.ID)
	if err != nil {
		m.notify("歌单读取失败")
		return
	}
	rows, err := m.local.Tracks(context.Background(), ids)
	if err != nil {
		m.notify("歌单曲目读取失败")
		return
	}
	tracks := make([]music.Track, 0, len(rows))
	for _, row := range rows {
		track := m.projectRow(row)
		if !p.Local() {
			track.OnlySource = music.SourceID(p.Source)
		}
		tracks = append(tracks, track)
	}
	m.localTracks = tracks
	// PlaylistTracks repairs dangling members and rewrites track_count; refresh
	// the cached card list so the card and the detail page agree immediately.
	m.refreshPlaylists()
	if updated, ok := m.playlistByID(p.ID); ok {
		m.localDetail = &updated
	}
}

// playlistByID returns the refreshed cached playlist row.
func (m *model) playlistByID(id string) (librarydb.Playlist, bool) {
	for _, pl := range m.localPlaylists {
		if pl.ID == id {
			return pl, true
		}
	}
	return librarydb.Playlist{}, false
}

// createPlaylist makes an empty local playlist and opens it.
func (m *model) createPlaylist() {
	name := strings.TrimSpace(m.newPlaylist.Text())
	if name == "" {
		m.notify("歌单名称不能为空")
		return
	}
	if len([]rune(name)) > 60 {
		m.notify("歌单名称最多 60 个字")
		return
	}
	if m.local == nil {
		return
	}
	playlist, err := m.local.CreatePlaylist(context.Background(), name, "")
	if err != nil {
		m.notify("新建歌单失败")
		return
	}
	m.dialog = ""
	m.newPlaylist.SetText("")
	m.refreshPlaylists()
	if m.addTrack.ID != "" {
		// The user came from "加入歌单" with no local playlist; finish that
		// action instead of dropping them into an empty playlist.
		m.addToPlaylist(playlist.ID)
		return
	}
	m.openPlaylist(playlist)
	m.notify("已新建歌单 " + playlist.Name)
}

// addToPlaylist collects one search or library row into the canonical library
// first, then links it into a local playlist.
func (m *model) addToPlaylist(playlistID string) {
	track := m.addTrack
	if track.ID == "" || m.local == nil {
		return
	}
	row, ok := m.canonicalRow(track.ID)
	if !ok {
		judge, policy := m.matchJudge()
		added, err := m.local.Add(context.Background(), track, judge, policy)
		if err != nil {
			m.notify("加入收录失败")
			return
		}
		_ = added
		if refreshed, found := m.local.RowForLink(music.SourceHuawei, track.ID); found {
			row, ok = refreshed, true
		}
	}
	if !ok {
		m.notify("这首歌暂时无法收录")
		return
	}
	if err := m.local.AddToPlaylist(context.Background(), playlistID, row.ID); err != nil {
		m.notify("加入歌单失败")
		return
	}
	m.dialog = ""
	m.installLocalLibrary()
	m.refreshPlaylists()
	if m.localDetail != nil && m.localDetail.ID == playlistID {
		m.openPlaylist(*m.localDetail)
	}
	m.notify("已加入歌单")
}

func (m *model) openAddToPlaylist(track music.Track) {
	if m.local == nil {
		return
	}
	m.refreshPlaylists()
	local := make([]librarydb.Playlist, 0, len(m.localPlaylists))
	for _, p := range m.localPlaylists {
		if p.Local() {
			local = append(local, p)
		}
	}
	m.addTrack = track
	m.addPlaylists = local
	m.dialog = "add-playlist"
}

// importSourceState reports the import status shown in the dialog.
func (m *model) importSourceState(id music.SourceID) (librarydb.ImportRecord, bool) {
	if m.local == nil {
		return librarydb.ImportRecord{}, false
	}
	record, ok, err := m.local.ImportState(id, "favorites", "")
	if err != nil {
		return librarydb.ImportRecord{}, false
	}
	return record, ok
}

func (m *model) importActionLabel(record librarydb.ImportRecord, ok bool, loggedIn bool) string {
	if !loggedIn {
		return "去登录"
	}
	if !ok {
		return "导入"
	}
	switch record.Status {
	case librarydb.ImportImported, librarydb.ImportUpdated:
		return "更新"
	case librarydb.ImportFailed, librarydb.ImportExpired:
		return "重试"
	}
	return "导入"
}

// importOne imports one source's favorites after the account-overwrite guard.
func (m *model) importOne(id music.SourceID) {
	if m.importing || m.local == nil || m.registry == nil {
		return
	}
	src, ok := m.registry.Source(id)
	if !ok {
		m.notify("音源未注册")
		return
	}
	if state, err := src.Auth().Status(context.Background()); err != nil || state.State != source.AuthSignedIn {
		m.notify("请先登录" + sourceLabel(id.String()))
		return
	}
	collection := source.Collection{Kind: "favorites", Name: "我的收藏"}
	if caps := src.Describe().Capabilities; len(caps.Collections) > 0 {
		collection = caps.Collections[0]
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.importCancel = cancel
	m.importing = true
	m.dialog = ""
	m.importStatus = "正在读取 " + sourceLabel(id.String()) + " 收藏…"
	judge, policy := m.matchJudge()
	go func() {
		defer cancel()
		result, err := m.local.Import(ctx, src, collection, judge, policy, func(progress canonical.Progress) {})
		m.postSource(sourceResult{kind: "import-done", result: result, err: err, message: id.String()})
	}()
}

// openImportDialog opens the shared import dialog. The mode must be reset on
// every entry: a stale "playlist" mode would turn 导入收藏 into 浏览歌单.
func (m *model) openImportDialog(kind string) {
	if kind == "" {
		kind = "favorites"
	}
	m.importKind = kind
	m.dialog = "import"
}

// paintImportDialog renders the per-source import list with the three states:
// not logged in, never imported, already imported.
func (m *model) paintImportDialog(c *ui.Canvas, size ui.Size) {
	initial := importDialogCancelID
	modalFocus(c, importDialogScope, initial, importDialogCancelID)
	defer c.PopFocusScope()
	c.PushLayer(3)
	defer c.PopLayer()
	c.Fill(rect(0, 0, size.Width, size.Height), color(0x16181c, .35))
	c.Block(rect(0, 0, size.Width, size.Height))
	sources := m.dialogSources()
	height := float32(150 + len(sources)*64)
	b := rect(size.Width/2-250, size.Height/2-height/2, 500, height)
	m.glass(c, b, 18, .92)
	c.PushLayer(4)
	defer c.PopLayer()
	title, subtitle := "从音源导入收藏", "只会新增或更新本机的收藏与曲库，不会改动平台上的收藏。"
	if m.importKind == "playlist" {
		title, subtitle = "从音源导入歌单", "复制到本机后不能编辑；更新时以平台当前的歌单为准，不会修改平台上的歌单。"
	}
	m.text(c, title, rect(b.X+24, b.Y+22, 360, 32), 20, 600, m.ink())
	if m.pill(c, importDialogCancelID, rect(b.X+b.Width-54, b.Y+18, 30, 30), "×", false) {
		m.dialog = ""
	}
	m.text(c, subtitle, rect(b.X+24, b.Y+56, 450, 20), 12, 400, m.muted())
	for i, src := range sources {
		y := b.Y + 92 + float32(i)*64
		id := src.ID()
		record, ok := m.importSourceState(id)
		status, _ := src.Auth().Status(context.Background())
		signedIn := status.State == source.AuthSignedIn
		m.text(c, sourceLabel(id.String()), rect(b.X+24, y, 220, 22), 14, 600, m.ink())
		detail := "尚未导入"
		if ok {
			switch record.Status {
			case librarydb.ImportImported:
				detail = fmt.Sprintf("已导入 %d 首", record.AddedCount)
				if m.local != nil {
					// AddedCount counts provider rows; 我的收藏 counts canonical
					// tracks after de-duplication, so show the number the
					// library page shows.
					detail = fmt.Sprintf("本地收藏共 %d 首", m.local.Len())
				}
			case librarydb.ImportUpdated:
				detail = fmt.Sprintf("已更新 · %d 首", record.AddedCount+record.UpdatedCount)
				if m.local != nil {
					detail = fmt.Sprintf("本地收藏共 %d 首", m.local.Len())
				}
			case librarydb.ImportFailed:
				detail = "上次导入失败，已保留原有收藏"
			case librarydb.ImportExpired:
				detail = "登录已失效"
			case librarydb.ImportDeleted:
				detail = "已删除，可重新导入"
			}
			if !record.LastSuccessAt.IsZero() {
				detail += " · 上次成功导入 " + record.LastSuccessAt.Local().Format("01-02 15:04")
			}
		}
		if m.importKind == "playlist" {
			imported := 0
			for _, pl := range m.localPlaylists {
				if pl.Source == id.String() {
					imported++
				}
			}
			if imported > 0 {
				detail = fmt.Sprintf("已导入 %d 个歌单", imported)
			} else {
				detail = "浏览并选择要导入的歌单"
			}
		}
		if !signedIn {
			detail = "未登录"
		}
		m.text(c, detail, rect(b.X+24, y+24, 300, 18), 11, 400, m.muted())
		label := m.importActionLabel(record, ok, signedIn)
		if m.importKind == "playlist" && label != "去登录" {
			label = "选择歌单"
		}
		if m.pill(c, importDialogActionIDs.At(i), rect(b.X+b.Width-140, y+6, 116, 34), label, label == "更新" || label == "导入" || label == "选择歌单") {
			switch label {
			case "去登录":
				m.dialog = ""
				m.view = 4
				m.settingsTab = 0
				m.startLogin()
			case "选择歌单":
				m.dialog = ""
				m.browseRemote = true
				m.detail = nil
				m.loadCollections("playlists", true)
			default:
				m.importOne(id)
			}
		}
	}
	if m.pill(c, importDialogCancelID, rect(b.X+b.Width-116, b.Y+b.Height-52, 92, 34), "关闭", false) {
		m.dialog = ""
	}
}

// searchScopeLabel names the platforms the current search will cover. The
// registry already knows which sources are enabled, so this stays correct when
// more adapters land.
func (m *model) searchScopeLabel() string {
	sources := m.dialogSources()
	if len(sources) == 0 {
		if m.demo {
			return sourceLabel(music.SourceHuawei.String())
		}
		return "未启用音源"
	}
	names := make([]string, 0, len(sources))
	for _, src := range sources {
		names = append(names, sourceLabel(src.ID().String()))
	}
	return strings.Join(names, "、")
}

// dialogSources lists enabled contract sources in registry order.
func (m *model) dialogSources() []source.Source {
	if m.registry == nil {
		return nil
	}
	out := make([]source.Source, 0, len(m.registry.SourceList()))
	for _, src := range m.registry.SourceList() {
		if src == nil || !m.sourceEnabled(src.ID()) {
			continue
		}
		out = append(out, src)
	}
	return out
}

func (m *model) paintNewPlaylistDialog(c *ui.Canvas, size ui.Size) {
	modalFocus(c, newPlaylistScope, newPlaylistFieldID, newPlaylistCancelID)
	defer c.PopFocusScope()
	c.PushLayer(3)
	defer c.PopLayer()
	c.Fill(rect(0, 0, size.Width, size.Height), color(0x16181c, .35))
	c.Block(rect(0, 0, size.Width, size.Height))
	b := rect(size.Width/2-230, size.Height/2-110, 460, 220)
	m.glass(c, b, 18, .92)
	c.PushLayer(4)
	defer c.PopLayer()
	m.text(c, "新建本地歌单", rect(b.X+24, b.Y+24, 400, 30), 20, 600, m.ink())
	submitted := false
	m.newPlaylistView.Draw(c, m.fieldTheme(), rect(b.X+24, b.Y+86, 412, 38), func(f *flex.Frame) {
		submitted = f.Field(&m.newPlaylist, newPlaylistFieldID, "歌单名称", "例如：通勤").Fill().Submitted()
	})
	if submitted {
		m.createPlaylist()
	}
	if m.pill(c, newPlaylistCancelID, rect(b.X+235, b.Y+160, 90, 34), "取消", false) {
		m.dialog = ""
	}
	if m.pill(c, newPlaylistConfirmID, rect(b.X+339, b.Y+160, 90, 34), "创建", true) {
		m.createPlaylist()
	}
}

func (m *model) paintAddToPlaylistDialog(c *ui.Canvas, size ui.Size) {
	modalFocus(c, addToPlaylistScope, addToPlaylistCancelID, addToPlaylistCancelID)
	defer c.PopFocusScope()
	c.PushLayer(3)
	defer c.PopLayer()
	c.Fill(rect(0, 0, size.Width, size.Height), color(0x16181c, .35))
	c.Block(rect(0, 0, size.Width, size.Height))
	rows := max(1, len(m.addPlaylists))
	if len(m.addPlaylists) == 0 {
		rows = 3
	}
	b := rect(size.Width/2-230, size.Height/2-(80+float32(rows)*52)/2, 460, 80+float32(rows)*52)
	m.glass(c, b, 18, .92)
	c.PushLayer(4)
	defer c.PopLayer()
	m.text(c, "加入歌单", rect(b.X+24, b.Y+22, 400, 28), 18, 600, m.ink())
	m.text(c, m.addTrack.Title, rect(b.X+24, b.Y+52, 412, 18), 12, 400, m.muted())
	if len(m.addPlaylists) == 0 {
		m.text(c, "还没有本地歌单", rect(b.X+24, b.Y+84, 380, 20), 13, 500, m.ink())
		m.text(c, "新建一个后，会把这首歌加进去。", rect(b.X+24, b.Y+108, 380, 18), 11, 400, m.muted())
		if m.pill(c, newPlaylistID, rect(b.X+24, b.Y+140, 160, 34), "新建本地歌单", true) {
			m.dialog = "new-playlist"
			m.newPlaylist.SetText("")
			return
		}
	}
	for i, playlist := range m.addPlaylists {
		y := b.Y + 80 + float32(i)*52
		if m.pill(c, addToPlaylistRows.At(i), rect(b.X+24, y, b.Width-48, 42), playlist.Name+" · "+fmt.Sprintf("%d 首", playlist.TrackCount), false) {
			m.addToPlaylist(playlist.ID)
			return
		}
	}
	if m.pill(c, addToPlaylistCancelID, rect(b.X+b.Width-116, b.Y+b.Height-52, 92, 34), "取消", false) {
		m.dialog = ""
	}
}

func (m *model) paintToast(c *ui.Canvas, size ui.Size) {
	if !m.toastVisible() {
		return
	}
	width := min(float32(360), size.Width-64)
	// Sit above the transport bar: the old position covered the very controls
	// (play / quality) that most toasts were asking the user to use.
	b := rect(size.Width-width-24, size.Height-148, width, 44)
	c.PushLayer(5)
	defer c.PopLayer()
	c.Shadow(b, 12, ui.ShadowStyle{OffsetY: 6, Blur: 18}, color(0x101216, .18))
	visual.Border(c, b, 12, 1, m.line(.08), m.surface(.96))
	m.centerText(c, m.toast, b, 12, 500, m.ink())
}

func (m *model) paintSearchTabs(c *ui.Canvas, b ui.Rect) {
	sources := m.dialogSources()
	if len(sources) <= 1 {
		return
	}
	for i, src := range sources {
		id := src.ID()
		active := id == m.searchSource
		x := b.X + float32(i)*104
		if m.pill(c, searchTabs.At(i), rect(x, b.Y, 96, 32), m.sourceDisplayName(id), active) {
			if m.searchSource != id {
				m.searchSource = id
				m.filtered = nil
				m.searchNext = 0
				m.searchMore = false
				if strings.TrimSpace(m.query.Text()) != "" {
					m.search()
				}
			}
		}
	}
}

// newPlaylistButton renders a secondary pill for the playlist page header.
func (m *model) newPlaylistButton(c *ui.Canvas, id ui.ID, b ui.Rect, label string) bool {
	return m.pill(c, id, b, label, false)
}

// remoteImportButton imports one remote playlist from the browser page.
func (m *model) remoteImportButton(c *ui.Canvas, id ui.ID, b ui.Rect, p music.Playlist) bool {
	// The playlist browser mirrors the favorites dialog: when the source is
	// signed out the action routes to login instead of failing on click.
	if m.session == nil && !m.demo {
		if !m.pill(c, id, b, "去登录", true) {
			return false
		}
		m.startLogin()
		return true
	}
	label := "导入"
	if m.remoteImported(p) {
		label = "更新"
	}
	if !m.pill(c, id, b, label, false) {
		return false
	}
	m.importPlaylist(p)
	return true
}

func (m *model) remoteImported(p music.Playlist) bool {
	if m.local == nil {
		return false
	}
	_, ok, err := m.local.ImportedPlaylist(context.Background(), music.SourceHuawei, p.ID)
	return err == nil && ok
}

// importPlaylist mirrors one remote playlist into the local library.
func (m *model) importPlaylist(p music.Playlist) {
	if m.demo {
		value := p
		m.detail = &value
		m.detailTracks = m.tracks[:min(2, len(m.tracks))]
		return
	}
	if m.local == nil || m.session == nil {
		m.notify("请先登录" + m.accountSourceName())
		return
	}
	playlist, err := m.local.EnsureImportedPlaylist(context.Background(), music.SourceHuawei, p.ID, p.Name, p.Description, p.CoverURL)
	if err != nil {
		m.notify("导入歌单失败")
		return
	}
	// Import must also fetch the playlist's tracks, otherwise the new local
	// copy is empty. Stay in the remote browser afterwards so the user can
	// import more playlists without walking back through the local list.
	m.refreshPlaylists()
	m.runPlaylistUpdate(playlist)
	m.notify("正在导入歌单 " + p.Name + "…")
}

// updateImportedPlaylist re-mirrors members from the current account snapshot
// after warning when the account differs from the last successful import.
func (m *model) updateImportedPlaylist(p librarydb.Playlist) {
	if m.local == nil || m.session == nil {
		m.notify("请先登录" + sourceLabel(p.Source))
		return
	}
	if account, ok, err := m.local.LastRunAccount(music.SourceID(p.Source), "playlist", p.RemoteID); err == nil && ok && m.session != nil {
		current := librarydb.AccountID(p.Source, m.session.Account())
		if account != "" && account != current {
			m.pendingPlaylistUpdate = p.ID
			m.dialog = "account-switch"
			return
		}
	}
	m.runPlaylistUpdate(p)
}

// errRemoteEmpty marks a remote collection that came back empty while the local
// copy still has members.
var errRemoteEmpty = errors.New("remote collection returned no members")

func (m *model) runPlaylistUpdate(p librarydb.Playlist) {
	src, ok := m.registry.Source(music.SourceID(p.Source))
	if !ok {
		m.notify("音源未注册")
		return
	}
	// Keep the local copy's own metadata: the sync payload only carries the
	// member list, so passing empty description/cover here would erase the
	// cover that was stored when the playlist was imported.
	remote := music.Playlist{Source: music.SourceHuawei, ID: p.RemoteID, Name: p.Name, Kind: music.PlaylistOwned, Description: p.Description, CoverURL: p.CoverURL}
	go func() {
		ctx := context.Background()
		var ids []string
		judge, policy := m.matchJudge()
		for cursor := 0; cursor < 1_000_000; {
			page, err := src.Library().Import(ctx, source.ImportRequest{Collection: source.Collection{Kind: "playlist", ID: p.RemoteID}, Cursor: fmt.Sprint(cursor), Limit: 50})
			if err != nil {
				m.postSource(sourceResult{kind: "playlist-done", err: err, message: p.ID})
				return
			}
			for _, track := range page.Tracks {
				row, found := m.local.RowForLink(track.Source, track.ID)
				if !found {
					if _, err := m.local.Add(ctx, track, judge, policy); err != nil {
						m.postSource(sourceResult{kind: "playlist-done", err: err, message: p.ID})
						return
					}
					row, found = m.local.RowForLink(track.Source, track.ID)
				}
				if found {
					ids = append(ids, row.ID)
				}
			}
			cursor += len(page.Tracks)
			if page.Done() || len(page.Tracks) == 0 {
				break
			}
		}
		// A remote playlist that suddenly reports zero members is almost always
		// an expired or wrong-account session, not a real empty list. Publishing
		// it would erase the local copy, so keep the copy and report instead.
		if len(ids) == 0 && p.TrackCount > 0 {
			m.postSource(sourceResult{kind: "playlist-done", err: errRemoteEmpty, message: p.ID})
			return
		}
		if err := m.local.ReplaceImportedMembers(ctx, p.ID, ids, remote.Name, remote.Description, remote.CoverURL); err != nil {
			m.postSource(sourceResult{kind: "playlist-done", err: err, message: p.ID})
			return
		}
		m.postSource(sourceResult{kind: "playlist-done", message: p.ID})
	}()
}

// paintAccountSwitchDialog warns that the current account differs from the one
// that produced the imported snapshot, and that updating overwrites members.
func (m *model) paintAccountSwitchDialog(c *ui.Canvas, size ui.Size) {
	modalFocus(c, accountSwitchScope, accountSwitchCancelID, accountSwitchCancelID)
	defer c.PopFocusScope()
	c.PushLayer(3)
	defer c.PopLayer()
	c.Fill(rect(0, 0, size.Width, size.Height), color(0x16181c, .35))
	c.Block(rect(0, 0, size.Width, size.Height))
	b := rect(size.Width/2-250, size.Height/2-140, 500, 280)
	m.glass(c, b, 18, .92)
	c.PushLayer(4)
	defer c.PopLayer()
	m.text(c, "账号已变化", rect(b.X+24, b.Y+24, 430, 32), 20, 600, m.ink())
	m.text(c, "这个歌单上次是用另一个账号导入的。继续更新会用当前账号的列表覆盖本地副本，旧的成员会被移除。收藏与收录音乐不受影响。",
		rect(b.X+24, b.Y+70, 452, 90), 13, 400, m.secondary())
	if m.pill(c, accountSwitchCancelID, rect(b.X+235, b.Y+200, 90, 34), "取消", false) {
		m.dialog = ""
		m.pendingPlaylistUpdate = ""
	}
	if m.pill(c, accountSwitchConfirmID, rect(b.X+339, b.Y+200, 90, 34), "继续更新", true) {
		id := m.pendingPlaylistUpdate
		m.dialog = ""
		m.pendingPlaylistUpdate = ""
		if playlist, err := m.local.Playlist(context.Background(), id); err == nil {
			m.runPlaylistUpdate(playlist)
		}
	}
}

// searchSource searches the currently selected platform tab. The selection is
// clamped to an enabled source, so disabling a plugin never leaves the page
// querying a stale source.
func (m *model) searchTracks(ctx context.Context, query string, start int) ([]music.Track, int, error) {
	catalog := m.catalogSource()
	if enabled := m.dialogSources(); len(enabled) > 0 {
		id := m.searchSource
		if id == "" || !m.sourceEnabled(id) {
			id = enabled[0].ID()
		}
		for _, src := range enabled {
			if src.ID() == id {
				catalog = src.Catalog()
				break
			}
		}
	}
	cursor := ""
	if start > 0 {
		cursor = fmt.Sprint(start)
	}
	page, err := catalog.Search(ctx, source.SearchRequest{Query: query, Cursor: cursor, Limit: 20})
	if err != nil {
		return nil, start, err
	}
	next := start + len(page.Items)
	if !page.HasMore {
		next = 0
	} else if parsed, err := strconv.Atoi(page.Next); err == nil {
		next = parsed
	}
	return page.Items, next, nil
}

// deleteLocalPlaylist hard-deletes a local playlist. Favorites and the
// canonical track rows are untouched.
func (m *model) deleteLocalPlaylist(id string) {
	if m.demo {
		for i, p := range m.localPlaylists {
			if p.ID == id {
				m.localPlaylists = append(m.localPlaylists[:i], m.localPlaylists[i+1:]...)
				break
			}
		}
		m.localDetail = nil
		return
	}
	if m.local == nil {
		return
	}
	if err := m.local.DeletePlaylist(context.Background(), id); err != nil {
		m.notify("删除歌单失败")
		return
	}
	m.localDetail = nil
	m.refreshPlaylists()
	m.notify("已删除歌单")
}

// setStatus keeps the legacy status field for tests and internal state while
// surfacing the message the way the rest of the app does: a transient toast.
func (m *model) setStatus(text string) {
	m.status = text
	if text != "" {
		m.notify(text)
	}
}

// refreshImportedMeta copies display metadata (name, description, cover) from
// a fresh remote listing onto local imported copies. It never creates a copy
// for a remote-only playlist, and an empty remote field keeps what is stored.
func (m *model) refreshImportedMeta(remote []music.Playlist) {
	if m.demo || m.local == nil {
		return
	}
	for _, p := range remote {
		if p.ID == "" || (p.CoverURL == "" && p.Description == "" && p.Name == "") {
			continue
		}
		if err := m.local.UpdateImportedMeta(context.Background(), music.SourceHuawei, p.ID, p.Name, p.Description, p.CoverURL); err != nil {
			continue
		}
	}
	m.refreshPlaylists()
}

// webView2DownloadURL is Microsoft's Evergreen bootstrapper. The runtime is
// shared by all apps, so the installer is small and one-time per machine.
const webView2DownloadURL = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

// paintWebViewGuide explains how to install the Windows WebView2 runtime.
// Login uses the embedded browser on Windows, so a missing runtime is a setup
// step, not a credentials problem.
func (m *model) paintWebViewGuide(c *ui.Canvas, size ui.Size) {
	modalFocus(c, webViewGuideScope, webViewGuideDownloadID, webViewGuideCancelID)
	defer c.PopFocusScope()
	c.PushLayer(3)
	defer c.PopLayer()
	c.Fill(rect(0, 0, size.Width, size.Height), color(0x16181c, .35))
	c.Block(rect(0, 0, size.Width, size.Height))
	b := rect(size.Width/2-250, size.Height/2-140, 500, 280)
	m.glass(c, b, 18, .92)
	c.PushLayer(4)
	defer c.PopLayer()
	m.text(c, "需要安装 WebView2", rect(b.X+24, b.Y+22, 400, 32), 20, 600, m.ink())
	m.text(c, "华为登录使用系统共享的 Microsoft Edge WebView2 Runtime。这台电脑还没有安装，安装后不用重启听澜，直接重试登录即可。", rect(b.X+24, b.Y+62, 450, 70), 13, 400, m.secondary())
	m.text(c, "下载约 1.5 MB，不会把浏览器内核打包进听澜。", rect(b.X+24, b.Y+136, 450, 20), 12, 400, m.muted())
	if m.pill(c, webViewGuideCancelID, rect(b.X+235, b.Y+216, 90, 34), "稍后", false) {
		m.dialog = ""
	}
	if m.pill(c, webViewGuideDownloadID, rect(b.X+339, b.Y+216, 130, 34), "打开下载页", true) {
		if err := openExternalURL(webView2DownloadURL); err != nil {
			m.setStatus("无法打开下载页，请手动访问 go.microsoft.com/fwlink/p/?LinkId=2124703")
		} else {
			m.setStatus("已打开下载页；安装完成后回来重试登录")
		}
		m.dialog = ""
	}
}
