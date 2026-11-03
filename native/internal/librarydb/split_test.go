package librarydb

import (
	"context"
	"testing"
	"time"

	"tinglan/native/internal/music"
)

func TestSplitIncompatibleMerges(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	studio := music.Track{Source: music.SourceHuawei, ID: "h1", Title: "好心分手", Artists: []music.Artist{{Name: "卢巧音"}}}
	studio.Version = music.DetectVersion(studio.Title)
	chorus := music.Track{Source: music.SourceHuawei, ID: "h2", Title: "好心分手(合唱版)", Artists: []music.Artist{{Name: "卢巧音/王力宏"}}}
	chorus.Version = music.DetectVersion(chorus.Title)
	row := music.NewCanonical(studio, 1, now)
	row.Links = []music.SourceLink{music.Link(studio, 1, now), music.Link(chorus, 2, now)}
	if err := db.SaveRow(ctx, DefaultProfile, row, true, 0); err != nil {
		t.Fatal(err)
	}
	n, err := db.SplitIncompatibleMerges(ctx, DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("splits = %d, want 1", n)
	}
	rows, _, err := db.Load(ctx, DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	versions := map[string]int{}
	for _, r := range rows {
		versions[r.Track.Version.Key()]++
	}
	if versions["studio"] != 1 || versions["chorus"] != 1 {
		t.Fatalf("versions = %v", versions)
	}
}
