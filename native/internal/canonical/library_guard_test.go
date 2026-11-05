package canonical

import (
	"context"
	"testing"

	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
	"tinglan/native/internal/source/sourcetest"
)

// An empty snapshot from a source that already has local tracks must fail the
// run instead of publishing a library with no bindings for that source.
func TestImportRejectsEmptySnapshotWithLocalTracks(t *testing.T) {
	lib := openForTest(t, t.TempDir())
	ctx := context.Background()
	track := music.Track{Source: music.SourceHuawei, ID: "tr-1", Title: "晴天", Artists: []music.Artist{{Name: "周杰伦"}}}
	if _, err := lib.Add(ctx, track, nil, music.MatchPolicy{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if got := len(lib.Snapshot()); got != 1 {
		t.Fatalf("seed rows = %d", got)
	}
	fake := &sourcetest.Fake{SourceID: music.SourceHuawei, Pages: []source.ImportPage{{}}}
	if _, err := lib.Import(ctx, fake, source.Collection{Kind: "favorites"}, nil, music.MatchPolicy{}, nil); err == nil {
		t.Fatal("empty snapshot accepted")
	}
	rows := lib.Snapshot()
	if len(rows) != 1 || len(rows[0].Links) == 0 {
		t.Fatalf("local tracks changed: %d rows", len(rows))
	}
}
