//go:build darwin && cgo

package main

import (
	"os"
	"testing"
	"time"
)

// Diagnostic: measures how long a real playback takes to produce sound.
// Requires HWMUSIC_LATENCY_PROFILE pointing at a profile copy with a session.
func TestPlaybackStartLatency(t *testing.T) {
	dir := os.Getenv("HWMUSIC_LATENCY_PROFILE")
	if dir == "" {
		t.Skip("HWMUSIC_LATENCY_PROFILE not set")
	}
	m := newModel()
	m.openProfile(dir)
	m.restoreSession()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		pending := m.pending
		m.pending = nil
		m.mu.Unlock()
		for _, r := range pending {
			if r.generation != m.generation || r.err != nil {
				continue
			}
			m.session = r.session
			m.installCatalog()
			m.attachAccount()
			m.initWriter()
		}
		m.mu.Lock()
		sess := m.session
		m.mu.Unlock()
		if sess != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	m.mu.Lock()
	sess := m.session
	m.mu.Unlock()
	if sess == nil {
		t.Fatal("session was not restored")
	}
	if m.queue.Current() == nil {
		t.Fatal("empty queue")
	}
	start := time.Now()
	m.playCurrent()
	last := time.Now()
	for time.Now().Before(deadline) {
		m.applyPlaybackResults()
		m.pollAudio()
		st := m.audioState
		if st.Playing && st.Position > 0 {
			t.Logf("AUDIO_STARTED after %v (pos %.2f, dur %.2f)", time.Since(start), st.Position, st.Duration)
			m.cancelPlayback()
			return
		}
		if time.Since(last) > 3*time.Second {
			last = time.Now()
			t.Logf("still waiting %v: ready=%v playing=%v pos=%.2f resolving=%v err=%q player=%v", time.Since(start), st.Ready, st.Playing, st.Position, m.resolving, m.playbackError, m.player != nil)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no audio after %v", time.Since(start))
}
