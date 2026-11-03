package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"tinglan/native/internal/audio"
	"tinglan/native/internal/library"
	"tinglan/native/internal/librarydb"
	"tinglan/native/internal/lyrics"
	"tinglan/native/internal/music"
	queuepkg "tinglan/native/internal/player"
	"tinglan/native/internal/source"
	"tinglan/native/internal/storage"
	"tinglan/native/internal/visual"
)

type libraryResult struct {
	epoch      uint64
	tracks     []music.Track
	complete   bool
	err        error
	search     bool
	query      string
	kind       string
	playlists  []music.Playlist
	detailID   string
	searchNext int
	cached     bool
	refreshing bool
}
type desktopState struct {
	stageChanged         time.Time
	stageFrom            float32
	stageTimer           *time.Timer
	demo                 bool
	demoCount            int
	fixtureDir           string
	view                 int
	settingsTab          int
	settings             storage.Settings
	store                *storage.Store
	accountStore         *storage.Store
	queue                queuepkg.Queue
	catalog              *library.Catalog
	tracks               []music.Track
	filtered             []music.Track
	complete             bool
	loading              bool
	loadError            bool
	loadCancel           context.CancelFunc
	epoch                uint64
	accountEpoch         uint64
	results              []libraryResult
	query                flex.Field
	searchView           flex.View
	searchQuery          string
	searchNext           int
	searchMore           bool
	searchGeneration     uint64
	searchCancel         context.CancelFunc
	searchLoading        bool
	searchError          bool
	collectionGeneration uint64
	collectionCancel     context.CancelFunc
	collectionLoading    bool
	collectionFailed     bool
	collectionMessage    string
	libraryRefreshing    bool
	scroll               [5]float32
	detailScroll         float32
	entityScroll         float32
	settingsScroll       float32
	playbackError        string
	// volumeBeforeMute remembers the level so clicking the percentage can
	// toggle mute without losing the user's setting.
	volumeBeforeMute float64

	// remotePending is the playlist the user clicked in the remote browser;
	// importing runs only after the confirmation dialog.
	remotePending     music.Playlist
	remotePendingOK   bool
	settingsSelect    string
	lyricStats        storage.LyricStats
	selected          int
	queueOpen         bool
	transportMenu     string
	queueScroll       float32
	lyricsMenuOpen    bool
	lyricResumeAt     time.Time
	lyricResumeTimer  *time.Timer
	lyricDragY        float32
	lyricDragStart    float32
	lyricDragging     bool
	lyricDragged      bool
	playTimer         *time.Timer
	background        ui.Bitmap
	backgroundGlass   ui.Bitmap
	backgroundSize    ui.Size
	favoriteOff       map[string]bool
	favoriteKnown     map[string]bool
	playlists         []music.Playlist
	detail            *music.Playlist
	detailTracks      []music.Track
	remoteHistory     []music.Track
	localHistory      []music.Track
	remoteHistoryOpen bool
	lyricLines        []lyrics.Line
	stageOpen         bool
	lyricOffset       float64
	lyricTrack        string
	lyricManual       bool
	lyricScroll       float32
}

func (m *model) initDesktop() {
	m.settings = storage.Defaults()
	m.selected = -1
	m.favoriteOff = make(map[string]bool)
	m.favoriteKnown = make(map[string]bool)
}

func (m *model) openProfile(dir string) {
	m.store = storage.New(dir)
	m.settings, _ = m.store.LoadSettings()
	if !m.demo {
		m.openLocalLibrary()
		if saved, err := m.store.LoadQueue(); err == nil && len(saved.Tracks) > 0 {
			m.queue.Mode = m.settings.RepeatMode
			m.queue.Shuffle = m.settings.Shuffle
			m.queue.Replace(saved.Tracks, saved.Index)
			m.queue.Position = saved.Position
			m.rememberResume(saved.Position)
		}
	}
	if m.demo {
		saved, err := m.store.LoadQueue()
		if err == nil {
			m.queue.Mode = m.settings.RepeatMode
			m.queue.Shuffle = m.settings.Shuffle
			m.queue.Replace(saved.Tracks, saved.Index)
			m.queue.Position = saved.Position
			m.rememberResume(saved.Position)
		}
	}
}

