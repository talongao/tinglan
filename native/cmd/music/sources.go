package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/vivaui/viva/app"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"path/filepath"

	"github.com/vivaui/viva/flex"
	"tinglan/native/internal/canonical"
	"tinglan/native/internal/jev"
	"tinglan/native/internal/music"
	"tinglan/native/internal/playback"
	"tinglan/native/internal/plugin"
	"tinglan/native/internal/source"
	"tinglan/native/internal/sources/builtin"
	"tinglan/native/internal/storage"
)

// sourceState owns the account-independent local library, the adapter registry
// and the optional Jev client. The UI list is a projection of canonical rows.
type sourceState struct {
	local         *canonical.Library
	registry      *source.Registry
	resolver      *playback.Resolver
	rows          []music.CanonicalTrack
	rowIndex      map[string]int
	importing     bool
	importStatus  string
	importCancel  context.CancelFunc
	sourceResults []sourceResult
	currentAccess music.Access
	playedLink    music.SourceLink
	rowSources    map[string][]music.SourceID

	resolveCache map[string]cachedResolve
	resolveMu    sync.Mutex

	jevActive     *jev.Client
	jevIdentity   jev.Identity
	jevModels     []jev.Model
	jevFetching   bool
	jevTesting    bool
	jevError      string
	jevTestStatus string
	jevCache      *jev.ModelCache
	devSource     builtin.Interactive
	plugins       *plugin.Manager
	pluginEntries []plugin.Entry
	pluginBusy    bool
	pluginPicking bool
	pluginPickAt  time.Time

	jevModelField    flex.Field
	jevEndpointField flex.Field
	jevKeyField      flex.Field
	fieldView        flex.View
}

type sourceResult struct {
	kind     string
	progress canonical.Progress
	result   canonical.ImportResult
	models   []jev.Model
	identity jev.Identity
	err      error
	message  string
}

func (m *model) openLocalLibrary() {
	if m.store == nil {
		return
	}
	m.local = nil
	library, err := canonical.Open(m.store.Dir())
	if err != nil {
		m.setStatus("本地收藏库无法读取：" + err.Error())
		return
	}
	m.local = library
	m.registry = source.NewRegistry()
	m.loadPlugins()
	m.registerDevFallback()
	m.seedLegacySession()
	m.initResourceClient()
	m.resolver = playback.New(m.registry)
	m.rowIndex = make(map[string]int)
	m.jevCache = jev.NewModelCache()
	m.jevKeyField.Secure = true
	m.jevEndpointField.SetText(m.settings.Jev.Endpoint)
	if m.settings.Jev.Model != "" {
		m.jevModelField.SetText(m.settings.Jev.Model)
	}
	m.installLocalLibrary()
	m.refreshPlaylists()
}

// registerDevFallback keeps `go run ./cmd/music` and the test suite usable when
// the bundled plugin is absent. The shipped app always has the plugin, so this
// adapter is never registered in a release build.
// registerDevFallback keeps `go run` and the test binary usable without a
// packaged plugin. A shipped app always carries the plugin, so silently
// falling back to the in-process adapter there would hide a packaging bug and
// put provider code back inside the shell.
func (m *model) registerDevFallback() {
	if m.registry == nil {
		return
	}
	if _, ok := m.registry.Source(music.SourceHuawei); ok {
		return
	}
	if os.Getenv("TINGLAN_DEV_SOURCE") != "1" {
		if info, err := os.Stat(bundledPluginRoot()); err == nil && info.IsDir() {
			return
		}
	}
	fallback := builtin.Huawei(func() *source.Session { return m.session })
	if err := m.registry.RegisterSource(fallback); err == nil {
		m.devSource = fallback
	}
}

func (m *model) installLocalLibrary() {
	if m.local == nil {
		return
	}
	rows := m.local.Snapshot()
	m.rows = rows
	m.rowIndex = make(map[string]int, len(rows))
	projected := make([]music.Track, 0, len(rows))
	m.rowSources = make(map[string][]music.SourceID, len(rows))
	for i, row := range rows {
		m.rowIndex[row.ID] = i
		m.rowSources[row.ID] = row.Sources()
		projected = append(projected, m.projectRow(row))
	}
	m.tracks = projected
	m.filtered = projected
	m.complete = true
	m.loading = false
	m.loadError = false
	if len(projected) > 0 && strings.HasPrefix(m.status, "本地收藏为空") {
		m.setStatus("")
	}
	m.favoriteKnown = make(map[string]bool)
	m.favoriteOff = make(map[string]bool)
	for _, track := range projected {
		m.favoriteKnown[track.ID] = true
		if row, ok := m.local.Row(track.ID); ok {
			for _, link := range row.Links {
				m.favoriteKnown[link.TrackID] = true
			}
		}
	}
}

