package huawei

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"tinglan/native/internal/lyrics"
	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
)

// Describe returns the immutable display identity. Names and colours live here
// so the UI never hard-codes a platform.
func (a *SourceAdapter) Describe() source.Descriptor {
	return source.Descriptor{
		SchemaVersion: 1,
		ID:            music.SourceHuawei,
		Name:          "华为音乐",
		ShortName:     "华",
		Accent:        "#C7000B",
		Capabilities:  a.Capabilities(),
		ResourceDomains: []string{
			".dbankcdn.cn", ".tingmall.com", ".kuke.com",
		},
	}
}

// Auth returns the account surface. Status stays local; Verify is the only
// method here that may perform a network call.
func (a *SourceAdapter) Auth() source.AuthSource { return huaweiAuth{a: a} }

// Library is the adapter itself: Import and Resolve are already implemented.
func (a *SourceAdapter) Library() source.LibrarySource { return a }

// Catalog exposes the provider catalogue through neutral types.
func (a *SourceAdapter) Catalog() source.CatalogSource { return huaweiCatalog{a: a} }

// Lyrics is not migrated yet; the UI still reads lyrics through its own path.
func (a *SourceAdapter) Lyrics() source.LyricsSource { return huaweiLyrics{} }

// Manage exposes the provider writes the shell still performs: listening
// reports and edits to an owned remote playlist.
func (a *SourceAdapter) Manage() source.ManageSource { return huaweiManage{a: a} }

type huaweiAuth struct{ a *SourceAdapter }

func (h huaweiAuth) Status(context.Context) (source.AuthStatus, error) {
	session := h.a.currentSession()
	if session == nil || session.token == "" {
		return source.AuthStatus{State: source.AuthSignedOut}, nil
	}
	if session.snsUserID == "" {
		return source.AuthStatus{State: source.AuthUnknown}, nil
	}
	return source.AuthStatus{
		State:       source.AuthSignedIn,
		AccountKey:  session.AccountKey(),
		DisplayName: "华为音乐账号",
	}, nil
}

