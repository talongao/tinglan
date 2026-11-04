package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"

	"tinglan/native/internal/appmeta"
	"tinglan/native/internal/audio"
	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
	"tinglan/native/internal/webauth"
)

var (
	tree     = ui.Tree{}
	feature  = tree.Feature("auth")
	loginID  = tree.ID(feature, "login")
	cancelID = tree.ID(feature, "cancel")
	logoutID = tree.ID(feature, "logout")
	playID   = tree.ID(feature, "play-fixture")
	pauseID  = tree.ID(feature, "pause-fixture")
	seekID   = tree.ID(feature, "seek-fixture")
	stateID  = tree.ID(feature, "inspect-fixture")
)

type completion struct {
	generation uint64
	session    *source.Session
	err        error
	restored   bool
}
type model struct {
	desktopState
	sourceState
	libraryUIState
	writeState
	resourceState
	applicationState
	typeState
	persistenceState
	playAllPending     bool
	audioRunning       bool
	invalidator        app.Invalidator
	mu                 sync.Mutex
	pending            []completion
	closed             bool
	generation         uint64
	cancel             context.CancelFunc
	busy               bool
	restoring          bool
	status             string
	session            *source.Session
	sourceSessions     map[music.SourceID]*source.Session
	sourceAuth         map[music.SourceID]source.AuthStatus
	loginSaved         bool
	signIn             func(context.Context) (*source.Session, error)
	loginTarget        music.SourceID
	player             *audio.Player
	audioState         audio.State
	playbackGeneration uint64
	playbackCancel     context.CancelFunc
	playbackResults    []playbackResult
	resolving          bool
	actualQuality      string
	resolvedDuration   float64
	durationMismatch   bool
	learnedDurationFor string
	previewNoticed     bool
	playFailures       int
	pendingSeek        float64
	resumeTrackID      string
	resumePosition     float64
	startedAt          time.Time
	lastQueueSave      time.Time
	listened           float64
	lastPoll           time.Time
	lastPosition       float64
	historyReported    bool
}

func newModel() *model {
	m := &model{status: "本地收藏为空；在 设置 → 音源 登录并导入。"}
	m.initDesktop()
	m.initResourceClient()
	return m
}

func (m *model) startLogin() {
	if m.busy {
		return
	}
	if m.restoring {
		m.cancelLogin()
	}
	m.generation++
	generation := m.generation
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	m.cancel, m.busy = cancel, true
	if m.loginTarget == "" {
		m.loginTarget = m.accountSourceID()
	}
	m.setStatus("请在授权窗口完成" + m.sourceDisplayName(m.loginTarget) + "登录，完成后会自动返回听澜")
	go func() {
		defer cancel()
		var session *source.Session
		var err error
		session, err = m.runSignIn(ctx)
		m.mu.Lock()
		if !m.closed {
			m.pending = append(m.pending, completion{generation: generation, session: session, err: err})
		}
		m.mu.Unlock()
		m.requestUpdate()
	}()
}

func (m *model) cancelLogin() {
	m.generation++
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.busy, m.restoring = false, false
	m.setStatus("已取消登录")
}

func (m *model) wake() {
	m.applySessionChecks()
	m.applyPersistence()
	m.applyMedia()
	if m.reopen.Swap(false) && m.window != nil {
		m.window.Show()
	}
	m.applyCovers()
	m.applyEntities()
	m.applyWrites()
	m.applyPlaybackResults()
	m.applySourceResults()
	m.applyLibraryResults()
	if m.audioRunning {
		m.pollAudio()
	}
	m.mu.Lock()
	pending := m.pending
	m.pending = nil
	m.mu.Unlock()
	for _, result := range pending {
		if result.generation != m.generation {
			continue
		}
		m.busy, m.restoring, m.cancel = false, false, nil
		if result.err == nil && result.restored && result.session == nil {
			m.setStatus(m.sourceSignInHint())
			continue
		}
		if result.err != nil {
			if !result.restored {
				m.logPlugin("login failed: %v", result.err)
			}
			if errors.Is(result.err, webauth.ErrWebView2Missing) {
				m.dialog = "webview-guide"
				m.setStatus("登录需要 Microsoft Edge WebView2 Runtime")
				continue
			}
			if result.restored && m.expireSession(result.err) {
				continue
			}
			m.setStatus("登录未完成，请重试；原会话保持不变")
			if !result.restored {
				m.setStatus(webauth.LoginErrorMessage(result.err))
			}
			if result.restored {
				m.setStatus(m.sourceSignInHint())
			}
			continue
		}
		if result.session != nil && result.session.Source != "" && result.session.Source != m.accountSourceID() {
			m.mu.Lock()
			if m.sourceSessions == nil {
				m.sourceSessions = make(map[music.SourceID]*source.Session)
			}
			m.sourceSessions[result.session.Source] = result.session
			m.mu.Unlock()
			m.refreshSourceAuth(result.session.Source)
			m.setStatus("已登录" + m.sourceDisplayName(result.session.Source))
			continue
		}
		m.session = result.session
		if result.restored {
			m.setStatus("已恢复登录状态，正在更新音乐库。")
		} else if m.loginSaved {
			m.setStatus("登录成功，登录状态已保存在本机。")
		} else {
			m.setStatus("登录成功，但登录状态未能保存，退出应用后需重新登录。")
		}
		m.installCatalog()
		m.attachAccount()
		m.scheduleSessionCheck()
	}
}