func syntheticTracks(count int) []music.Track {
	tracks := make([]music.Track, count)
	for i := range tracks {
		duration := float64(180 + i%180)
		tracks[i] = music.Track{ID: fmt.Sprintf("synthetic-%04d", i), Title: fmt.Sprintf("合成歌曲 %03d", i+1), Artists: []music.Artist{{Name: "离线测试艺人"}}, Album: &music.Album{ID: "synthetic-album", Title: "合成测试专辑"}, Duration: &duration}
	}
	if count == 2 {
		duration := 4.0
		tracks[0] = music.Track{ID: "synthetic-0000", Title: "Smoke MP3", Artists: []music.Artist{{ID: "synthetic-artist", Name: "Local fixture"}}, Album: &music.Album{ID: "synthetic-album", Title: "Fixture Album"}, Duration: &duration}
		tracks[1] = music.Track{ID: "synthetic-0001", Title: "Smoke FLAC", Artists: []music.Artist{{ID: "synthetic-artist", Name: "Local fixture"}}, Duration: &duration}
	}
	for i := range tracks {
		tracks[i].CoverURL = "https://fixture.dbankcdn.cn/synthetic-cover"
	}
	return tracks
}

func (m *model) enableDemo(count int) {
	m.demo = true
	m.demoCount = count
	pix := make([]byte, 256*256*4)
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			i := (y*256 + x) * 4
			pix[i] = uint8(100 + y/2)
			pix[i+1] = uint8(65 + x/2)
			pix[i+2] = uint8(210 - y/2)
			pix[i+3] = 255
		}
	}
	m.covers = map[string]ui.Bitmap{"https://fixture.dbankcdn.cn/synthetic-cover": {ID: 500, Version: 1, Width: 256, Height: 256, Stride: 1024, Pix: pix}}
	m.setStatus("离线合成测试数据 · 不是本人音乐库")
	all := syntheticTracks(count)
	m.covers["https://fixture.dbankcdn.cn/synthetic-cover"] = visual.Round(m.covers["https://fixture.dbankcdn.cn/synthetic-cover"], 22)
	m.catalog = library.New(func(ctx context.Context, start, limit int) (music.Page, error) {
		if err := ctx.Err(); err != nil {
			return music.Page{}, err
		}
		end := min(start+limit, len(all))
		if start > len(all) {
			return music.Page{}, errors.New("invalid fixture page")
		}
		return music.Page{Items: all[start:end], Next: end, More: end < len(all)}, nil
	})
	// Test data is generated locally; the first page requires no worker or I/O.
	_ = m.catalog.LoadNext(context.Background())
	m.tracks, m.complete = m.catalog.Snapshot()
	for _, track := range m.tracks {
		m.favoriteKnown[track.ID] = true
	}
	m.playlists = []music.Playlist{{Source: music.SourceHuawei, ID: "synthetic-playlist", Name: "离线测试歌单", Kind: music.PlaylistOwned, Description: "合成歌单，不会修改真实账号"}}
	m.localPlaylists = []librarydb.Playlist{{ID: "synthetic-local", Kind: "local", Name: "离线测试歌单", Description: "合成歌单，不会修改真实账号", TrackCount: min(2, len(m.tracks))}}
}

func (m *model) loadLibrary(all bool) {
	if m.catalog == nil || m.loading {
		return
	}
	m.loading = true
	m.loadError = false
	ctx, cancel := context.WithCancel(context.Background())
	m.loadCancel = cancel
	epoch, catalog := m.epoch, m.catalog
	go func() {
		defer cancel()
		var err error
		if all {
			err = catalog.LoadAll(ctx)
		} else {
			err = catalog.LoadNext(ctx)
		}
		tracks, complete := catalog.Snapshot()
		m.mu.Lock()
		if !m.closed {
			m.results = append(m.results, libraryResult{epoch: epoch, tracks: tracks, complete: complete, err: err})
		}
		m.mu.Unlock()
		m.requestUpdate()
	}()
}

func (m *model) installCatalog() {
	// Favorites are now local and account-independent. Login only re-enables
	// the Huawei adapter; the list itself comes from favorites.local.json.
	m.installLocalLibrary()
	if len(m.tracks) == 0 {
		m.setStatus("本地收藏为空，可在设置 → 音源中从华为音乐导入。")
	}
}

