package main

import (
	"testing"

	"tinglan/native/internal/music"
)

// A preview stream is only worth a notice once per session: the resolver has
// already tried every configured source before falling back to an excerpt, so
// repeating the message on each track is noise.
func TestPreviewNoticeAppearsOncePerSession(t *testing.T) {
	m := &model{}
	first := playbackResultForTest(music.AccessPreview)
	m.announceAccess(first)
	if !m.previewNoticed || m.status == "" {
		t.Fatalf("first preview: noticed=%v status=%q", m.previewNoticed, m.status)
	}
	m.status = ""
	m.announceAccess(playbackResultForTest(music.AccessPreview))
	if m.status != "" {
		t.Fatalf("second preview repeated the notice: %q", m.status)
	}
	m.status = ""
	m.announceAccess(playbackResultForTest(music.AccessFull))
	if m.status != "" {
		t.Fatalf("full playback should stay silent: %q", m.status)
	}
}