func (m *model) projectRow(row music.CanonicalTrack) music.Track {
	artists := make([]music.Artist, 0, len(row.Artists))
	for _, artist := range row.Artists {
		artists = append(artists, music.Artist{ID: artist.ID, Name: artist.Name})
	}
	var album *music.Album
	if row.Album != nil {
		album = &music.Album{ID: row.Album.ID, Title: row.Album.Title}
	}
	return music.Track{
		ID: row.ID, Title: row.Title, Artists: artists, Album: album,
		CoverURL: row.CoverURL, CoverURLs: row.CoverCandidates(), Duration: row.Duration,
		Collected: true,
	}
}

func (m *model) canonicalRow(id string) (music.CanonicalTrack, bool) {
	if m.local == nil {
		return music.CanonicalTrack{}, false
	}
	return m.local.Row(id)
}

// canonicalFor returns a canonical row for any playable queue item. Favorite
// rows come from the local library; search/playlist items become a transient
// single-link row so every stream goes through the same resolver.
func (m *model) canonicalFor(track music.Track) (music.CanonicalTrack, bool) {
	if row, ok := m.canonicalRow(track.ID); ok {
		return restrictToSource(row, string(track.OnlySource)), true
	}
	neutral := track
	if neutral.Source == "" {
		// Rows reaching the shell without a source come from the built-in
		// adapter; transient search results already carry one.
		neutral.Source = music.SourceHuawei
	}
	if !neutral.Valid() {
		return music.CanonicalTrack{}, false
	}
	return music.NewCanonical(neutral, 0, time.Now()), true
}

// restrictToSource drops bindings from other platforms. Imported playlists set
// OnlySource because their membership mirrors one remote playlist; a row that
// also exists elsewhere must not silently play from the other platform.
func restrictToSource(row music.CanonicalTrack, only string) music.CanonicalTrack {
	if only == "" {
		return row
	}
	kept := make([]music.SourceLink, 0, len(row.Links))
	for _, link := range row.Links {
		if link.Source.String() == only {
			kept = append(kept, link)
		}
	}
	row.Links = kept
	return row
}

func (m *model) matchJudge() (music.Judge, music.MatchPolicy) {
	policy := music.DefaultMatchPolicy()
	if !m.settings.Jev.Enabled {
		return nil, policy
	}
	client, err := m.jevClient()
	if err != nil || client == nil {
		return nil, policy
	}
	if m.settings.Jev.Threshold > 0 {
		policy.JevMerge = m.settings.Jev.Threshold
	}
	return jev.Judge{Client: client}, policy
}

func (m *model) jevClient() (*jev.Client, error) {
	if m.store == nil {
		return nil, jev.ErrNotConfigured
	}
	if !m.settings.Jev.Enabled {
		return nil, jev.ErrDisabled
	}
	key, err := m.store.LoadSecret(jev.SecretName(m.settings.Jev.Endpoint))
	if errors.Is(err, os.ErrNotExist) {
		return nil, jev.ErrNotConfigured
	}
	if err != nil {
		return nil, err
	}
	config := jev.Config{
		Enabled:   true,
		Endpoint:  m.settings.Jev.Endpoint,
		APIKey:    strings.TrimSpace(string(key)),
		Model:     m.settings.Jev.Model,
		Threshold: m.settings.Jev.Threshold,
	}
	client, err := jev.New(config)
	if err != nil {
		return nil, err
	}
	m.jevActive = client
	m.jevIdentity = client.Identity()
	return client, nil
}

func (m *model) fetchJevModels() {
	if m.jevFetching {
		return
	}
	client, err := m.jevClient()
	if err != nil {
		m.jevError = jevErrorText(err)
		return
	}
	m.jevFetching = true
	m.jevError = ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		models, err := client.Models(ctx)
		m.postSource(sourceResult{kind: "jev-models", models: models, identity: client.Identity(), err: err})
	}()
}

