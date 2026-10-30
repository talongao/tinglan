package main

import (
	"context"
	"flag"
	"log"
	"os"
	"sync"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"

	"huawei-music-desktop/native/internal/audio"
	"huawei-music-desktop/native/internal/huawei"
	"huawei-music-desktop/native/internal/webauth"
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
	session    *huawei.Session
	err        error
	restored   bool
}
type model struct {
	desktopState
	writeState
	resourceState
	applicationState
	typeState
	browserState
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
	session            *huawei.Session
	authorize          func(context.Context, *huawei.Attempt) (string, error)
	exchange           func(context.Context, *huawei.Attempt) (*huawei.Session, error)
	validate           func(context.Context, *huawei.Session) error
	player             *audio.Player
	audioState         audio.State
	playbackGeneration uint64
	playbackCancel     context.CancelFunc
	playbackResults    []playbackResult
	resolving          bool
	actualQuality      string
	resolvedDuration   float64
	durationMismatch   bool
	pendingSeek        float64
	startedAt          time.Time
	lastQueueSave      time.Time
	listened           float64
	lastPoll           time.Time
	lastPosition       float64
	historyReported    bool
}

func newModel() *model {
	client := huawei.NewClient()
	m := &model{status: "请登录以读取个人音乐库。", authorize: webauth.Authorize, exchange: client.Exchange, validate: client.ValidateSession}
	m.initDesktop()
	if webauth.NeedsManualCallback() {
		m.authorize = m.browserAuthorize
	}
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
	m.cancel, m.busy, m.status = cancel, true, "请在系统浏览器完成华为授权"
	go func() {
		defer cancel()
		attempt := huawei.NewAttempt(time.Now())
		defer attempt.Cancel()
		var session *huawei.Session
		raw, err := m.authorize(ctx, attempt)
		if err == nil {
			err = attempt.Accept(raw, time.Now())
		}
		if err == nil {
			session, err = m.exchange(ctx, attempt)
		}
		if err == nil {
			err = m.validate(ctx, session)
		}
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
	m.status = "已取消登录"
}

func (m *model) wake() {
	m.applySessionChecks()
	m.applyPersistence()
	m.applyBrowser()
	m.applyMedia()
	if m.reopen.Swap(false) && m.window != nil {
		m.window.Show()
	}
	m.applyCovers()
	m.applyEntities()
	m.applyWrites()
	m.applyPlaybackResults()
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
		if result.err != nil {
			if result.restored && m.expireSession(result.err) {
				continue
			}
			m.status = "登录未完成，请重试；原会话保持不变"
			if result.restored {
				m.status = "请登录以读取个人音乐库。"
			}
			continue
		}
		m.session, m.status = result.session, "音乐会话校验通过 · 仅本次进程有效"
		if result.restored {
			m.status = "已恢复保存的音乐会话，正在更新音乐库。"
		}
		m.installCatalog()
		m.attachAccount()
		m.initWriter()
		m.scheduleSessionCheck()
		if !result.restored && m.store != nil {
			m.persistSession()
		}
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
	if m.player != nil {
		m.player.Close()
		m.player = nil
	}
}

func main() {
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
