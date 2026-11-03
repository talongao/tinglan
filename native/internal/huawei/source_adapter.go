package huawei

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"

	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
)

// SessionFunc returns the current music session, or nil when signed out. The
// desktop model owns the session; the adapter never persists it.
type SessionFunc func() *Session

// SourceAdapter exposes Huawei Music through the provider-neutral source
// contract. It is the only place where Huawei payloads become music.Track.
type SourceAdapter struct {
	client  *Client
	session SessionFunc
}

func NewSourceAdapter(client *Client, session SessionFunc) *SourceAdapter {
	if client == nil {
		client = NewClient()
	}
	return &SourceAdapter{client: client, session: session}
}

func (a *SourceAdapter) ID() music.SourceID { return music.SourceHuawei }

func (a *SourceAdapter) Capabilities() source.Capabilities {
	return source.Capabilities{
		Import: true, Resolve: true, Search: true, Lyrics: true, Playlist: true,
		Collections: []source.Collection{{Kind: "favorites", Name: "我的收藏"}},
		MaxPageSize: 50,
	}
}

// Login is intentionally local and cheap: it reports whether a session is
// present. Definitive expiry is discovered by Import/Resolve and surfaced with
// source.KindLogin.
func (a *SourceAdapter) Login(context.Context) (source.LoginState, error) {
	session := a.currentSession()
	if session == nil || session.token == "" || session.snsUserID == "" {
		return source.LoginState{}, nil
	}
	return source.LoginState{LoggedIn: true, Account: "华为音乐账号"}, nil
}

// AccountKey is the stable, non-secret identity of the signed-in account.
func (a *SourceAdapter) AccountKey() string {
	session := a.currentSession()
	if session == nil {
		return ""
	}
	return session.AccountKey()
}

func (a *SourceAdapter) currentSession() *Session {
	if a == nil || a.session == nil {
		return nil
	}
	return a.session()
}

func (a *SourceAdapter) Import(ctx context.Context, request source.ImportRequest) (source.ImportPage, error) {
	session := a.currentSession()
	if session == nil {
		return source.ImportPage{}, source.NewError(music.SourceHuawei, "import", source.KindLogin, errors.New("music session missing"))
	}
	start := 0
	if request.Cursor != "" {
		parsed, err := strconv.Atoi(request.Cursor)
		if err != nil || parsed < 0 || parsed > 1_000_000 {
			return source.ImportPage{}, source.NewError(music.SourceHuawei, "import", source.KindProtocol, errors.New("invalid import cursor"))
		}
		start = parsed
	}
	limit := request.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 50 {
		limit = 50
	}
	var page Page
	var err error
	switch strings.ToLower(strings.TrimSpace(request.Collection.Kind)) {
	case "", "favorites":
		page, err = a.client.Favorites(ctx, session, start, limit)
	case "playlist":
		if request.Collection.ID == "" {
			return source.ImportPage{}, source.NewError(music.SourceHuawei, "import", source.KindProtocol, errors.New("playlist id missing"))
		}
		page, err = a.client.PlaylistDetail(ctx, session, Playlist{ID: request.Collection.ID, Kind: "owned"}, start, limit)
	default:
		return source.ImportPage{}, source.NewError(music.SourceHuawei, "import", source.KindUnsupported, errors.New("collection kind unsupported"))
	}
	if err != nil {
		return source.ImportPage{}, classify("import", err)
	}
	tracks := make([]music.Track, 0, len(page.Tracks))
	for _, track := range page.Tracks {
		tracks = append(tracks, MusicTrack(track))
	}
	result := source.ImportPage{Tracks: tracks}
	if page.More {
		if len(page.Tracks) == 0 {
			return source.ImportPage{}, source.NewError(music.SourceHuawei, "import", source.KindProtocol, errors.New("favorites pagination made no progress"))
		}
		result.Next = strconv.Itoa(page.Next)
	}
	return result, nil
}

func (a *SourceAdapter) Resolve(ctx context.Context, request source.ResolveRequest) (music.Playback, error) {
	session := a.currentSession()
	if session == nil {
		return music.Playback{}, source.NewError(music.SourceHuawei, "resolve", source.KindLogin, errors.New("music session missing"))
	}
	if request.Track.Source != "" && request.Track.Source != music.SourceHuawei {
		return music.Playback{}, source.NewError(music.SourceHuawei, "resolve", source.KindUnsupported, errors.New("track belongs to another source"))
	}
	quality := strings.ToLower(strings.TrimSpace(request.Quality))
	if quality == "" {
		quality = "standard"
	}
	if quality != "standard" && quality != "lossless" {
		return music.Playback{}, source.NewError(music.SourceHuawei, "resolve", source.KindUnsupported, errors.New("unsupported quality"))
	}
	playback, err := a.client.ResolvePlayback(ctx, session, request.Track.ID, quality)
	if err != nil {
		return music.Playback{}, classify("resolve", err)
	}
	access, evidence := accessFromDurations(request.Track.Duration, playback.Duration)
	return music.Playback{
		URL: playback.URL, Quality: playback.Quality, Access: access, Evidence: evidence,
		Duration: playback.Duration, LyricURL: playback.LyricURL, LyricText: playback.LyricText,
	}, nil
}

