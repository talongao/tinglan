package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"tinglan/native/internal/source"
)

func TestFailedAndStaleLoginPreserveCurrentSession(t *testing.T) {
	old := &source.Session{}
	m := &model{session: old, generation: 2, busy: true}
	m.pending = []completion{{generation: 1, session: &source.Session{}}}
	m.wake()
	if m.session != old || !m.busy {
		t.Fatal("stale login changed current state")
	}
	m.pending = []completion{{generation: 2, err: errors.New("synthetic failure")}}
	m.wake()
	if m.session != old || m.busy {
		t.Fatal("failed login cleared current session")
	}
	m.generation++
	m.pending = []completion{{generation: 3, session: &source.Session{}}}
	m.wake()
	if m.session == old {
		t.Fatal("validated candidate was not installed")
	}
}

func TestCancelDoesNotExchangeOrReplaceSession(t *testing.T) {
	started := make(chan struct{})
	done := make(chan struct{})
	old := &source.Session{}
	m := &model{session: old,
		signIn: func(ctx context.Context) (*source.Session, error) {
			close(started)
			<-ctx.Done()
			close(done)
			return nil, ctx.Err()
		},
	}
	m.startLogin()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	m.cancelLogin()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not cancel")
	}
	if m.session != old || m.busy {
		t.Fatal("cancel damaged current session")
	}
	m.close()
}

func TestFreshInstallRestorationIsNotInteractiveLogin(t *testing.T) {
	m := newModel()
	openProfileForTest(t, m, t.TempDir())
	m.restoreSession()
	if m.busy || !m.restoring {
		t.Fatal("restore incorrectly displays cancel login")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		ready := len(m.pending) > 0
		m.mu.Unlock()
		if ready {
			m.wake()
			break
		}
		time.Sleep(time.Millisecond)
	}
	if m.restoring || m.busy || m.session != nil {
		t.Fatal("fresh install did not settle to logged out")
	}
	m.close()
}

func TestExplicitLoginSupersedesSessionRestoration(t *testing.T) {
	m := newModel()
	m.restoring = true
	cancelled := false
	m.cancel = func() { cancelled = true }
	started := make(chan struct{})
	m.signIn = func(ctx context.Context) (*source.Session, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	m.startLogin()
	<-started
	if !cancelled || m.restoring || !m.busy {
		t.Fatal("restore blocked explicit login")
	}
	m.pending = []completion{{generation: m.generation - 1, restored: true, err: errors.New("old restore")}}
	m.wake()
	if !m.busy {
		t.Fatal("stale restoration ended new login")
	}
	m.cancelLogin()
	m.close()
}

// openProfileForTest opens a profile and registers cleanup, so Windows can
// delete the SQLite files during TempDir cleanup.
func openProfileForTest(t *testing.T, m *model, dir string) {
	t.Helper()
	m.openProfile(dir)
	t.Cleanup(m.close)
}