func (m *model) jevConfigDirty() bool {
	if endpoint := strings.TrimSpace(m.jevEndpointField.Text()); endpoint != "" && endpoint != m.settings.Jev.Endpoint {
		return true
	}
	if strings.TrimSpace(m.jevKeyField.Text()) != "" {
		return true
	}
	if model := strings.TrimSpace(m.jevModelField.Text()); model != "" && model != m.settings.Jev.Model {
		return true
	}
	return false
}

func (m *model) testJevConfig() {
	if m.jevTesting {
		return
	}
	if m.jevConfigDirty() {
		m.jevTestStatus = "请先保存配置"
		return
	}
	if !m.settings.Jev.Enabled {
		m.jevTestStatus = "请先启用决策模型"
		return
	}
	if strings.TrimSpace(m.settings.Jev.Model) == "" {
		m.jevTestStatus = "请先选择或添加模型"
		return
	}
	client, err := m.jevClient()
	if err != nil {
		m.jevTestStatus = "不可用：" + jevErrorText(err)
		return
	}
	m.jevTesting = true
	m.jevTestStatus = "正在测试…"
	identity := client.Identity()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		err := client.SelfTest(ctx)
		m.postSource(sourceResult{kind: "jev-test", identity: identity, err: err})
	}()
}

func jevErrorText(err error) string {
	if err == nil {
		return ""
	}
	var api *jev.Error
	if errors.As(err, &api) {
		switch api.Kind {
		case jev.KindAuth:
			return "API Key 无效或没有权限"
		case jev.KindModel:
			return "模型不存在"
		case jev.KindRateLimit:
			return "请求过于频繁"
		case jev.KindNetwork:
			return "网络不可用"
		}
	}
	if errors.Is(err, jev.ErrDisabled) {
		return "未启用决策模型"
	}
	if errors.Is(err, jev.ErrNotConfigured) {
		return "缺少 API Key"
	}
	return "请求失败"
}

func (m *model) postSource(result sourceResult) {
	m.mu.Lock()
	if !m.closed {
		m.sourceResults = append(m.sourceResults, result)
	}
	m.mu.Unlock()
	m.requestUpdate()
}

func (m *model) applySourceResults() {
	m.mu.Lock()
	results := m.sourceResults
	m.sourceResults = nil
	m.mu.Unlock()
	for _, result := range results {
		switch result.kind {
		case "import-progress":
			m.importStatus = fmt.Sprintf("正在导入 %s · 第 %d 页 · %d 首", sourceLabel(result.message), result.progress.Pages, result.progress.Result.Added+result.progress.Result.Merged+result.progress.Result.Refreshed)
		case "import-done":
			if result.err != nil {
				m.installLocalLibrary()
				m.importStatus = "导入失败：" + friendlySourceError(result.err)
				m.importing = false
				m.importCancel = nil
				continue
			}
			m.importing = false
			m.importCancel = nil
			m.importStatus = fmt.Sprintf("%s 导入完成：新增 %d · 合并 %d · 更新 %d", sourceLabel(result.message), result.result.Added, result.result.Merged, result.result.Refreshed)
			m.installLocalLibrary()
			m.notify(fmt.Sprintf("%s 导入完成：新增 %d 首", sourceLabel(result.message), result.result.Added))
		case "playlist-done":
			if result.err != nil {
				m.notify("歌单更新失败：" + friendlySourceError(result.err))
				continue
			}
			if m.localDetail != nil && m.localDetail.ID == result.message {
				m.openPlaylist(*m.localDetail)
			}
			m.refreshPlaylists()
			m.notify("歌单已更新")
		case "jev-models":
			if result.identity.Generation != m.jevIdentity.Generation {
				// The endpoint/key/model changed while this request was in flight.
				continue
			}
			m.jevFetching = false
			if result.err != nil {
				m.jevError = jevErrorText(result.err)
				continue
			}
			m.jevModels = result.models
			m.jevError = ""
			if m.jevCache != nil {
				m.jevCache.Put(result.identity, result.models)
				if merged := jev.Merge(result.models, manualModels(m.settings.Jev.Models), m.settings.Jev.Model); len(merged) > 0 {
					m.jevModels = merged
				}
			}
		case "jev-test":
			m.jevTesting = false
			if result.identity.Generation != m.jevIdentity.Generation {
				m.jevTestStatus = "配置已变更，请重新测试"
				continue
			}
			if result.err != nil {
				m.jevTestStatus = "不可用：" + jevErrorText(result.err)
				continue
			}
			m.jevTestStatus = "可用 · " + m.settings.Jev.Model
		}
	}
}

