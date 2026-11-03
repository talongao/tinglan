package main

import (
	"testing"

	"tinglan/native/internal/huawei"
)

// pollAudio keeps queue.Position at the live playhead, so when a new track is
// started the queue still carries the previous track's position. That offset
// must not become the new track's pending seek: on a short track it clamps to
// the duration (the song appears to end immediately) and on a long one it
// forces a remote seek before audio starts.
func TestPlayCurrentIgnoresStaleQueuePosition(t *testing.T) {
	m := newModel()
	m.enableDemo(3)
	m.queue.Replace(m.tracks, 0)
	m.queue.Position = 261.4
	m.resumeTrackID = ""
	m.resumePosition = 0

	m.playCurrent()

	if m.pendingSeek != 0 {
		t.Fatalf("pendingSeek = %v, want 0 for a freshly started track", m.pendingSeek)
	}
	if m.queue.Position != 0 {
		t.Fatalf("queue position = %v, want 0", m.queue.Position)
	}
}

// A queue restored from disk resumes only the track it was saved on.
func TestPlayCurrentResumesRestoredTrackOnly(t *testing.T) {
	m := newModel()
	m.enableDemo(3)
	m.queue.Replace(m.tracks, 1)
	m.rememberResume(42)
	m.queue.Position = 42

	m.playCurrent()
	if m.pendingSeek != 42 {
		t.Fatalf("pendingSeek = %v, want the restored 42", m.pendingSeek)
	}
	if m.resumeTrackID != "" || m.resumePosition != 0 {
		t.Fatal("resume point was not cleared after use")
	}

	// Starting a different track afterwards must not inherit the offset.
	other := m.tracks[2]
	m.queue.Replace([]huawei.Track{other}, 0)
	m.queue.Position = 42
	m.playCurrent()
	if m.pendingSeek != 0 {
		t.Fatalf("pendingSeek = %v, want 0 for a different track", m.pendingSeek)
	}
}
