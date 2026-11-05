// Package sourcetest provides a scriptable Source for tests in packages that
// consume the contract. It is not used by production code.
package sourcetest

import (
	"context"

	"tinglan/native/internal/lyrics"
	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
)

// Fake is a Source with settable behaviour. Pages are returned in order.
type Fake struct {
	SourceID   music.SourceID
	Desc       source.Descriptor
	AuthStatus source.AuthStatus
	AuthErr    error
	Pages      []source.ImportPage
	ImportErr  error
	SearchErr  error
	Playback   music.Playback
	ResolveErr error
	// SearchResults feeds Catalog().Search.
	SearchResults []music.Track
	// Calls records import cursors in order.
	Calls []string
	// ResolveCalls records the resolved track ids in order.
	ResolveCalls []string

	calls int
}

func (f *Fake) ID() music.SourceID { return f.SourceID }

func (f *Fake) Describe() source.Descriptor {
	if f.Desc.ID == "" {
		f.Desc = source.Descriptor{
			SchemaVersion: 1,
			ID:            f.SourceID,
			Name:          string(f.SourceID),
			Capabilities: source.Capabilities{
				Import: true, Resolve: true, Search: true,
				Collections: []source.Collection{{Kind: "favorites", Name: "我的收藏"}},
				MaxPageSize: 50,
			},
		}
	}
	return f.Desc
}

func (f *Fake) Auth() source.AuthSource       { return fakeAuth{f} }
func (f *Fake) Library() source.LibrarySource { return fakeLibrary{f} }
func (f *Fake) Catalog() source.CatalogSource { return fakeCatalog{f} }
func (f *Fake) Lyrics() source.LyricsSource   { return fakeLyrics{} }
func (f *Fake) Manage() source.ManageSource   { return fakeManage{} }

type fakeAuth struct{ f *Fake }

func (a fakeAuth) Status(context.Context) (source.AuthStatus, error) {
	status := a.f.AuthStatus
	if status.State == "" {
		status.State = source.AuthSignedIn
	}
	return status, a.f.AuthErr
}

func (a fakeAuth) Verify(ctx context.Context) (source.AuthStatus, error) { return a.Status(ctx) }

func (a fakeAuth) Login(context.Context, source.LoginRequest) (source.AuthResult, error) {
	return source.AuthResult{Succeeded: true, Persisted: true}, nil
}

func (a fakeAuth) PersistSession(context.Context, source.PersistRequest) (bool, error) {
	return true, nil
}

func (a fakeAuth) Logout(context.Context) error { return nil }

type fakeLibrary struct{ f *Fake }

func (l fakeLibrary) Import(_ context.Context, request source.ImportRequest) (source.ImportPage, error) {
	l.f.Calls = append(l.f.Calls, request.Cursor)
	if l.f.ImportErr != nil {
		return source.ImportPage{}, l.f.ImportErr
	}
	if l.f.calls >= len(l.f.Pages) {
		return source.ImportPage{}, nil
	}
	page := l.f.Pages[l.f.calls]
	l.f.calls++
	return page, nil
}

func (l fakeLibrary) Resolve(_ context.Context, request source.ResolveRequest) (music.Playback, error) {
	l.f.ResolveCalls = append(l.f.ResolveCalls, request.Track.ID)
	return l.f.Playback, l.f.ResolveErr
}

type fakeCatalog struct{ f *Fake }

func (c fakeCatalog) Search(context.Context, source.SearchRequest) (source.Page[music.Track], error) {
	if c.f.SearchErr != nil {
		return source.Page[music.Track]{}, c.f.SearchErr
	}
	if len(c.f.SearchResults) == 0 {
		return source.Page[music.Track]{}, nil
	}
	return source.Page[music.Track]{Items: c.f.SearchResults}, nil
}
func (fakeCatalog) Playlists(context.Context, source.PlaylistRequest) (source.Page[music.Playlist], error) {
	return source.Page[music.Playlist]{}, nil
}
func (fakeCatalog) PlaylistDetail(context.Context, source.EntityRequest) (source.PlaylistDetail, error) {
	return source.PlaylistDetail{}, nil
}
func (fakeCatalog) AlbumDetail(context.Context, source.EntityRequest) (source.AlbumDetail, error) {
	return source.AlbumDetail{}, nil
}
func (fakeCatalog) ArtistDetail(context.Context, source.EntityRequest) (source.ArtistDetail, error) {
	return source.ArtistDetail{}, nil
}
func (fakeCatalog) History(context.Context, source.PageRequest) (source.Page[music.Track], error) {
	return source.Page[music.Track]{}, nil
}

type fakeLyrics struct{}

func (fakeLyrics) Lyrics(context.Context, source.TrackRef) ([]lyrics.Line, error) {
	return nil, source.NewError("", "lyrics", source.KindUnsupported, nil)
}

type fakeManage struct{}

func (fakeManage) ReportHistory(context.Context, music.Track, int) error { return nil }
func (fakeManage) UpdatePlaylistDescription(context.Context, source.EntityRef, string) error {
	return nil
}
func (fakeManage) DeletePlaylist(context.Context, source.EntityRef) error { return nil }
