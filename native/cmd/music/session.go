package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"tinglan/native/internal/appmeta"
	"tinglan/native/internal/source"
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

// persistSession is a no-op now: the external source persists its own session
// through the host's credential namespace during login. The shell only mirrors
// the account identity.
func (m *model) persistSession() {
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
			m.setStatus("本地凭据保存失败，退出后需重新登录。")
		} else {
			m.setStatus("登录成功，音乐会话已保存到本机。")
		}
	}
}

func defaultProfile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, filepath.FromSlash(appmeta.DataDir))
}

// Called only for the normal app launch. Automated/headless tests never read a
// default profile.
func (m *model) restoreSession() {
	if m.demo || m.store == nil {
		return
	}
	// No installed source means there is nothing to sign in to: skip the
	// restore probe entirely instead of showing a login prompt.
	if m.registry == nil || len(m.registry.SourceList()) == 0 {
		return
	}
	m.restoring = true
	m.setStatus("正在恢复登录状态…")
	generation := m.generation
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	m.cancel = cancel
	go func() {
		defer cancel()
		// The source owns the session now; the shell only mirrors the account
		// identity. A network outage keeps an unknown state instead of logging
		// the user out.
		status, err := m.verifySource(ctx)
		var session *source.Session
		switch status.State {
		case source.AuthSignedIn, source.AuthUnknown:
			session = mirrorSession(m.accountSourceID(), status)
		case source.AuthExpired:
			err = source.NewError(m.accountSourceID(), "verify", source.KindLogin, errors.New("session expired"))
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
	m.accountStore = m.store.Account(m.session.Account())
	if saved, err := m.accountStore.LoadQueue(); err == nil {
		m.queue.Mode = m.settings.RepeatMode
		m.queue.Shuffle = m.settings.Shuffle
		m.queue.Replace(saved.Tracks, saved.Index)
		m.queue.Position = saved.Position
		m.rememberResume(saved.Position)
	}
	m.localHistory, _ = m.accountStore.LoadHistory()
	// The local canonical library is the only favorites source; the per-account
	// cache is no longer copied into the visible list.
	//
	// Restoring a session must also restore the account's stored login state:
	// a stale logged_out row would mark every binding Disabled and make every
	// track look like it has no source.
	if m.local != nil {
		if _, err := m.local.EnsureAccount(context.Background(), m.session.Source, m.session.Account(), m.accountSourceName()); err == nil {
			m.installLocalLibrary()
		}
	}
}
func (m *model) logout() {
	if m.sessionTimer != nil {
		m.sessionTimer.Stop()
		m.sessionTimer = nil
	}
	m.persistVersion.Add(1)
	m.cancelLogin()
	if src, ok := m.primarySource(); ok {
		_ = src.Auth().Logout(context.Background())
	}
	accountSource := m.accountSourceID()
	if m.plugins != nil {
		_ = m.plugins.ClearSession(accountSource)
	}
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
	if m.local != nil {
		_ = m.local.DisableSource(accountSource)
		m.installLocalLibrary()
		m.setStatus("已退出账号；本地收藏仍可用")
	}
	if m.store != nil {
		m.persistMu.Lock()
		err := m.store.DeleteSession()
		m.persistMu.Unlock()
		if err != nil {
			m.setStatus("已退出内存会话，但本地凭据清理失败，请重试")
			return
		}
	}
	if m.status == "" {
		m.setStatus("已退出账号")
	}
	if cleanupFailed {
		m.setStatus("已退出账号，但本机音乐库缓存清理失败；本地收藏仍可用")
	}
}

type sessionCheckResult struct {
	epoch uint64
	err   error
}

func (m *model) expireSession(err error) bool {
	if !sessionExpired(err) {
		return false
	}
	m.logout()
	m.setStatus("音乐会话已过期，请重新登录。")
	return true
}
func (m *model) scheduleSessionCheck() {
	if m.session == nil || m.demo {
		return
	}
	if m.sessionTimer != nil {
		m.sessionTimer.Stop()
	}
	epoch := m.accountEpoch
	m.sessionTimer = time.AfterFunc(5*time.Minute, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := m.checkSession(ctx)
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