func (m *model) clearLibrary() {
	m.cancelPlayback()
	m.epoch++
	m.searchGeneration++
	if m.searchCancel != nil {
		m.searchCancel()
		m.searchCancel = nil
	}
	m.searchLoading = false
	m.collectionGeneration++
	if m.collectionCancel != nil {
		m.collectionCancel()
		m.collectionCancel = nil
	}
	m.collectionLoading = false
	m.collectionFailed = false
	m.libraryRefreshing = false
	m.accountEpoch++
	if m.writeCancel != nil {
		m.writeCancel()
		m.writeCancel = nil
	}
	m.writeBusy = false
	m.dialog = ""
	m.favoriteOff = make(map[string]bool)
	m.favoriteKnown = make(map[string]bool)
	m.lyricLines = nil
	m.lyricSource = ""
	m.lyricManual = false
	m.lyricScroll = 0
	m.entity = nil
	m.entityGeneration++
	m.covers = nil
	m.stageCovers = nil
	m.coverPending = nil
	m.coverRetry = nil
	if m.coverRetryTimer != nil {
		m.coverRetryTimer.Stop()
		m.coverRetryTimer = nil
	}
	m.coverAlternates = nil
	m.coverUsed = nil
	m.albumCovers = nil
	m.accountStore = nil
	if m.loadCancel != nil {
		m.loadCancel()
		m.loadCancel = nil
	}
	m.catalog = nil
	m.tracks = nil
	m.filtered = nil
	m.complete = false
	m.loading = false
	m.loadError = false
	m.playAllPending = false
	m.playlists = nil
	m.detail = nil
	m.detailTracks = nil
	m.remoteHistory = nil
	m.localHistory = nil
	m.scroll = [5]float32{}
	m.entityScroll = 0
	m.detailScroll = 0
	m.settingsSelect = ""
	m.queue.Replace(nil, 0)
	m.queueOpen = false
	if m.playTimer != nil {
		m.playTimer.Stop()
		m.playTimer = nil
	}
	if m.player != nil {
		m.player.Close()
		m.player = nil
	}
	m.audioState = audio.State{}
	m.resolvedDuration = 0
	m.durationMismatch = false
	m.audioRunning = false
}

func (m *model) applyLibraryResults() {
	m.mu.Lock()
	results := m.results
	m.results = nil
	m.mu.Unlock()
	for _, r := range results {
		if r.kind != "" {
			if r.epoch != m.collectionGeneration {
				continue
			}
			m.collectionLoading = r.refreshing
			if !r.refreshing {
				m.collectionCancel = nil
			}
			if r.err != nil {
				if m.expireSession(r.err) {
					continue
				}
				if errors.Is(r.err, context.Canceled) {
					continue
				}
				m.collectionFailed = true
				m.collectionMessage = collectionError(r.kind, r.err)
				continue
			}

			m.collectionFailed = false
			m.collectionMessage = ""
			if !r.cached && m.accountStore != nil {
				_ = m.accountStore.SaveCollection(r.kind, r.detailID, storage.Collection{Playlists: r.playlists, Tracks: r.tracks})
			}
			switch r.kind {
			case "playlists":
				m.playlists = r.playlists
				m.refreshImportedMeta(r.playlists)
			case "detail":
				if m.detail != nil && m.detail.ID == r.detailID {
					m.detailTracks = r.tracks
				}
			case "history":
				m.remoteHistory = r.tracks
			}
			continue
		}
		if r.search {
			if r.epoch != m.searchGeneration || r.query != m.searchQuery {
				continue
			}
			m.searchLoading = false
			m.searchCancel = nil
			m.searchError = r.err != nil
			if r.err != nil {
				if m.expireSession(r.err) {
					continue
				}
				m.setStatus("搜索失败，请重试。")
				continue
			}
			seen := make(map[string]bool)
			for _, t := range m.filtered {
				seen[t.ID] = true
			}
			before := len(m.filtered)
			for _, t := range r.tracks {
				if !seen[t.ID] {
					m.filtered = append(m.filtered, t)
					seen[t.ID] = true
				}
			}
			m.searchNext = r.searchNext
			m.searchMore = !r.complete && len(m.filtered) > before
			continue
		}
		if r.epoch != m.epoch {
			continue
		}
		m.loading = false
		m.loadCancel = nil
		m.loadError = r.err != nil
		if r.err != nil {
			if m.expireSession(r.err) {
				continue
			}
			m.setStatus("读取失败，已加载内容保留；点击重试。")
			continue
		}
		if (m.libraryRefreshing || len(m.tracks) > len(r.tracks)) && !r.complete {
			m.loadLibrary(true)
			continue
		}
		m.libraryRefreshing = false
		m.tracks, m.complete = r.tracks, r.complete
		for _, track := range m.tracks {
			m.favoriteKnown[track.ID] = true
			m.favoriteOff[track.ID] = false
		}
		if m.accountStore != nil && r.complete {
			_ = m.accountStore.SaveLibrary(m.tracks)
		}
		if m.playAllPending && r.complete {
			m.playAllPending = false
			m.startTracks(m.tracks, 0)
		}
		if !m.demo && !r.complete {
			m.loadLibrary(true)
		}
	}
}

