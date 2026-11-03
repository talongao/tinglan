package librarydb

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"tinglan/native/internal/music"
)

func TestProbeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx := context.Background()
	dur := 210.5
	tr := music.Track{Source: music.SourceHuawei, ID: "hw1", Title: "晴天", Artists: []music.Artist{{Name: "周杰伦"}}, Album: &music.Album{Title: "叶惠美"}, Duration: &dur}
	row := music.NewCanonical(tr, 1, time.Now())
	if err := db.SaveRow(ctx, DefaultProfile, row, true, 0); err != nil { t.Fatal(err) }
	rows, seq, err := db.Load(ctx, DefaultProfile)
	if err != nil { t.Fatal(err) }
	if len(rows) != 1 || seq != 0 { t.Fatalf("rows=%d seq=%d", len(rows), seq) }
	if rows[0].Track.Title != "晴天" || len(rows[0].Track.Links) != 1 { t.Fatalf("%+v", rows[0]) }
	if rows[0].Track.Links[0].Disabled { t.Fatal("disabled") }
	if err := db.RemoveFavorite(ctx, DefaultProfile, row.ID); err != nil { t.Fatal(err) }
	rows, _, _ = db.Load(ctx, DefaultProfile)
	if len(rows) != 1 || rows[0].Favorite { t.Fatalf("favorite should be gone: %+v", rows[0]) }
	if err := db.DeleteTrack(ctx, DefaultProfile, row.ID); err != nil { t.Fatal(err) }
	rows, _, _ = db.Load(ctx, DefaultProfile)
	if len(rows) != 0 { t.Fatalf("row not deleted") }
	_ = filepath.Join(dir, FileName)
}
