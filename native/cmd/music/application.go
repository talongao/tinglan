package main

import (
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"time"
	"tinglan/native/internal/mainthread"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/app/iconpng"
	"github.com/vivaui/viva/app/singleinstance"
	"github.com/vivaui/viva/ui"
	"tinglan/native/internal/icons"
	"tinglan/native/internal/media"
)

type applicationState struct {
	backgroundCheck bool
	nativeReady     atomic.Bool
	updateQueued    atomic.Bool
	quit            func()
	window          *app.Window
	reopen          atomic.Bool
	mediaController *media.Controller
	mediaEvents     []media.Event
}

func (m *model) togglePlayback() {
	if m.player == nil {
		m.playCurrent()
		return
	}
	if m.audioRunning {
		m.player.Pause()
		m.audioState = m.player.Snapshot()
		m.queue.Position = m.audioState.Position
		m.audioRunning = false
		if m.playTimer != nil {
			m.playTimer.Stop()
		}
		m.saveQueue()
	} else {
		m.player.Play()
		m.audioRunning = true
		m.pollAudio()
	}
	m.requestUpdate()
	m.updateMedia()
}
func (m *model) stepPlayback(direction int) {
	if m.queue.Step(direction, false) {
		m.playCurrent()
		m.saveQueue()
		m.requestUpdate()
	}
}
func runMusic(m *model) error {
	if !m.demo && os.Getenv("VIVA_HEADLESS") == "" {
		claim, primary, err := singleinstance.SingleInstance("org.tinglan.unofficial.viva", nil, func([]string) { m.reopen.Store(true); m.requestUpdate() })
		if err != nil {
			return err
		}
		if !primary {
			return nil
		}
		defer claim.Close()
		m.restoreSession()
	}
	var canvas ui.Canvas
	return app.RunApplication(func(application *app.Application) error {
		m.quit = application.Quit
		cfg := config(m)
		icon, err := iconpng.Decode(icons.AppPNG)
		if err != nil {
			return err
		}
		cfg.Icon = icon
		var tray *app.StatusItem
		cfg.OnCloseRequest = func(allow func()) {
			if tray != nil && m.settings.CloseToTray {
				m.window.Hide()
			} else {
				allow()
			}
		}
		cfg.OnClosed = func() {
			if tray != nil {
				tray.Remove()
			}
			m.close()
		}
		m.window, err = application.OpenWindow(cfg, &canvas)
		if err != nil {
			return err
		}
		m.nativeReady.Store(runtime.GOOS == "darwin" && os.Getenv("VIVA_HEADLESS") == "")
		if m.backgroundCheck {
			time.AfterFunc(300*time.Millisecond, func() { mainthread.Post(func() { m.window.Hide(); m.startTracks(m.tracks, 0) }) })
			time.AfterFunc(12*time.Second, func() {
				mainthread.Post(func() {
					if !m.closed {
						fmt.Printf("BACKGROUND_CHECK_TIMEOUT hidden=%v index=%d running=%v state=%+v\n", m.window.Hidden(), m.queue.Index, m.audioRunning, m.audioState)
						os.Exit(2)
					}
				})
			})
		}

		if os.Getenv("VIVA_HEADLESS") != "" {
			return nil
		}
		if !m.demo {
			m.mediaController = media.New(func(e media.Event) {
				m.mu.Lock()
				if !m.closed {
					m.mediaEvents = append(m.mediaEvents, e)
				}
				m.mu.Unlock()
				m.requestUpdate()
			})
		}
		trayIcon, err := iconpng.Decode(icons.TrayPNG)
		if err != nil {
			return err
		}
		tray, _ = application.AddStatusItem(app.StatusItemConfig{Tooltip: appDisplayName, Icon: app.StatusIcon{Size: trayIcon.Size, RGBA: trayIcon.RGBA, Template: true}, OnClick: func(app.StatusClick) { m.window.Show() }, Menu: func() []app.MenuItem {
			return []app.MenuItem{{Title: "显示主窗口", OnSelect: m.window.Show}, {Title: "播放 / 暂停", OnSelect: m.togglePlayback}, {Title: "上一首", OnSelect: func() { m.stepPlayback(-1) }}, {Title: "下一首", OnSelect: func() { m.stepPlayback(1) }}, {Separator: true}, {Title: "退出", OnSelect: application.Quit}}
		}})
		return nil
	})
}

func (m *model) applyMedia() {
	m.mu.Lock()
	events := m.mediaEvents
	m.mediaEvents = nil
	m.mu.Unlock()
	for _, e := range events {
		switch e.Command {
		case 0:
			m.togglePlayback()
		case 1:
			if !m.audioRunning {
				m.togglePlayback()
			}
		case 2:
			if m.audioRunning {
				m.togglePlayback()
			}
		case 3:
			m.stepPlayback(-1)
		case 4:
			m.stepPlayback(1)
		case 5:
			if m.player != nil {
				m.seekPlayback(e.Position)
			}
		case 8:
			m.settings.Volume = max(0, min(1, e.Position))
			m.saveSettings()
		case 9:
			m.settings.Shuffle = e.Position != 0
			m.queue.SetShuffle(m.settings.Shuffle)
			m.saveSettings()
		case 10:
			if e.Position >= 0 && e.Position <= 2 {
				m.settings.RepeatMode = []string{"off", "all", "one"}[int(e.Position)]
				m.settings.Shuffle = false
				m.queue.SetShuffle(false)
				m.saveSettings()
			}
		case 6:
			if m.window != nil {
				m.window.Show()
			}
		case 7:
			if m.quit != nil {
				m.quit()
			}
		}
	}
}
func (m *model) updateMedia() {
	if m.mediaController == nil {
		return
	}
	m.mediaController.Preferences(m.settings.Volume, m.settings.Shuffle, m.settings.RepeatMode)
	if t := m.queue.Current(); t != nil {
		position, duration := m.playbackTimes()
		m.mediaController.Update(t.Title, t.ArtistText(), duration, position, m.audioState.Playing)
	} else {
		m.mediaController.Update("", "", 0, 0, false)
	}
}

// Playback and pending network results must be drained even when no drawable
// is available. All model mutation remains on the native main thread.
func (m *model) requestUpdate() {
	if !m.nativeReady.Load() {
		m.invalidator.Invalidate()
		return
	}
	if m.updateQueued.Swap(true) {
		return
	}
	mainthread.Post(func() {
		m.updateQueued.Store(false)
		m.mu.Lock()
		closed := m.closed
		m.mu.Unlock()
		if closed {
			return
		}
		if m.window != nil && (m.window.Hidden() || m.window.Minimized()) {
			m.wake()

			if m.backgroundCheck && m.queue.Index == 1 && m.audioState.Playing {
				fmt.Println("BACKGROUND_CHECK_OK: hidden window advanced to track 2")
				m.quit()
			}
			return
		}
		m.invalidator.Invalidate()
	})
}