func (h huaweiAuth) Verify(ctx context.Context) (source.AuthStatus, error) {
	session := h.a.currentSession()
	if session == nil || session.token == "" {
		return source.AuthStatus{State: source.AuthSignedOut}, nil
	}
	if err := h.a.client.ValidateSession(ctx, session); err != nil {
		kind := source.KindOf(classify("verify", err))
		switch kind {
		case source.KindLogin, source.KindPermission:
			return source.AuthStatus{State: source.AuthExpired}, nil
		default:
			// A network failure proves nothing about the session, so the state
			// stays unknown instead of flipping to expired.
			return source.AuthStatus{State: source.AuthUnknown, AccountKey: session.AccountKey()}, nil
		}
	}
	return source.AuthStatus{
		State:          source.AuthSignedIn,
		AccountKey:     session.AccountKey(),
		DisplayName:    "华为音乐账号",
		LastVerifiedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (h huaweiAuth) Login(context.Context, source.LoginRequest) (source.AuthResult, error) {
	return source.AuthResult{}, source.NewError(music.SourceHuawei, "login", source.KindUnsupported,
		errors.New("webview login is still driven by the desktop shell"))
}

func (h huaweiAuth) PersistSession(context.Context, source.PersistRequest) (bool, error) {
	// The Huawei adapter keeps no in-memory-only session today: the desktop
	// shell persists it before handing it over, so there is nothing to retry.
	return false, source.NewError(music.SourceHuawei, "persistSession", source.KindUnsupported,
		errors.New("no pending session to persist"))
}

func (h huaweiAuth) Logout(context.Context) error { return nil }

type huaweiCatalog struct{ a *SourceAdapter }

func (c huaweiCatalog) Search(ctx context.Context, request source.SearchRequest) (source.Page[music.Track], error) {
	start, limit, err := cursorStart(request.Cursor, request.Limit)
	if err != nil {
		return source.Page[music.Track]{}, source.NewError(music.SourceHuawei, "search", source.KindProtocol, err)
	}
	page, err := c.a.client.Search(ctx, request.Query, start, limit)
	if err != nil {
		return source.Page[music.Track]{}, classify("search", err)
	}
	return source.Page[music.Track]{
		Items:   musicTracks(page.Tracks),
		Next:    nextCursor(page.More, page.Next),
		HasMore: page.More,
	}, nil
}

func (c huaweiCatalog) Playlists(ctx context.Context, request source.PlaylistRequest) (source.Page[music.Playlist], error) {
	session := c.a.currentSession()
	if session == nil {
		return source.Page[music.Playlist]{}, source.NewError(music.SourceHuawei, "playlists", source.KindLogin, errors.New("music session missing"))
	}
	playlists, err := c.a.client.Playlists(ctx, session)
	if err != nil {
		return source.Page[music.Playlist]{}, classify("playlists", err)
	}
	out := make([]music.Playlist, 0, len(playlists))
	for _, p := range playlists {
		out = append(out, music.Playlist{
			Source:      music.SourceHuawei,
			ID:          p.ID,
			Kind:        music.PlaylistKind(strings.ToLower(p.Kind)),
			Name:        p.Title,
			Description: p.Description,
			CoverURL:    p.CoverURL,
		})
	}
	// The provider returns the whole list in one response.
	return source.Page[music.Playlist]{Items: out}, nil
}

func (c huaweiCatalog) PlaylistDetail(ctx context.Context, request source.EntityRequest) (source.PlaylistDetail, error) {
	session := c.a.currentSession()
	if session == nil {
		return source.PlaylistDetail{}, source.NewError(music.SourceHuawei, "playlistDetail", source.KindLogin, errors.New("music session missing"))
	}
	start, limit, err := cursorStart(request.Cursor, request.Limit)
	if err != nil {
		return source.PlaylistDetail{}, source.NewError(music.SourceHuawei, "playlistDetail", source.KindProtocol, err)
	}
	kind := string(request.Ref.PlaylistKind)
	if kind == "" {
		kind = "owned"
	}
	page, err := c.a.client.PlaylistDetail(ctx, session, Playlist{ID: request.Ref.ID, Kind: kind}, start, limit)
	if err != nil {
		return source.PlaylistDetail{}, classify("playlistDetail", err)
	}
	return source.PlaylistDetail{
		Playlist: music.Playlist{
			Source: music.SourceHuawei,
			ID:     request.Ref.ID,
			Kind:   request.Ref.PlaylistKind,
		},
		Tracks:  musicTracks(page.Tracks),
		Next:    nextCursor(page.More, page.Next),
		HasMore: page.More,
	}, nil
}

func (c huaweiCatalog) AlbumDetail(ctx context.Context, request source.EntityRequest) (source.AlbumDetail, error) {
	start, limit, err := cursorStart(request.Cursor, request.Limit)
	if err != nil {
		return source.AlbumDetail{}, source.NewError(music.SourceHuawei, "albumDetail", source.KindProtocol, err)
	}
	detail, err := c.a.client.AlbumDetail(ctx, request.Ref.ID, start, limit)
	if err != nil {
		return source.AlbumDetail{}, classify("albumDetail", err)
	}
	return source.AlbumDetail{
		Album:   music.Album{ID: detail.ID, Title: detail.Title, CoverURL: detail.CoverURL},
		Tracks:  musicTracks(detail.Tracks),
		Next:    nextCursor(detail.More, detail.Next),
		HasMore: detail.More,
	}, nil
}

func (c huaweiCatalog) ArtistDetail(ctx context.Context, request source.EntityRequest) (source.ArtistDetail, error) {
	detail, err := c.a.client.ArtistDetail(ctx, request.Ref.ID)
	if err != nil {
		return source.ArtistDetail{}, classify("artistDetail", err)
	}
	out := source.ArtistDetail{
		Artist: music.Artist{ID: detail.ID, Name: detail.Title, CoverURL: detail.CoverURL},
		// The provider returns a finite snapshot for artists: no cursor, so it
		// must be declared truncated instead of pretending completeness.
		Tracks:    musicTracks(detail.Tracks),
		Truncated: true,
	}
	for _, album := range detail.Albums {
		out.Albums = append(out.Albums, music.Album{ID: album.ID, Title: album.Title})
	}
	return out, nil
}

func (c huaweiCatalog) History(ctx context.Context, request source.PageRequest) (source.Page[music.Track], error) {
	session := c.a.currentSession()
	if session == nil {
		return source.Page[music.Track]{}, source.NewError(music.SourceHuawei, "history", source.KindLogin, errors.New("music session missing"))
	}
	tracks, err := c.a.client.History(ctx, session)
	if err != nil {
		return source.Page[music.Track]{}, classify("history", err)
	}
	return source.Page[music.Track]{Items: musicTracks(tracks)}, nil
}

type huaweiLyrics struct{}

func (huaweiLyrics) Lyrics(context.Context, source.TrackRef) ([]lyrics.Line, error) {
	return nil, source.NewError(music.SourceHuawei, "lyrics", source.KindUnsupported, errors.New("lyrics still read through the desktop shell"))
}

func musicTracks(in []Track) []music.Track {
	out := make([]music.Track, 0, len(in))
	for _, track := range in {
		out = append(out, MusicTrack(track))
	}
	return out
}

func cursorStart(cursor string, limit int) (int, int, error) {
	start := 0
	if cursor != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil || parsed < 0 || parsed > 1_000_000 {
			return 0, 0, errors.New("invalid cursor")
		}
		start = parsed
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 50 {
		limit = 50
	}
	return start, limit, nil
}

func nextCursor(more bool, next int) string {
	if !more {
		return ""
	}
	return strconv.Itoa(next)
}

var _ source.Source = (*SourceAdapter)(nil)

type huaweiManage struct{ a *SourceAdapter }

func (h huaweiManage) ReportHistory(ctx context.Context, track music.Track, listened int) error {
	writer := h.a.ensureWriter()
	if writer == nil {
		return source.NewError(music.SourceHuawei, "reportHistory", source.KindLogin, errors.New("music session missing"))
	}
	return writer.ReportHistory(ctx, ProviderTrack(track), listened)
}

func (h huaweiManage) UpdatePlaylistDescription(ctx context.Context, ref source.EntityRef, description string) error {
	writer := h.a.ensureWriter()
	if writer == nil {
		return source.NewError(music.SourceHuawei, "reportHistory", source.KindLogin, errors.New("music session missing"))
	}
	return writer.UpdateDescription(ctx, ref.ID, description)
}

func (h huaweiManage) DeletePlaylist(ctx context.Context, ref source.EntityRef) error {
	writer := h.a.ensureWriter()
	if writer == nil {
		return source.NewError(music.SourceHuawei, "reportHistory", source.KindLogin, errors.New("music session missing"))
	}
	return writer.DeletePlaylist(ctx, ref.ID)
}

// NeutralSession projects the provider session onto the host-visible shape.
func NeutralSession(s *Session) *source.Session {
	if s == nil {
		return nil
	}
	return &source.Session{
		Source:     music.SourceHuawei,
		AccountKey: s.AccountKey(),
		Token:      s.token,
		UserID:     s.snsUserID,
		ExpiresAt:  s.expireTime,
	}
}

// ProviderSession rebuilds the provider session from stored host state.
func ProviderSession(s *source.Session) *Session {
	if s == nil || (s.Token == "" && s.UserID == "") {
		return nil
	}
	return &Session{token: s.Token, snsUserID: s.UserID, expireTime: s.ExpiresAt}
}
