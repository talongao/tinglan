//go:build darwin && cgo

package audio

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tinglan/native/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.Run(m)) }

func await(t *testing.T, p *Player, condition func(State) bool) State {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		state := p.Snapshot()
		if state.Failed {
			t.Fatal("native decoding failed")
		}
		if condition(state) {
			return state
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("native playback timed out: %+v", p.Snapshot())
	return State{}
}

func TestNativeLocalPlayback(t *testing.T) {
	dir := os.Getenv("HWMUSIC_AUDIO_FIXTURES")
	if dir == "" {
		t.Skip("set HWMUSIC_AUDIO_FIXTURES to a synthetic tone.mp3/tone.flac directory")
	}
	for _, name := range []string{"tone.mp3", "tone.flac"} {
		t.Run(name, func(t *testing.T) {
			p, err := OpenLocalFixture(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			ready := await(t, p, func(s State) bool { return s.Ready && s.Duration > 3 })
			if ready.Playing || ready.Position > .1 {
				t.Fatal("started automatically")
			}
			if err := p.SetVolume(0); err != nil {
				t.Fatal(err)
			}
			p.Play()
			await(t, p, func(s State) bool { return s.Playing && s.Position > .2 })
			p.Pause()
			paused := await(t, p, func(s State) bool { return !s.Playing })
			time.Sleep(100 * time.Millisecond)
			if math.Abs(p.Snapshot().Position-paused.Position) > .08 {
				t.Fatal("pause did not stop position")
			}
			if err := p.Seek(ready.Duration - .4); err != nil {
				t.Fatal(err)
			}
			await(t, p, func(s State) bool { return s.Position >= ready.Duration-.5 })
			p.Play()
			await(t, p, func(s State) bool { return s.Ended })
			p.Close()
			p.Close()
			if p.Snapshot().Playing {
				t.Fatal("closed player still playing")
			}
		})
	}
}

func TestNativeSeekCannotExceedMediaDuration(t *testing.T) {
	dir := os.Getenv("HWMUSIC_AUDIO_FIXTURES")
	if dir == "" {
		t.Skip("synthetic fixtures required")
	}
	for _, name := range []string{"tone.mp3", "tone.flac"} {
		t.Run(name, func(t *testing.T) {
			p, err := OpenLocalFixture(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			ready := await(t, p, func(s State) bool { return s.Ready && s.Duration > 0 })
			if math.Abs(ready.Duration-4) > .1 {
				t.Fatalf("fixture duration wrong: %.3f", ready.Duration)
			}
			if err := p.Seek(ready.Duration + 23); err != nil {
				t.Fatal(err)
			}
			state := await(t, p, func(s State) bool { return s.Position >= ready.Duration-.1 })
			if state.Position > ready.Duration+.02 {
				t.Fatalf("seek escaped media: %+v", state)
			}
			if err := p.Seek(1); err != nil {
				t.Fatal(err)
			}
			await(t, p, func(s State) bool { return math.Abs(s.Position-1) < .1 && !s.Ended })
		})
	}
}