func (m *model) search() {
	if m.searchCancel != nil {
		m.searchCancel()
		m.searchCancel = nil
	}
	m.searchGeneration++
	m.searchLoading = false
	query := strings.TrimSpace(m.query.Text())
	m.searchError = false
	m.searchQuery = query
	m.filtered = nil
	m.searchNext = 0
	m.searchMore = false
	m.scroll[2] = 0
	if m.demo {
		m.filtered = nil
		for _, t := range syntheticTracks(m.demoCount) {
			if strings.Contains(strings.ToLower(t.Title+" "+t.ArtistText()), strings.ToLower(query)) {
				m.filtered = append(m.filtered, t)
			}
		}
		return
	}
	if query == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.searchCancel = cancel
	m.searchLoading = true
	epoch := m.searchGeneration
	m.loadSearchPage(ctx, cancel, epoch, query, 0)
}
func (m *model) moreSearch() {
	if m.searchLoading || !m.searchMore {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.searchCancel = cancel
	m.searchLoading = true
	m.searchError = false
	m.loadSearchPage(ctx, cancel, m.searchGeneration, m.searchQuery, m.searchNext)
}
func (m *model) loadSearchPage(ctx context.Context, cancel context.CancelFunc, epoch uint64, query string, start int) {
	go func() {
		defer cancel()
		tracks, next, err := m.searchTracks(ctx, query, start)
		m.mu.Lock()
		if !m.closed {
			m.results = append(m.results, libraryResult{epoch: epoch, tracks: tracks, search: true, query: query, err: err, searchNext: next, complete: next <= start})
		}
		m.mu.Unlock()
		m.requestUpdate()
	}()
}

func (m *model) saveSettings() {
	m.settings.Sanitize()
	m.queue.Mode = m.settings.RepeatMode
	m.updateMedia()
	if m.player != nil && !m.demo {
		_ = m.player.SetVolume(m.settings.Volume)
	}
	if m.store != nil {
		if err := m.store.SaveSettings(m.settings); err != nil {
			m.setStatus("设置保存失败，本次修改仍在内存中。")
		}
	}
}
func (m *model) saveQueue() {
	store := m.accountStore
	if m.demo || store == nil {
		store = m.store
	}
	if store != nil {
		if err := store.SaveQueue(storage.SavedQueue{Tracks: m.queue.Tracks, Index: m.queue.Index, Position: m.queue.Position}); err != nil {
			m.setStatus("队列保存失败。")
		}
	}
}

func (m *model) startTracks(tracks []music.Track, index int) {
	if len(tracks) == 0 {
		return
	}
	m.queue.Mode = m.settings.RepeatMode
	m.queue.Shuffle = m.settings.Shuffle
	m.queue.Replace(tracks, index)
	m.playCurrent()
	m.saveQueue()
}

// rememberResume records the queue's restored playhead so the first play can
// continue where the user left off, without leaking that offset into a later,
// different track.
func (m *model) rememberResume(position float64) {
	if track := m.queue.Current(); track != nil && position > 0 {
		m.resumeTrackID = track.ID
		m.resumePosition = position
	}
}

func (m *model) playListTrack(tracks []music.Track, index int) {
	if index < 0 || index >= len(tracks) {
		return
	}
	m.playFailures = 0
	if m.view != 2 || m.entity != nil {
		m.startTracks(tracks, index)
		return
	}
	m.queue.Enqueue(tracks[index])
	m.playCurrent()
	m.saveQueue()
}
func (m *model) playCurrent() {
	track := m.queue.Current()
	if track == nil {
		return
	}
	// pollAudio keeps queue.Position at the live playhead, so it still holds the
	// previous track's position when a new track starts. Only a restored queue
	// that still points at the same track may resume; everything else starts at
	// zero, otherwise the new track would seek to a stale offset (slow start,
	// or an immediate "ended" when the offset is past the new duration).
	m.pendingSeek = 0
	if m.resumeTrackID != "" && track.ID == m.resumeTrackID {
		m.pendingSeek = m.resumePosition
	}
	m.resumeTrackID = ""
	m.resumePosition = 0
	m.queue.Position = 0
	m.listened = 0
	m.lastPoll = time.Time{}
	m.lastPosition = 0
	m.historyReported = false
	m.lyricManual = false
	m.lyricScroll = 0
	m.lyricLines = nil
	m.lyricSource = ""
	if m.player != nil {
		m.player.Close()
		m.player = nil
	}
	m.lyricOffset = 0
	m.currentAccess = music.AccessUnknown
	m.playedLink = music.SourceLink{}
	if m.store != nil {
		m.lyricOffset = m.store.LyricOffset(m.trackOffsetKey(*track))
	}
	m.audioState = audio.State{}
	m.resolvedDuration = 0
	m.durationMismatch = false
	m.audioRunning = false
	if !m.demo {
		m.resolveCurrent()
		return
	}
	if m.fixtureDir == "" {
		m.lyricLines = lyrics.Parse("[00:00.00]离线合成歌词\n[00:01.00]用于验证切句与高亮\n[00:02.00]不是来自真实歌曲\n[00:03.00]播放即将结束")
		m.setStatus("合成曲目已加入队列；未配置合成音频目录。")
		return
	}
	name := "tone.mp3"
	if strings.HasSuffix(track.ID, "0001") {
		name = "tone.flac"
	}
	p, err := audio.OpenLocalFixture(filepath.Join(m.fixtureDir, name))
	if err != nil {
		m.setStatus("合成音频不可用，请检查夹具目录。")
		return
	}
	m.player = p
	m.lyricTrack = ""
	m.lyricLines = lyrics.Parse("[00:00.00]离线合成歌词\n[00:01.00]用于验证切句与高亮\n[00:02.00]不是来自真实歌曲\n[00:03.00]播放即将结束")
	p.Play()
	m.audioRunning = true
	m.pollAudio()
}

func (m *model) loadCollections(kind string, force ...bool) {
	if m.demo {
		if kind == "detail" {
			m.detailTracks = syntheticTracks(min(m.demoCount, 50))
		}
		if kind == "history" {
			m.remoteHistory = syntheticTracks(min(m.demoCount, 8))
		}
		return
	}
	if m.session == nil {
		return
	}
	if m.collectionCancel != nil {
		m.collectionCancel()
	}
	m.collectionGeneration++
	ctx, cancel := context.WithCancel(context.Background())
	m.collectionCancel = cancel
	m.collectionLoading = true
	epoch, store := m.collectionGeneration, m.accountStore
	var playlist music.Playlist
	if m.detail != nil {
		playlist = *m.detail
	}
	if kind != "detail" {
		playlist = music.Playlist{}
	}
	refresh := len(force) > 0 && force[0]
	go func() {
		defer cancel()
		publish := func(r libraryResult) {
			m.mu.Lock()
			if !m.closed && ctx.Err() == nil {
				m.results = append(m.results, r)
			}
			m.mu.Unlock()
			m.requestUpdate()
		}
		if !refresh && store != nil {
			if cached, err := store.LoadCollection(kind, playlist.ID); err == nil {
				stale := !collectionFresh(cached.SavedAt, time.Now())
				publish(libraryResult{epoch: epoch, kind: kind, detailID: playlist.ID, playlists: cached.Playlists, tracks: cached.Tracks, cached: true, refreshing: stale})
				if !stale {
					return
				}
			}
		}
		catalog := m.catalogSource()
		r := libraryResult{epoch: epoch, kind: kind, detailID: playlist.ID}
		if catalog == nil {
			r.err = errNoSource
			publish(r)
			return
		}
		switch kind {
		case "playlists":
			page, err := catalog.Playlists(ctx, source.PlaylistRequest{Limit: 50})
			r.playlists, r.err = page.Items, err
		case "history":
			page, err := catalog.History(ctx, source.PageRequest{Limit: 50})
			r.tracks, r.err = page.Items, err
		case "detail":
			ref := source.EntityRef{Source: playlist.Source, Kind: "playlist", ID: playlist.ID, PlaylistKind: playlist.Kind}
			if ref.Source == "" {
				ref.Source = music.SourceHuawei
			}
			cursor := ""
			for {
				page, err := catalog.PlaylistDetail(ctx, source.EntityRequest{Ref: ref, Cursor: cursor, Limit: 50})
				if err != nil {
					r.err = err
					break
				}
				r.tracks = append(r.tracks, page.Tracks...)
				if !page.HasMore || page.Next == "" || page.Next == cursor {
					break
				}
				cursor = page.Next
			}
		}
		publish(r)
	}()
}
func (m *model) pollAudio() {
	if m.player == nil {
		return
	}
	m.audioState = m.player.Snapshot()
	m.queue.Position = m.audioState.Position
	// List APIs never return a duration, so remember the one the decoder
	// reports and persist it in the queue entry. Without this a restored
	// player shows a playhead against a 0:00 total.
	if d := m.audioState.Duration; d > 0 {
		if t := m.queue.Current(); t != nil && m.learnedDurationFor != t.ID {
			t.Duration = &d
			m.learnedDurationFor = t.ID
			m.saveQueue()
		}
	}
	m.checkDuration()
	m.updateMedia()
	now := time.Now()
	if m.audioState.Playing && !m.lastPoll.IsZero() {
		delta := m.audioState.Position - m.lastPosition
		elapsed := now.Sub(m.lastPoll).Seconds()
		if delta > 0 && delta <= elapsed+1 {
			m.listened += min(delta, elapsed)
		}
	}
	m.lastPoll = now
	m.lastPosition = m.audioState.Position
	if m.audioState.Ready && m.pendingSeek > 0 {
		m.seekPlayback(m.pendingSeek)
		m.pendingSeek = 0
	}
	if m.audioState.Playing && m.queue.Current() != nil && m.lyricTrack != m.queue.Current().ID {
		track := *m.queue.Current()
		m.lyricTrack = track.ID
		items := []music.Track{track}
		for _, old := range m.localHistory {
			if old.ID != track.ID {
				items = append(items, old)
			}
			if len(items) >= 200 {
				break
			}
		}
		m.localHistory = items
		if m.accountStore != nil {
			_ = m.accountStore.SaveHistory(items)
		}
	}
	m.queue.Position = m.audioState.Position
	if time.Since(m.lastQueueSave) > 5*time.Second {
		m.saveQueue()
		m.lastQueueSave = time.Now()
	}
	if m.audioState.Ended {
		m.audioRunning = false
		m.saveQueue()
		if m.queue.Step(1, true) {
			m.playCurrent()
		}
		return
	}
	if m.audioState.Failed {
		m.audioRunning = false
		if m.skipUnplayable() {
			return
		}
		m.setStatus("原生音频播放失败。")
		return
	}
	// Schedule only while playing/buffering; paused and idle windows have no timer.
	if m.playTimer != nil {
		m.playTimer.Stop()
	}
	m.playTimer = time.AfterFunc(100*time.Millisecond, m.requestUpdate)
}

func collectionFresh(saved, now time.Time) bool {
	age := now.Sub(saved)
	return age >= 0 && age < 5*time.Minute
}

func collectionError(kind string, err error) string {
	name := map[string]string{"playlists": "歌单", "detail": "歌单详情", "history": "播放历史"}[kind]
	var failure *source.Error
	if errors.As(err, &failure) {
		if failure.Kind == source.KindLogin || failure.Kind == source.KindPermission {
			return name + "更新失败（需要重新登录），保留已有内容。"
		}
		return name + "更新失败（" + string(failure.Kind) + "），保留已有内容，请刷新重试。"
	}
	return name + "更新失败，请检查网络后刷新；已有内容保留。"
}

func (m *model) refreshLibrary() {
	if m.catalog == nil || m.loading {
		return
	}
	m.catalog.Reset()
	m.libraryRefreshing = true
	m.loadError = false
	m.loadLibrary(true)
}
