package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tinglan/native/internal/audio"
	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
	"tinglan/native/internal/storage"
)

type testLogger struct{ t *testing.T }

func (l testLogger) Printf(format string, args ...any) { l.t.Logf(format, args...) }

// TestLiveHuaweiPlugin exercises the bundled Huawei plugin against the real
// application profile. It only runs when TINGLAN_LIVE_PROFILE points at a
// project's data directory, so CI and unit runs stay offline.
func TestLiveHuaweiPlugin(t *testing.T) {
	profile := os.Getenv("TINGLAN_LIVE_PROFILE")
	if profile == "" {
		t.Skip("set TINGLAN_LIVE_PROFILE to the app data directory")
	}
	appBundle := os.Getenv("TINGLAN_LIVE_PLUGINS")
	if appBundle == "" {
		appBundle = filepath.Join("..", "..", "..", "out", "native", "Tinglan.app", "Contents", "Resources", "sources")
	}
	manager := NewManager(filepath.Join(profile, "sources"), storage.New(profile), testLogger{t})
	manager.AddRoot(appBundle)
	defer manager.Close()
	registry := source.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if failures := manager.LoadEnabled(ctx, registry); len(failures) > 0 {
		t.Fatalf("load: %v", failures)
	}
	src, ok := registry.Source("huawei")
	if !ok {
		t.Fatal("huawei plugin not registered")
	}
	status, err := src.Auth().Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.State != source.AuthSignedIn {
		t.Fatalf("status = %+v", status)
	}
	page, err := src.Catalog().Search(ctx, source.SearchRequest{Query: "周杰伦", Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatal("search returned nothing")
	}
	playlists, err := src.Catalog().Playlists(ctx, source.PlaylistRequest{Limit: 5})
	if err != nil {
		t.Fatalf("playlists: %v", err)
	}
	t.Logf("account=%s search=%d playlists=%d", status.AccountKey, len(page.Items), len(playlists.Items))
	if len(page.Items) == 0 {
		t.Fatal("no search results")
	}
	playback, err := src.Library().Resolve(ctx, source.ResolveRequest{Track: page.Items[0], Quality: "standard"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if playback.URL == "" {
		t.Fatal("empty playback url")
	}
	t.Logf("resolve host=%s access=%s", playback.URL[:min(len(playback.URL), 40)], playback.Access)
	if probe, err := audio.ProbeRemote(ctx, playback.URL); err != nil {
		t.Fatalf("probe resolved stream: %v", err)
	} else if player, err := probe.Open(); err != nil {
		t.Fatalf("open resolved stream: %v", err)
	} else {
		player.Close()
	}
	if len(playlists.Items) > 0 {
		detail, err := src.Catalog().PlaylistDetail(ctx, source.EntityRequest{Ref: source.EntityRef{Source: music.SourceHuawei, Kind: "playlist", ID: playlists.Items[0].ID}, Limit: 5})
		if err != nil {
			t.Fatalf("playlist detail: %v", err)
		}
		t.Logf("playlist %q tracks=%d truncated=%v", detail.Playlist.Name, len(detail.Tracks), detail.Truncated)
	}
	history, err := src.Catalog().History(ctx, source.PageRequest{Limit: 5})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	t.Logf("history=%d", len(history.Items))
}
