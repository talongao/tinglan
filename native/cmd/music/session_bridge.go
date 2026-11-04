package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
)

// bundledPluginRoot is where the installer keeps the preinstalled sources. On
// macOS they live in Tinglan.app/Contents/Resources/sources; elsewhere the
// binary ships next to a sources directory.
func bundledPluginRoot() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	if runtime.GOOS == "darwin" && filepath.Base(dir) == "MacOS" {
		return filepath.Join(filepath.Dir(dir), "Resources", "sources")
	}
	return filepath.Join(dir, "sources")
}

// seedLegacySession moves a session saved before the Huawei source became a
// plugin into the plugin's own namespace. It is a one-time compatibility step
// and never overwrites a session the plugin already has.
func (m *model) seedLegacySession() {
	if m.plugins == nil || m.store == nil {
		return
	}
	entry, ok := m.pluginEntry(m.accountSourceID())
	if !ok || !entry.Enabled {
		return
	}
	session, err := m.store.LoadSession()
	if err != nil || session == nil || session.Token == "" || session.UserID == "" {
		return
	}
	// Never overwrite a working plugin session; drop a stale seed left by an
	// older build that hashed the account key twice.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if src, ok := m.registry.Source(entry.ID()); ok {
		status, statusErr := src.Auth().Status(ctx)
		if statusErr == nil && status.AccountKey == session.AccountKey {
			_ = m.store.DeleteSession()
			return
		}
		if statusErr == nil && status.AccountKey != "" {
			if err := m.plugins.ClearSession(entry.ID()); err != nil {
				return
			}
		}
	}
	raw, err := json.Marshal(map[string]string{
		"token":   session.Token,
		"userID":  session.UserID,
		"expires": session.ExpiresAt,
	})
	if err != nil {
		return
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	if err := m.plugins.SeedSession(entry.ID(), "session", encoded); err != nil {
		m.setStatus("旧登录状态迁移失败，请重新登录")
		return
	}
	_ = m.store.DeleteSession()
}

// sourceStatus is the local account snapshot from the primary source.
func (m *model) sourceStatus(ctx context.Context) (source.AuthStatus, error) {
	src, ok := m.primarySource()
	if !ok {
		return source.AuthStatus{State: source.AuthSignedOut}, nil
	}
	return src.Auth().Status(ctx)
}

// verifySource checks the account against the network, keeping an unknown state
// (for example a network outage) distinct from an expired session.
func (m *model) verifySource(ctx context.Context) (source.AuthStatus, error) {
	src, ok := m.primarySource()
	if !ok {
		return source.AuthStatus{State: source.AuthSignedOut}, nil
	}
	return src.Auth().Verify(ctx)
}

// mirrorSession builds the account identity the shell displays. Credentials
// live only inside the source, never in the shell model.
func mirrorSession(sourceID music.SourceID, status source.AuthStatus) *source.Session {
	if status.AccountKey == "" {
		return nil
	}
	return &source.Session{Source: sourceID, AccountKey: status.AccountKey, ExpiresAt: status.ExpiresAt}
}

// loginSource is the source that owns the account UI. Every source is a plugin
// now, so the shell does not keep a provider adapter of its own.
func (m *model) loginSource() (source.Source, bool) {
	if id := m.loginTarget; id != "" {
		if m.registry != nil {
			if src, ok := m.registry.Source(id); ok {
				return src, true
			}
		}
	}
	return m.primarySource()
}

// runSourceLogin performs one interactive login through the source contract and
// returns the account identity the shell mirrors in its own state.
func (m *model) runSourceLogin(ctx context.Context) (*source.Session, error) {
	if m.signIn != nil {
		return m.signIn(ctx)
	}
	src, ok := m.loginSource()
	if !ok {
		return nil, fmt.Errorf("音源未初始化")
	}
	attempt := fmt.Sprintf("attempt-%d", time.Now().UnixNano())
	result, err := src.Auth().Login(ctx, source.LoginRequest{Kind: source.LoginWebView, AttemptID: attempt})
	if err != nil {
		return nil, err
	}
	m.loginSaved = result.Persisted
	if !result.Succeeded || result.Status.AccountKey == "" {
		return nil, source.NewError(music.SourceHuawei, "login", source.KindCanceled, fmt.Errorf("登录已取消"))
	}
	target := m.loginTarget
	if target == "" {
		target = m.accountSourceID()
	}
	return &source.Session{
		Source:     target,
		AccountKey: result.Status.AccountKey,
		ExpiresAt:  result.Status.ExpiresAt,
	}, nil
}
