// Command sample-plugin is a minimal external source used for manual checks.
// It mirrors what a third-party adapter does: describe itself, answer a search
// and an import page, and leave login to the host when a session is needed.
//
// Build: go build -o bin/sample ./internal/plugin/testdata/sample-plugin
package main

import (
	"context"
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
	plugin := &sdk.Plugin{Source: fake, HostServices: []string{"log"}}
	if err := plugin.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		os.Exit(1)
	}
}