// toggleSourceRow routes the switch to the right mechanism: plugins are
// processes that must be started or stopped, while built-in sources only need
// their setting flipped.
func (m *model) toggleSourceRow(row sourceRow) {
	if !m.isPluginSource(row.id) {
		m.toggleSource(row.id)
		return
	}
	m.setSourceEnabled(row.id, !row.enabled)
	if row.enabled {
		m.disablePlugin(row.id)
		return
	}
	if m.plugins != nil && m.plugins.Consented(row.id) {
		m.enablePlugin(row.id)
		return
	}
	m.askEnablePlugin(row.id)
}

// setSourceEnabled records the user's preference; it does not touch processes.
func (m *model) setSourceEnabled(id music.SourceID, enabled bool) {
	for i := range m.settings.Sources {
		if m.settings.Sources[i].ID == id.String() {
			m.settings.Sources[i].Enabled = enabled
			m.saveSettings()
			return
		}
	}
	m.settings.Sources = append(m.settings.Sources, storage.SourceSetting{ID: id.String(), Enabled: enabled})
	m.settings.Sanitize()
	m.saveSettings()
}

func (m *model) toggleSource(id music.SourceID) {
	defer m.clampSearchSource()
	defer m.refreshSourceAuth(id)
	for i := range m.settings.Sources {
		if m.settings.Sources[i].ID == id.String() {
			m.settings.Sources[i].Enabled = !m.settings.Sources[i].Enabled
			m.saveSettings()
			return
		}
	}
	m.settings.Sources = append(m.settings.Sources, storage.SourceSetting{ID: id.String(), Enabled: true})
	m.settings.Sanitize()
	m.saveSettings()
}

func (m *model) isPluginSource(id music.SourceID) bool {
	_, ok := m.pluginEntry(id)
	return ok
}

// sourceDisplayName prefers the descriptor of a registered source so external
// plugins are not labelled by their machine id.
func (m *model) sourceDisplayName(id music.SourceID) string {
	if entry, ok := m.pluginEntry(id); ok {
		return entry.Name()
	}
	if m.registry != nil {
		if src, ok := m.registry.Source(id); ok {
			if name := src.Describe().Name; name != "" {
				return name
			}
		}
	}
	return sourceLabel(id.String())
}

func (m *model) sourceEnabled(id music.SourceID) bool {
	if entry, ok := m.pluginEntry(id); ok {
		return entry.Enabled
	}
	for _, setting := range m.settings.Sources {
		if setting.ID == id.String() {
			return setting.Enabled
		}
	}
	return false
}

func sourceLabel(source string) string {
	switch music.SourceID(source) {
	case music.SourceHuawei:
		return "华为音乐"
	case music.SourceNetEase:
		return "网易云音乐"
	}
	return source
}

func friendlySourceError(err error) string {
	var classified *source.Error
	if errors.As(err, &classified) {
		switch classified.Kind {
		case source.KindLogin:
			return "登录已失效，请重新登录"
		case source.KindPermission:
			return "没有权限读取"
		case source.KindRateLimit:
			return "请求过于频繁"
		case source.KindUnsupported:
			return "该音源不支持此操作"
		case source.KindCanceled:
			return "已取消"
		}
	}
	return "请检查网络后重试"
}

func manualModels(values []string) []jev.Model {
	out := make([]jev.Model, 0, len(values))
	for _, value := range values {
		out = append(out, jev.Model{Name: value, Description: "手动添加"})
	}
	return out
}

func (m *model) trackLyricsKey(track music.Track) string {
	if row, ok := m.canonicalFor(track); ok {
		if links := row.SortedLinks(); len(links) > 0 {
			return lyricsCacheKey(links[0])
		}
	}
	return string(music.SourceHuawei) + ":" + track.ID
}

// trackOffsetKey identifies one queue row, not one source binding. Lyrics text
// may be shared per link, but the user's offset belongs to the canonical row.
func (m *model) trackOffsetKey(track music.Track) string {
	if m.local != nil {
		if row, ok := m.local.Row(track.ID); ok {
			return row.ID
		}
	}
	return string(music.SourceHuawei) + ":" + track.ID
}

