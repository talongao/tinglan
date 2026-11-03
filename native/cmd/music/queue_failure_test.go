package main

import (
	"testing"

	"tinglan/native/internal/music"
	"tinglan/native/internal/storage"
)

func enabledSourceSettings(pairs ...any) []storage.SourceSetting {
	out := make([]storage.SourceSetting, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, storage.SourceSetting{ID: pairs[i].(string), Enabled: pairs[i+1].(bool)})
	}
	return out
}

// A dead track must walk to the next queue entry instead of parking the
// transport, but the walk has to terminate when the whole queue is broken.
func TestSkipUnplayableAdvancesAndStops(t *testing.T) {
	m := &model{}
	m.demo = true
	m.queue.Replace([]music.Track{{ID: "a", Title: "甲"}, {ID: "b", Title: "乙"}, {ID: "c", Title: "丙"}}, 0)

	for i := 1; i <= 2; i++ {
		if !m.skipUnplayable() {
			t.Fatalf("skip %d did not advance", i)
		}
		if got := m.queue.Current().ID; got != []string{"b", "c"}[i-1] {
			t.Fatalf("skip %d current = %q", i, got)
		}
	}
	if m.skipUnplayable() {
		t.Fatal("skip walked past every entry")
	}
	if m.playFailures != 2 {
		t.Fatalf("playFailures = %d, want 2", m.playFailures)
	}
}

func TestSkipUnplayableNeedsMoreThanOneTrack(t *testing.T) {
	m := &model{}
	m.demo = true
	m.queue.Replace([]music.Track{{ID: "only"}}, 0)
	if m.skipUnplayable() {
		t.Fatal("single-track queue advanced")
	}
}

func TestTrackAvailableFollowsEnabledSources(t *testing.T) {
	m := &model{rowSources: map[string][]music.SourceID{"t1": {music.SourceHuawei, music.SourceID("sample")}}}
	m.settings.Sources = enabledSourceSettings("huawei", false, "sample", false)
	if m.trackAvailable(music.Track{ID: "t1"}) {
		t.Fatal("track with no enabled source reported available")
	}
	m.settings.Sources = enabledSourceSettings("huawei", true, "sample", false)
	if !m.trackAvailable(music.Track{ID: "t1"}) {
		t.Fatal("track lost its enabled source")
	}
	if !m.trackAvailable(music.Track{ID: "transient", Source: music.SourceHuawei}) {
		t.Fatal("transient row availability should follow its own source")
	}
}
