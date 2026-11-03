package huawei

import "tinglan/native/internal/music"

// Track, Artist and Album stay as the provider's wire shape; the shell and the
// queue now speak music.*. These conversions keep the boundary explicit while
// the remaining call sites migrate.
func NeutralTrack(track Track) music.Track {
	out := music.Track{
		Source: music.SourceHuawei, ID: track.ID, Title: track.Title,
		CoverURL: track.CoverURL, CoverURLs: track.CoverURLs, Duration: track.Duration,
		OnlySource: music.SourceID(track.OnlySource), Collected: track.Collected,
	}
	for _, artist := range track.Artists {
		out.Artists = append(out.Artists, music.Artist{ID: artist.ID, Name: artist.Name})
	}
	if track.Album != nil {
		out.Album = &music.Album{ID: track.Album.ID, Title: track.Album.Title}
	}
	return out
}

func NeutralTracks(tracks []Track) []music.Track {
	out := make([]music.Track, 0, len(tracks))
	for _, track := range tracks {
		out = append(out, NeutralTrack(track))
	}
	return out
}

// ProviderTrack converts a neutral row back to the provider wire shape so the
// legacy client calls keep working during the migration.
func ProviderTrack(track music.Track) Track {
	out := Track{
		ID: track.ID, Title: track.Title, CoverURL: track.CoverURL,
		CoverURLs: track.CoverURLs, Duration: track.Duration,
		OnlySource: string(track.OnlySource), Collected: track.Collected,
	}
	for _, artist := range track.Artists {
		out.Artists = append(out.Artists, Artist{ID: artist.ID, Name: artist.Name})
	}
	if track.Album != nil {
		out.Album = &Album{ID: track.Album.ID, Title: track.Album.Title}
	}
	return out
}