func (m *model) close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	if m.stageTimer != nil {
		m.stageTimer.Stop()
	}
	if m.lyricResumeTimer != nil {
		m.lyricResumeTimer.Stop()
	}
	if m.coverRetryTimer != nil {
		m.coverRetryTimer.Stop()
	}
	if m.sessionTimer != nil {
		m.sessionTimer.Stop()
	}
	m.mu.Unlock()
	if m.writeCancel != nil {
		m.writeCancel()
		m.writeCancel = nil
	}
	if m.mediaController != nil {
		m.mediaController.Close()
		m.mediaController = nil
	}
	m.saveQueue()
	m.cancelPlayback()
	if m.playTimer != nil {
		m.playTimer.Stop()
	}
	if m.loadCancel != nil {
		m.loadCancel()
	}
	if m.collectionCancel != nil {
		m.collectionCancel()
	}
	if m.searchCancel != nil {
		m.searchCancel()
	}
	m.cancelLogin()
	m.mu.Lock()
	m.closed, m.pending = true, nil
	m.results = nil
	for _, r := range m.playbackResults {
		if r.player != nil {
			r.player.Close()
		}
	}
	m.playbackResults = nil
	m.mu.Unlock()
	m.session = nil
	if m.local != nil {
		_ = m.local.Close()
		m.local = nil
	}
	if m.player != nil {
		m.player.Close()
		m.player = nil
	}
}

func main() {
	// Viva drives AppKit, and AppKit must run on the process's original thread:
	// viva_system_reduce_motion hops to the main queue with dispatch_sync. Pin the
	// goroutine before any file or network work can hand it to another thread,
	// otherwise startup deadlocks with no window.
	runtime.LockOSThread()
	// Move the pre-rename profile directory on first launch so the local
	// library, queue and saved session survive the rename.
	_ = appmeta.MigrateDataDir()
	backgroundCheck := flag.Bool("check-background", false, "synthetic hidden-window playback check; requires --demo 2 and --fixture-dir")
	fixture := flag.String("audio-fixture", "", "explicit synthetic local MP3/FLAC fixture (muted)")
	demo := flag.Int("demo", 0, "explicit offline synthetic library size (1..10000)")
	fixtureDir := flag.String("fixture-dir", "", "synthetic tone.mp3/tone.flac directory")
	profile := flag.String("profile", "", "explicit isolated native profile; never reads Electron profile")
	flag.Parse()
	m := newModel()
	if *demo > 0 {
		m.enableDemo(min(*demo, 10000))
	}
	m.fixtureDir = *fixtureDir
	if *backgroundCheck {
		if *demo != 2 || *fixtureDir == "" || *profile != "" || os.Getenv("VIVA_HEADLESS") != "" {
			log.Fatal("background check requires isolated synthetic fixtures")
		}
		m.backgroundCheck = true
	}
	if *profile != "" {
		m.openProfile(*profile)
	} else if !m.demo && os.Getenv("VIVA_HEADLESS") == "" {
		if dir := defaultProfile(); dir != "" {
			m.openProfile(dir)
		}
	}
	if *fixture != "" {
		var err error
		m.player, err = audio.OpenLocalFixture(*fixture)
		if err != nil {
			log.Fatal("local audio fixture unavailable")
		}
		defer m.player.Close()
	}
	if err := runMusic(m); err != nil {
		log.Fatal("native application failed")
	}
}

// sessionExpired reports whether a validation failure means the account needs to
// sign in again, without exposing provider error types to the shell.
func sessionExpired(err error) bool {
	if err == nil {
		return false
	}
	var failure *source.Error
	if errors.As(err, &failure) {
		return failure.Kind == source.KindLogin || failure.Kind == source.KindPermission
	}
	return false
}

// runSignIn drives the interactive sign-in. Tests replace signIn to exercise
// supersede/cancel behavior without opening a browser.
func (m *model) runSignIn(ctx context.Context) (*source.Session, error) {
	if m.signIn != nil {
		return m.signIn(ctx)
	}
	return m.runSourceLogin(ctx)
}