func (m *model) removeLocalFavorite(id string) {
	if m.local == nil {
		return
	}
	if err := m.local.Delete(id); err != nil {
		m.setStatus("本地收藏删除失败")
		return
	}
	m.installLocalLibrary()
	m.setStatus("已从本地收藏移除")
}

func (m *model) saveJevConfig() {
	if m.store == nil {
		return
	}
	m.jevTestStatus = ""
	before := m.settings.Jev.Endpoint
	endpoint, err := jev.NormalizeEndpoint(m.jevEndpointField.Text())
	if err != nil {
		m.setStatus("决策模型 API 地址格式不正确；可填 http(s)://主机[:端口][/路径]，会自动补 /v1/systemone")
		return
	}
	if model := strings.TrimSpace(m.jevModelField.Text()); model != "" {
		normalized, err := jev.NormalizeModelID(model)
		if err != nil {
			m.setStatus("模型 ID 无效")
			return
		}
		m.settings.Jev.Model = normalized
	}
	m.settings.Jev.Endpoint = endpoint
	m.settings.Sanitize()
	m.jevEndpointField.SetText(m.settings.Jev.Endpoint)
	keyChanged := false
	failed := false
	if key := strings.TrimSpace(m.jevKeyField.Text()); key != "" {
		if err := m.store.SaveSecret(jev.SecretName(m.settings.Jev.Endpoint), []byte(key)); err != nil {
			m.setStatus("决策模型 API Key 保存失败")
			failed = true
		} else {
			m.jevKeyField.SetText("")
			keyChanged = true
		}
	}
	m.saveSettings()
	m.jevIdentity = jev.Identity{}
	m.jevActive = nil
	m.jevFetching = false
	m.jevTesting = false
	m.jevTestStatus = ""
	if before != m.settings.Jev.Endpoint || keyChanged {
		m.jevModels = nil
		m.jevError = ""
		m.jevCache = jev.NewModelCache()
		m.jevModelField.SetText(m.settings.Jev.Model)
	}
	if !failed {
		m.setStatus("决策模型配置已保存")
		if jev.InsecureEndpoint(m.settings.Jev.Endpoint) {
			m.setStatus("决策模型配置已保存（明文 HTTP，API Key 会发往该地址）")
		}
	}
}

func (m *model) addJevModel() {
	model, err := jev.NormalizeModelID(m.jevModelField.Text())
	if err != nil {
		m.setStatus("模型 ID 无效")
		return
	}
	for _, existing := range m.settings.Jev.Models {
		if existing == model {
			m.settings.Jev.Model = model
			m.saveSettings()
			return
		}
	}
	m.settings.Jev.Models = append(m.settings.Jev.Models, model)
	m.settings.Jev.Model = model
	m.settings.Sanitize()
	m.saveSettings()
	m.jevTestStatus = ""
	m.setStatus("已添加模型 " + model)
}

func (m *model) selectJevThreshold(value float64) {
	if value < .5 || value > .99 {
		return
	}
	m.settings.Jev.Threshold = value
	m.settings.Sanitize()
	m.jevActive = nil
	m.jevIdentity = jev.Identity{}
	m.jevCache = nil
	m.jevFetching = false
	m.saveSettings()
	m.setStatus(fmt.Sprintf("决策模型灰区阈值已设为 %.2f", value))
}

func (m *model) selectJevModel(name string) {
	m.settings.Jev.Model = name
	m.settings.Sanitize()
	m.saveSettings()
	m.jevModelField.SetText(name)
	m.jevTestStatus = ""
	m.setStatus("已选择模型 " + name)
}

// providerTracks converts neutral rows back to the provider wire shape. It is
// a migration seam: every call disappears once the shell speaks music.*.

// primarySource is the source the shell's account-scoped pages are built
// around. Every source is a plugin now, so this is a lookup rather than a
// dedicated built-in adapter.
// trackAvailable reports whether a row still has at least one enabled source.
// Rows that lost every source are rendered grey instead of pretending they can
// play; the resolver would fail anyway.
func (m *model) trackAvailable(t music.Track) bool {
	if sources, ok := m.rowSources[t.ID]; ok && len(sources) > 0 {
		for _, id := range sources {
			if m.sourceEnabled(id) {
				return true
			}
		}
		return false
	}
	if t.Source != "" {
		return m.sourceEnabled(t.Source)
	}
	return true
}

