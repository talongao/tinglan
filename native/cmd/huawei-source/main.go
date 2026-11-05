// Command huawei-source is the preinstalled Huawei Music source, shipped as an
// external plugin so the shell reaches every source through the same protocol.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"tinglan/native/internal/huawei"
	"tinglan/native/internal/plugin/sdk"
	"tinglan/native/internal/source"
)

const sessionKey = "session"

func debugf(format string, args ...any) {
	if os.Getenv("TINGLAN_PLUGIN_DEBUG") == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "huawei-source: "+format+"\n", args...)
}

func main() {
	store := &sessionStore{client: huawei.NewClient()}
	adapter := huawei.NewSourceAdapter(store.client, store.current)
	plugin := &sdk.Plugin{
		Source:       adapter,
		Auth:         &pluginAuth{adapter: adapter, store: store, client: store.client},
		HostServices: []string{"session", "webview", "log"},
		OnReady:      func(host *sdk.Host) { store.host = host },
	}
	if err := plugin.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "huawei-source:", err)
		os.Exit(1)
	}
}

type sessionStore struct {
	client *huawei.Client
	host   *sdk.Host

	mu       sync.Mutex
	session  *huawei.Session
	revision int64
	loaded   bool
}

func (s *sessionStore) current() *source.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		return nil
	}
	return huawei.NeutralSession(s.session)
}

func (s *sessionStore) load(ctx context.Context) error {
	s.mu.Lock()
	if s.loaded {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	raw, revision, err := s.host.SessionGet(ctx, sessionKey)
	if err != nil {
		return err
	}
	session, err := huawei.ImportSession(raw)
	if raw == "" || err != nil {
		session = nil
	}
	s.mu.Lock()
	s.session = session
	s.revision = revision
	s.loaded = true
	s.mu.Unlock()
	return nil
}

func (s *sessionStore) set(session *huawei.Session, revision int64) {
	s.mu.Lock()
	s.session = session
	s.revision = revision
	s.loaded = true
	s.mu.Unlock()
}

type pluginAuth struct {
	adapter *huawei.SourceAdapter
	store   *sessionStore
	client  *huawei.Client
}

func (a *pluginAuth) Status(ctx context.Context) (source.AuthStatus, error) {
	if err := a.store.load(ctx); err != nil {
		return source.AuthStatus{State: source.AuthUnknown}, nil
	}
	return a.adapter.Auth().Status(ctx)
}

func (a *pluginAuth) Verify(ctx context.Context) (source.AuthStatus, error) {
	if err := a.store.load(ctx); err != nil {
		debugf("verify load: %v", err)
		return source.AuthStatus{State: source.AuthUnknown}, nil
	}
	status, err := a.adapter.Auth().Verify(ctx)
	debugf("verify: state=%s err=%v", status.State, err)
	return status, err
}

func (a *pluginAuth) Login(ctx context.Context, request source.LoginRequest) (source.AuthResult, error) {
	if request.Kind != source.LoginWebView {
		return source.AuthResult{}, errors.New("only webview login is supported")
	}
	attempt := huawei.NewAttempt(time.Now())
	defer attempt.Cancel()
	result, err := a.store.host.WebViewOpen(ctx, sdk.WebViewRequest{
		AttemptID: request.AttemptID,
		URL:       attempt.AuthorizeURL(),
		AllowedDomains: []string{
			"oauth-login.platform.hicloud.com",
			"oauth-login.cloud.huawei.com",
			"oauth-login1.cloud.huawei.com",
			"id1.cloud.huawei.com",
			"account.huawei.com",
			"consumer.huawei.com",
		},
		Callback: source.CallbackRule{Scheme: "hms", Host: "redirect_uri"},
	})
	if err != nil {
		return source.AuthResult{}, err
	}
	if result.Canceled || result.CallbackURL == "" {
		return source.AuthResult{Status: source.AuthStatus{State: source.AuthSignedOut}}, nil
	}
	if err := attempt.Accept(result.CallbackURL, time.Now()); err != nil {
		return source.AuthResult{}, err
	}
	session, err := a.client.Exchange(ctx, attempt)
	if err != nil {
		return source.AuthResult{}, err
	}
	if err := a.client.ValidateSession(ctx, session); err != nil {
		return source.AuthResult{}, err
	}
	persisted, err := a.persist(ctx, session, request.WriteToken)
	status := source.AuthStatus{State: source.AuthSignedIn, AccountKey: session.AccountKey(), DisplayName: "华为音乐账号"}
	return source.AuthResult{Succeeded: true, Persisted: persisted, Status: status}, err
}

func (a *pluginAuth) persist(ctx context.Context, session *huawei.Session, writeToken string) (bool, error) {
	raw, err := huawei.ExportSession(session)
	if err != nil {
		return false, err
	}
	revision, err := a.store.host.SessionSet(ctx, sessionKey, raw, a.store.revision, writeToken)
	if err != nil {
		return false, err
	}
	a.store.set(session, revision)
	return true, nil
}

func (a *pluginAuth) PersistSession(ctx context.Context, request source.PersistRequest) (bool, error) {
	a.store.mu.Lock()
	session := a.store.session
	a.store.mu.Unlock()
	if session == nil {
		return false, nil
	}
	return a.persist(ctx, session, request.WriteToken)
}

func (a *pluginAuth) Logout(ctx context.Context) error {
	a.store.mu.Lock()
	a.store.session = nil
	a.store.loaded = true
	a.store.mu.Unlock()
	return nil
}
