package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"huawei-music-desktop/native/internal/huawei"
)

type persistenceResult struct {
	version uint64
	err     error
}
type persistenceState struct {
	sessionTimer   *time.Timer
	sessionChecks  []sessionCheckResult
	persistMu      sync.Mutex
	persistVersion atomic.Uint64
	persistResults []persistenceResult
}

func (m *model) persistSession() {
	if m.store == nil || m.session == nil {
		return
	}
	version := m.persistVersion.Add(1)
	store, session := m.store, m.session
	go func() {
		m.persistMu.Lock()
		if m.persistVersion.Load() != version {
			m.persistMu.Unlock()
			return
		}
		err := store.SaveSession(session)
		m.persistMu.Unlock()
		m.mu.Lock()
		if !m.closed {
			m.persistResults = append(m.persistResults, persistenceResult{version, err})
		}
		m.mu.Unlock()
		m.requestUpdate()
	}()
}
func (m *model) applyPersistence() {
	m.mu.Lock()
	results := m.persistResults
	m.persistResults = nil
	m.mu.Unlock()
	for _, r := range results {
		if r.version != m.persistVersion.Load() {
			continue
		}
		if r.err != nil {
			m.status = "本地凭据保存失败，退出后需重新登录。"
		} else {
			m.status = "登录成功，音乐会话已保存到本机。"
		}
	}
}

func defaultProfile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "huawei-music-desktop", "native-v1")
}

// Called only for the normal app launch. Automated/headless tests never read a
// default profile.
func (m *model) restoreSession() {
	if m.demo || m.store == nil {
		return
	}
	m.restoring = true
	m.status = "正在恢复音乐会话…"
	generation := m.generation
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	m.cancel = cancel
	go func() {
		defer cancel()
		session, err := m.store.LoadSession()
		// A network outage must not destroy a saved session or its cache.
		if err == nil {
			validation := huawei.NewClient().ValidateSession(ctx, session)
			if huawei.SessionExpired(validation) {
				err = validation
			}
		}
		m.mu.Lock()
		if !m.closed {
			m.pending = append(m.pending, completion{generation: generation, session: session, err: err, restored: true})
		}
		m.mu.Unlock()
		m.requestUpdate()
	}()
}
func (m *model) attachAccount() {
	if m.store == nil || m.session == nil {
		return
	}
	m.accountStore = m.store.Account(m.session.AccountKey())
	if saved, err := m.accountStore.LoadQueue(); err == nil {
		m.queue.Mode = m.settings.RepeatMode
		m.queue.Shuffle = m.settings.Shuffle
		m.queue.Replace(saved.Tracks, saved.Index)
		m.queue.Position = saved.Position
	}
	m.localHistory, _ = m.accountStore.LoadHistory()
	if cached, err := m.accountStore.LoadLibrary(); err == nil && len(cached) > 0 {
		m.tracks = cached
		for _, track := range cached {
			m.favoriteKnown[track.ID] = true
		}
		m.status = "正在更新音乐库 · 缓存可能不是最新"
	}
}
func (m *model) logout() {
	if m.sessionTimer != nil {
		m.sessionTimer.Stop()
		m.sessionTimer = nil
	}
	m.persistVersion.Add(1)
	m.cancelLogin()
	accountStore := m.accountStore
	cleanupFailed := false
	m.clearLibrary()
	if accountStore != nil {
		if err := accountStore.ClearPersonal(); err != nil {
			cleanupFailed = true
		}
	}
	m.accountStore = nil
	m.session = nil
	if m.store != nil {
		m.persistMu.Lock()
		err := m.store.DeleteSession()
		m.persistMu.Unlock()
		if err != nil {
			m.status = "已退出内存会话，但本地凭据清理失败，请重试"
			return
		}
	}
	m.status = "已退出账号"
	if cleanupFailed {
		m.status = "已退出账号，但本机音乐库缓存清理失败"
	}
}

type sessionCheckResult struct {
	epoch uint64
	err   error
}

func (m *model) expireSession(err error) bool {
	if !huawei.SessionExpired(err) {
		return false
	}
	m.logout()
	m.status = "音乐会话已过期，请重新登录。"
	return true
}
func (m *model) scheduleSessionCheck() {
	if m.session == nil || m.demo {
		return
	}
	if m.sessionTimer != nil {
		m.sessionTimer.Stop()
	}
	epoch, session := m.accountEpoch, m.session
	m.sessionTimer = time.AfterFunc(5*time.Minute, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := huawei.NewClient().ValidateSession(ctx, session)
		m.mu.Lock()
		if !m.closed {
			m.sessionChecks = append(m.sessionChecks, sessionCheckResult{epoch, err})
		}
		m.mu.Unlock()
		m.requestUpdate()
	})
}
func (m *model) applySessionChecks() {
	m.mu.Lock()
	results := m.sessionChecks
	m.sessionChecks = nil
	m.mu.Unlock()
	for _, r := range results {
		if r.epoch != m.accountEpoch {
			continue
		}
		if !m.expireSession(r.err) {
			m.scheduleSessionCheck()
		}
	}
}
