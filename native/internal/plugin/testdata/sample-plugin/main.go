// Command sample-plugin is a minimal external source used for manual checks.
// It mirrors what a third-party adapter does: describe itself, answer a search
// and an import page, and leave login to the host when a session is needed.
//
// Build: go build -o bin/sample ./internal/plugin/testdata/sample-plugin
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"tinglan/native/internal/music"
	"tinglan/native/internal/plugin/sdk"
	"tinglan/native/internal/source"
	"tinglan/native/internal/source/sourcetest"
)

func main() {
	fake := &sourcetest.Fake{
		SourceID: "sample",
		Desc: source.Descriptor{
			SchemaVersion: 1,
			ID:            "sample",
			Name:          "示例音源",
			ShortName:     "示例",
			Accent:        "#2F6FEB",
			Capabilities: source.Capabilities{
				Search:      true,
				Playlist:    true,
				Import:      true,
				Resolve:     true,
				MaxPageSize: 20,
				LoginKinds:  []source.LoginKind{source.LoginWebView},
				Collections: []source.Collection{{Kind: "favorites", Name: "示例收藏"}},
			},
			ResourceDomains: []string{"*.example.com"},
		},
		AuthStatus: source.AuthStatus{State: source.AuthSignedOut},
		SearchResults: []music.Track{
			{ID: "sample-1", Source: "sample", Title: "示例歌曲 · 第一首", Artists: []music.Artist{{Name: "示例艺人"}}, Album: &music.Album{Title: "示例专辑"}},
			{ID: "sample-2", Source: "sample", Title: "示例歌曲 · 第二首", Artists: []music.Artist{{Name: "示例艺人"}}, Album: &music.Album{Title: "示例专辑"}},
		},
		Pages: []source.ImportPage{{
			Tracks: []music.Track{
				{ID: "sample-1", Source: "sample", Title: "示例歌曲 · 第一首", Artists: []music.Artist{{Name: "示例艺人"}}, Album: &music.Album{Title: "示例专辑"}},
				{ID: "sample-2", Source: "sample", Title: "示例歌曲 · 第二首", Artists: []music.Artist{{Name: "示例艺人"}}, Album: &music.Album{Title: "示例专辑"}},
			},
		}},
	}
	auth := &demoAuth{source: fake}
	plugin := &sdk.Plugin{Source: fake, Auth: auth, HostServices: []string{"session", "log"}}
	plugin.OnReady = func(host *sdk.Host) { auth.host = host }
	if err := plugin.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		os.Exit(1)
	}
}

// demoAuth keeps a fake login state in the host session namespace so the shell
// can exercise the per-source login UI without a real account.
type demoAuth struct {
	source *sourcetest.Fake
	host   *sdk.Host
}

func (a *demoAuth) Status(ctx context.Context) (source.AuthStatus, error) {
	if a.host == nil {
		return source.AuthStatus{State: source.AuthSignedOut}, nil
	}
	value, _, err := a.host.SessionGet(ctx, "session")
	if err != nil || value == "" {
		return source.AuthStatus{State: source.AuthSignedOut}, nil
	}
	return source.AuthStatus{State: source.AuthSignedIn, AccountKey: "sample-account", DisplayName: "示例账号"}, nil
}

func (a *demoAuth) Verify(ctx context.Context) (source.AuthStatus, error) { return a.Status(ctx) }

func (a *demoAuth) Login(ctx context.Context, request source.LoginRequest) (source.AuthResult, error) {
	value := base64.StdEncoding.EncodeToString([]byte("sample-session"))
	_, err := a.host.SessionSet(ctx, "session", value, 0, request.WriteToken)
	if err != nil {
		return source.AuthResult{}, err
	}
	return source.AuthResult{
		Succeeded: true,
		Persisted: true,
		Status:    source.AuthStatus{State: source.AuthSignedIn, AccountKey: "sample-account", DisplayName: "示例账号"},
	}, nil
}

func (a *demoAuth) PersistSession(context.Context, source.PersistRequest) (bool, error) {
	return true, nil
}

func (a *demoAuth) Logout(ctx context.Context) error {
	if a.host == nil {
		return nil
	}
	if err := a.host.SessionDelete(ctx, "session", ""); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}