// sourceLoginable reports whether a source can start an interactive login.
func (m *model) sourceLoginable(id music.SourceID) bool {
	if entry, ok := m.pluginEntry(id); ok {
		return len(entry.LoginKinds()) > 0
	}
	return m.devSource != nil && id == music.SourceHuawei
}

func (m *model) registrySource(id music.SourceID) (source.Source, bool) {
	if m.registry == nil {
		return nil, false
	}
	return m.registry.Source(id)
}

// sourceSignedIn reports the cached login state of one source. The account
// source also mirrors the shell session so the header stays in sync.
func (m *model) sourceSignedIn(id music.SourceID) bool {
	if session := m.sourceSession(id); session != nil && session.AccountKey != "" {
		return true
	}
	status, ok := m.sourceAuth[id]
	return ok && status.State == source.AuthSignedIn
}

func (m *model) sourceSession(id music.SourceID) *source.Session {
	if m.session != nil && (m.session.Source == id || (m.session.Source == "" && id == m.accountSourceID())) {
		return m.session
	}
	return m.sourceSessions[id]
}

// refreshSourceAuth caches the async auth status so painting never blocks on an
// RPC to a plugin process.
func (m *model) refreshSourceAuth(id music.SourceID) {
	src, ok := m.registrySource(id)
	if !ok {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		status, err := src.Auth().Status(ctx)
		if err != nil {
			return
		}
		m.mu.Lock()
		if m.sourceAuth == nil {
			m.sourceAuth = make(map[music.SourceID]source.AuthStatus)
		}
		m.sourceAuth[id] = status
		m.mu.Unlock()
		m.requestUpdate()
	}()
}

// logoutSource signs out one source without wiping the local library unless it
// is the account source.
func (m *model) logoutSource(id music.SourceID) {
	if id == m.accountSourceID() {
		m.logout()
		return
	}
	if src, ok := m.registrySource(id); ok {
		_ = src.Auth().Logout(context.Background())
	}
	if m.plugins != nil {
		_ = m.plugins.ClearSession(id)
	}
	m.mu.Lock()
	delete(m.sourceSessions, id)
	delete(m.sourceAuth, id)
	m.mu.Unlock()
	m.refreshSourceAuth(id)
	m.notify("已退出" + m.sourceDisplayName(id))
}

// clampSearchSource keeps the search tab on an enabled source: disabling the
// selected plugin must not leave the page querying a source that is gone.
func (m *model) clampSearchSource() {
	if m.searchSource == "" || m.sourceEnabled(m.searchSource) {
		return
	}
	m.searchSource = ""
	m.filtered = nil
	m.searchNext = 0
	m.searchMore = false
}

// accountSourceID is the source that owns the login UI. It prefers Huawei when
// present and otherwise falls back to the first registered source.
func (m *model) accountSourceID() music.SourceID {
	if m.registry != nil {
		if _, ok := m.registry.Source(music.SourceHuawei); ok {
			return music.SourceHuawei
		}
		if list := m.registry.SourceList(); len(list) > 0 {
			return list[0].ID()
		}
	}
	return music.SourceHuawei
}

// accountSourceName is the display name used by account-related copy.
func (m *model) accountSourceName() string {
	return m.sourceDisplayName(m.accountSourceID())
}

// accountSourceReady reports whether the source owning the account UI is
// enabled. Login and logout affordances are hidden until it is, because the
// provider no longer runs inside the shell.
func (m *model) accountSourceReady() bool {
	id := m.accountSourceID()
	if m.registry == nil {
		return false
	}
	if _, ok := m.registry.Source(id); !ok {
		return false
	}
	return m.sourceEnabled(id)
}

func (m *model) primarySource() (source.Source, bool) {
	if m.registry == nil {
		return nil, false
	}
	if src, ok := m.registry.Source(music.SourceHuawei); ok {
		return src, true
	}
	list := m.registry.SourceList()
	if len(list) == 0 {
		return nil, false
	}
	return list[0], true
}

// catalogSource returns the catalogue surface of the primary source. Shell code
// must go through the Source contract rather than a provider client so the
// adapter stays the only place that knows provider endpoints.
func (m *model) catalogSource() source.CatalogSource {
	src, ok := m.primarySource()
	if !ok {
		return nil
	}
	return src.Catalog()
}