// accessFromDurations classifies completeness from the only signal available
// today: the duration of the resolved resource compared with the canonical
// track. It never uses auditionTime, which appears on full resources too.
func accessFromDurations(expected, resolved *float64) (music.Access, string) {
	if expected == nil || *expected <= 0 {
		return music.AccessUnknown, "no-reference-duration"
	}
	if resolved == nil || *resolved <= 0 {
		return music.AccessUnknown, "provider-no-duration"
	}
	delta := math.Abs(*resolved - *expected)
	switch {
	case delta <= 2:
		return music.AccessFull, "duration-match"
	case *resolved < *expected*.85 && *expected-*resolved >= 10:
		return music.AccessPreview, "duration-short"
	default:
		return music.AccessUnknown, "duration-inconclusive"
	}
}

// MusicTrack converts one Huawei track into the neutral model. Non-favorite
// surfaces (search, playlists, history) are still Huawei entities and use this
// converter before entering the shared queue or resolver.
func MusicTrack(track Track) music.Track {
	artists := make([]music.Artist, 0, len(track.Artists))
	for _, artist := range track.Artists {
		artists = append(artists, music.Artist{ID: artist.ID, Name: artist.Name})
	}
	var album *music.Album
	if track.Album != nil {
		album = &music.Album{ID: track.Album.ID, Title: track.Album.Title}
	}
	version := music.DetectVersion(track.Title)
	if track.Album != nil {
		version = music.DetectVersion(track.Title, track.Album.Title)
	}
	return music.Track{
		Source: music.SourceHuawei, ID: track.ID, Title: track.Title, Artists: artists,
		Album: album, CoverURL: track.CoverURL, Duration: track.Duration, Version: version,
	}
}

// classify maps provider failures into the neutral taxonomy. The wrapped error
// never contains a URL, token or response body.
func classify(op string, err error) error {
	if err == nil {
		return nil
	}
	var provider *ProviderError
	if errors.As(err, &provider) {
		kind := source.KindTransient
		switch {
		case provider.Expired, provider.Status == 401, provider.Authenticated && provider.Status == 403:
			kind = source.KindLogin
		case provider.Status == 403:
			kind = source.KindPermission
		case provider.Status == 404:
			kind = source.KindNotFound
		case provider.Status == 429:
			kind = source.KindRateLimit
		case provider.Status >= 500:
			kind = source.KindTransient
		case provider.Code != "" && provider.Code != "000000":
			kind = source.KindProtocol
		}
		return source.NewError(music.SourceHuawei, op, kind, err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return source.NewError(music.SourceHuawei, op, source.KindCanceled, err)
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "session missing"), strings.Contains(message, "invalid music session"):
		return source.NewError(music.SourceHuawei, op, source.KindLogin, err)
	case strings.Contains(message, "unsupported quality"):
		return source.NewError(music.SourceHuawei, op, source.KindUnsupported, err)
	case strings.Contains(message, "resource rejected"), strings.Contains(message, "identity mismatch"):
		return source.NewError(music.SourceHuawei, op, source.KindProtocol, err)
	default:
		return source.NewError(music.SourceHuawei, op, source.KindTransient, err)
	}
}

// Search runs one page of the Huawei catalogue search and converts the rows to
// the neutral track type. Search results have no canonical ID until collected.
func (a *SourceAdapter) Search(ctx context.Context, request source.SearchRequest) (source.SearchPage, error) {
	start := 0
	if request.Cursor != "" {
		value, err := strconv.Atoi(request.Cursor)
		if err != nil || value < 0 {
			return source.SearchPage{}, source.NewError(a.ID(), "search", source.KindProtocol, errors.New("invalid search cursor"))
		}
		start = value
	}
	limit := request.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	page, err := a.client.Search(ctx, request.Query, start, limit)
	if err != nil {
		return source.SearchPage{}, classify("search", err)
	}
	out := make([]music.Track, 0, len(page.Tracks))
	for _, track := range page.Tracks {
		out = append(out, MusicTrack(track))
	}
	next := ""
	if page.More && page.Next > start {
		next = strconv.Itoa(page.Next)
	}
	return source.SearchPage{Tracks: out, Next: next}, nil
}

// TrackFromMusic converts a neutral Huawei-sourced track back into the legacy
// Huawei entity used by the existing queue, list and playback code.
func TrackFromMusic(track music.Track) Track {
	artists := make([]Artist, 0, len(track.Artists))
	for _, artist := range track.Artists {
		artists = append(artists, Artist{ID: artist.ID, Name: artist.Name})
	}
	var album *Album
	if track.Album != nil {
		album = &Album{ID: track.Album.ID, Title: track.Album.Title}
	}
	return Track{
		ID: track.ID, Title: track.Title, Artists: artists, Album: album,
		CoverURL: track.CoverURL, Duration: track.Duration,
	}
}
