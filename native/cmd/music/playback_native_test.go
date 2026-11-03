//go:build darwin && cgo

package main

import (
	"github.com/vivaui/viva/ui"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
	"tinglan/native/internal/audio"
	"tinglan/native/internal/media"
	"tinglan/native/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.Run(m)) }

func TestBothProgressBarsAndSystemSeekUseMediaClock(t *testing.T) {
	dir := os.Getenv("HWMUSIC_AUDIO_FIXTURES")
	if dir == "" {
		t.Skip("synthetic fixtures required")
	}
	m := newModel()
	m.enableDemo(2)
	m.queue.Replace(m.tracks, 0)
	p, err := audio.OpenLocalFixture(filepath.Join(dir, "tone.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	m.player = p
	defer m.close()
	await := func(position float64) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			m.audioState = p.Snapshot()
			if m.audioState.Ready && m.audioState.Duration > 0 && (position < 0 || math.Abs(m.audioState.Position-position) < .1) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("seek not applied: %+v", m.audioState)
	}
	await(-1)
	size := ui.Size{Width: 1180, Height: 760}
	for _, stage := range []bool{false, true} {
		m.stageOpen = stage
		m.stageChanged = time.Time{}
		b := transportBounds(size, m.stageAmount(time.Now())).progress
		result := m.pointerEvent(ui.PointerEvent{Kind: ui.PointerDown, Target: progressID, X: b.X + b.Width*.5, Y: b.Y + 8, Viewport: size})
		if !result.Handled {
			t.Fatal("progress click ignored")
		}
		await(2)
		result = m.pointerEvent(ui.PointerEvent{Kind: ui.PointerMove, Captured: true, Target: progressID, X: b.X + b.Width*.75, Y: b.Y + 8, Viewport: size})
		if !result.Handled {
			t.Fatal("progress drag ignored")
		}
		await(3)
	}
	m.mediaEvents = []media.Event{{Command: 5, Position: 27}}
	m.applyMedia()
	await(4)
	if m.audioState.Position > m.audioState.Duration+.02 {
		t.Fatal("system seek exceeded actual duration")
	}
	m.mediaEvents = []media.Event{{Command: 5, Position: 1}}
	m.applyMedia()
	await(1)
	if m.audioState.Playing {
		t.Fatal("seeking resumed a paused song")
	}
}
