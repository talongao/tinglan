package main

import (
	"github.com/vivaui/viva/ui"
	"testing"
	"time"
	"tinglan/native/internal/audio"
)

func TestAudioDurationWinsOverCatalogAndResolved(t *testing.T) {
	m := newModel()
	m.enableDemo(2)
	m.queue.Replace(m.tracks, 0)
	catalog := 262.0
	m.queue.Current().Duration = &catalog
	m.resolvedDuration = 263
	m.audioState = audio.State{Ready: true, Duration: 300, Position: 285}
	m.queue.Position = 285
	position, duration := m.playbackTimes()
	if position != 285 || duration != 300 {
		t.Fatalf("mixed clocks: %v/%v", position, duration)
	}
	m.checkDuration()
	if !m.durationMismatch {
		t.Fatal("discrepancy not recorded")
	}
	m.audioState.Duration = 262
	position, duration = m.playbackTimes()
	if position != 285 || duration != 262 {
		t.Fatal("duration discrepancy was hidden by clipping elapsed time")
	}
}
func TestTransportMotionAndExclusiveMode(t *testing.T) {
	m := newModel()
	size := ui.Size{Width: 960, Height: 640}
	a, b, c := transportBounds(size, 0), transportBounds(size, .5), transportBounds(size, 1)
	if a != b || b != c {
		t.Fatal("transport moved between views")
	}
	m.setStage(true)
	start := m.stageChanged
	if m.stageAmount(start) != 0 || m.stageAmount(start.Add(160*time.Millisecond)) != .5 || m.stageAmount(start.Add(time.Second)) != 1 {
		t.Fatal("transition is not continuous")
	}
	for _, want := range []string{"列表循环", "单曲循环", "随机", "顺序"} {
		m.cyclePlayMode()
		if m.playMode() != want || (m.settings.Shuffle && m.settings.RepeatMode != "off") {
			t.Fatal("play modes not exclusive")
		}
	}
	m.queue.Replace(syntheticTracks(965), 700)
	m.centerQueue(size)
	if m.queueScroll <= 0 || m.queueScroll > 700*48 {
		t.Fatal("queue did not reveal paused current track")
	}
}