var errNoSource = errors.New("音源未注册")

// manageSource returns the provider write surface of the primary source.
func (m *model) manageSource() source.ManageSource {
	src, ok := m.primarySource()
	if !ok {
		return nil
	}
	return src.Manage()
}

// checkSession asks the source to verify its account against the network. An
// expired account becomes a login error; an unknown state stays retryable.
func (m *model) checkSession(ctx context.Context) error {
	status, err := m.verifySource(ctx)
	if err != nil {
		return err
	}
	switch status.State {
	case source.AuthExpired:
		return source.NewError(music.SourceHuawei, "verify", source.KindLogin, errors.New("session expired"))
	case source.AuthSignedOut:
		return source.NewError(music.SourceHuawei, "verify", source.KindLogin, errors.New("signed out"))
	}
	return nil
}

// loadPlugins discovers installed plugins and restarts the ones the user
// previously enabled. Failures are reported but never block the app.
func (m *model) loadPlugins() {
	if m.store == nil {
		return
	}
	m.plugins = plugin.NewManager(filepath.Join(m.store.Dir(), "sources"), m.store, pluginLogger{})
	m.plugins.AddRoot(bundledPluginRoot())
	if dir := os.Getenv("TINGLAN_PLUGIN_ROOT"); dir != "" {
		m.plugins.AddRoot(dir)
	}
	m.pluginEntries = m.plugins.List()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if failures := m.plugins.LoadEnabled(ctx, m.registry); len(failures) > 0 {
		m.setStatus("部分外置音源未能启用：" + strings.Join(failures, "；"))
	}
	m.pluginEntries = m.plugins.List()
	m.refreshAllSourceAuth()
}

// choosePluginFolder asks the system picker for a plugin directory and installs it.
// choosePluginBundle asks for a .zip plugin package and installs it.
func (m *model) choosePluginBundle() {
	if !m.beginPluginPick() {
		return
	}
	paths, err := app.OpenFiles(app.OpenFileOptions{
		Title:   "添加音源",
		Filters: []app.FileFilter{{Name: "音源包", Pattern: "*.zip"}},
	})
	m.endPluginPick()
	if errors.Is(err, app.ErrCanceled) || (err == nil && len(paths) == 0) {
		return
	}
	if err != nil {
		m.notify("无法打开选择器：" + err.Error())
		return
	}
	m.installPluginFrom(paths[0])
}

// choosePluginFolder is the development path: install straight from a folder.
func (m *model) choosePluginFolder() {
	// The native modal runs inside the click handler and keeps pumping click
	// events; a second click can arrive while it is open, so guard re-entry.
	if !m.beginPluginPick() {
		return
	}
	path, err := app.OpenDirectory(app.OpenDirectoryOptions{Title: "从文件夹添加音源"})
	m.endPluginPick()
	if errors.Is(err, app.ErrCanceled) || (err == nil && strings.TrimSpace(path) == "") {
		return
	}
	if err != nil {
		m.notify("无法打开选择器：" + err.Error())
		return
	}
	m.installPluginFrom(path)
}

func (m *model) beginPluginPick() bool {
	if m.pluginPicking {
		return false
	}
	if !m.pluginPickAt.IsZero() && time.Since(m.pluginPickAt) < 500*time.Millisecond {
		return false
	}
	m.pluginPicking = true
	return true
}

func (m *model) endPluginPick() {
	m.pluginPicking = false
	m.pluginPickAt = time.Now()
}

// installPluginFrom installs a plugin from a directory or a .zip package.
func (m *model) installPluginFrom(path string) {
	if m.plugins == nil {
		m.notify("插件管理器未就绪")
		return
	}
	dir, cleanup, err := m.preparePluginSource(path)
	if err != nil {
		m.notify("添加失败：" + err.Error())
		return
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	entry, updated, err := m.plugins.Install(ctx, dir)
	if err != nil {
		m.notify("添加失败：" + err.Error())
		return
	}
	label := entry.Name()
	if v := entry.Manifest.Version; v != "" {
		label += " " + v
	}
	m.refreshPlugins()
	if updated {
		m.notify("已更新 " + label)
		if m.sourceEnabled(entry.ID()) {
			// The old process was stopped so its files could be replaced.
			if err := m.plugins.Enable(context.Background(), m.registry, entry.ID()); err != nil {
				m.notify("插件重启失败：" + err.Error())
			}
		}
		return
	}
	m.notify("已添加 " + label)
	m.askEnablePlugin(entry.ID())
}

// preparePluginSource turns a .zip package or folder into a plugin directory.
// The returned cleanup removes any temporary extraction directory.
func (m *model) preparePluginSource(path string) (string, func(), error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", func() {}, err
	}
	if info.IsDir() {
		root, err := plugin.PluginRoot(path)
		return root, func() {}, err
	}
	if !strings.EqualFold(filepath.Ext(path), ".zip") {
		return "", func() {}, errors.New("请选择 .zip 音源包或插件文件夹")
	}
	tmp, err := os.MkdirTemp("", "tinglan-plugin-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	if err := plugin.ExtractZip(path, tmp); err != nil {
		cleanup()
		return "", func() {}, err
	}
	root, err := plugin.PluginRoot(tmp)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return root, cleanup, nil
}

// refreshPlugins re-reads the plugin directories after an install/removal.
func (m *model) refreshPlugins() {
	if m.plugins == nil {
		return
	}
	m.pluginEntries = m.plugins.List()
	m.refreshAllSourceAuth()
}

// refreshAllSourceAuth primes the cached login state for the settings page.
func (m *model) refreshAllSourceAuth() {
	for _, row := range m.sourceRows() {
		if row.enabled && row.loginable {
			m.refreshSourceAuth(row.id)
		}
	}
}

func (m *model) pluginEntry(id music.SourceID) (plugin.Entry, bool) {
	for _, entry := range m.pluginEntries {
		if string(entry.ID()) == string(id) {
			return entry, true
		}
	}
	return plugin.Entry{}, false
}

// askEnablePlugin opens the consent dialog. Enabling a plugin is an explicit
// user decision because the process runs with the user's privileges.
func (m *model) askEnablePlugin(id music.SourceID) {
	if _, ok := m.pluginEntry(id); !ok {
		return
	}
	m.dialog = "plugin-enable"
	m.dialogID = string(id)
}

func (m *model) enablePlugin(id music.SourceID) {
	if m.plugins == nil || m.pluginBusy {
		return
	}
	m.pluginBusy = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		entry, ok := m.pluginEntry(id)
		if !ok {
			m.mu.Lock()
			m.pluginBusy = false
			m.mu.Unlock()
			return
		}
		err := m.plugins.Enable(ctx, m.registry, id)
		if err != nil {
			m.mu.Lock()
			m.pluginBusy = false
			m.pluginEntries = m.plugins.List()
			m.mu.Unlock()
			m.notify("外置音源启动失败：" + err.Error())
			return
		}
		m.mu.Lock()
		m.pluginBusy = false
		m.pluginEntries = m.plugins.List()
		m.mu.Unlock()
		m.setSourceEnabled(id, true)
		m.notify("已启用 " + entry.Name())
		m.refreshSourceAuth(id)
		m.invalidator.Invalidate()
	}()
}

func (m *model) disablePlugin(id music.SourceID) {
	if m.plugins == nil || m.pluginBusy {
		return
	}
	defer m.clampSearchSource()
	m.pluginBusy = true
	entry, ok := m.pluginEntry(id)
	go func() {
		_ = m.plugins.Disable(m.registry, id)
		m.mu.Lock()
		m.pluginBusy = false
		m.pluginEntries = m.plugins.List()
		m.mu.Unlock()
		m.setSourceEnabled(id, false)
		if ok {
			m.notify("已停用 " + entry.Name())
		}
		m.invalidator.Invalidate()
	}()
}

// openPluginFolder reveals the plugin install directory. It only opens the
// system file manager; it never launches a browser or a plugin itself.
func (m *model) openPluginFolder() {
	if m.plugins == nil {
		return
	}
	dir := m.plugins.Root()
	_ = os.MkdirAll(dir, 0o755)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		m.notify("无法打开插件文件夹：" + err.Error())
		return
	}
	m.notify("插件文件夹已打开")
}

// pluginLogger routes plugin diagnostics to stderr without touching user data.
type pluginLogger struct{}

func (pluginLogger) Printf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "plugin: "+format+"\n", args...)
}
