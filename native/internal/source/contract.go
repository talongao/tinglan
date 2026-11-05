package source

import (
	"context"

	"tinglan/native/internal/lyrics"
	"tinglan/native/internal/music"
)

// Source is the complete contract a music source implements. Adapter stays as
// the import/resolve subset during the migration; new code must depend on
// Source so no caller needs to know a provider payload type.
type Source interface {
	ID() music.SourceID
	Manage() ManageSource
	Describe() Descriptor
	Auth() AuthSource
	Library() LibrarySource
	Catalog() CatalogSource
	Lyrics() LyricsSource
}

// Descriptor is the immutable, display-facing identity of one source. Built-in
// and external sources return the same shape so the UI never hard-codes a
// platform name, colour or icon.
type Descriptor struct {
	SchemaVersion   int            `json:"schemaVersion"`
	ID              music.SourceID `json:"id"`
	Name            string         `json:"name"`
	ShortName       string         `json:"shortName"`
	Icon            IconRef        `json:"icon,omitempty"`
	Accent          string         `json:"accent,omitempty"`
	Capabilities    Capabilities   `json:"capabilities"`
	ResourceDomains []string       `json:"resourceDomains,omitempty"`
}

// IconRef points at a bundled or plugin-provided icon. Empty means the UI falls
// back to ShortName in a neutral colour.
type IconRef struct {
	Path string `json:"path,omitempty"`
	// Data is a small inline SVG/PNG payload used by built-in sources.
	Data []byte `json:"-"`
}

// LoginKind is how a source authenticates.
type LoginKind string

const (
	LoginNone    LoginKind = "none"
	LoginWebView LoginKind = "webview"
	LoginQR      LoginKind = "qr"
)

// AuthState is the four-state account status. Unknown is deliberately distinct
// from expired: credentials exist but were not verified, so calls may proceed.
type AuthState string

const (
	AuthSignedOut AuthState = "signedOut"
	AuthSignedIn  AuthState = "signedIn"
	AuthExpired   AuthState = "expired"
	AuthUnknown   AuthState = "unknown"
)

// AuthStatus is the local snapshot. Status never performs a network call;
// Verify is the explicit network check.
type AuthStatus struct {
	State          AuthState `json:"state"`
	AccountKey     string    `json:"accountKey,omitempty"` // stable, never a credential
	DisplayName    string    `json:"displayName,omitempty"`
	LastVerifiedAt string    `json:"lastVerifiedAt,omitempty"`
	ExpiresAt      string    `json:"expiresAt,omitempty"`
	// Ephemeral marks a session that only lives in this process because it could
	// not be persisted. The UI must keep saying so until it is saved.
	Ephemeral bool `json:"ephemeral,omitempty"`
}

// LoginRequest drives one login attempt. The host owns the attempt id so a late
// result from an old attempt cannot be applied to a newer session.
type LoginRequest struct {
	Kind      LoginKind `json:"kind"`
	AttemptID string    `json:"attemptId"`
	// WriteToken is issued by the host and bound to this attempt. The source may
	// use it for SessionStore writes until the attempt ends or is canceled.
	WriteToken string `json:"writeToken,omitempty"`
}

// PersistRequest asks a source to retry saving its in-memory session.
type PersistRequest struct {
	OperationID string `json:"operationId"`
	WriteToken  string `json:"writeToken"`
}

// AuthResult separates "the login worked" from "the session was saved".
type AuthResult struct {
	Status    AuthStatus `json:"status"`
	Succeeded bool       `json:"succeeded"`
	Persisted bool       `json:"persisted"`
}

// AuthSource covers account lifecycle. Implementations own the provider login
// flow and call the host for windows, QR codes and credential storage.
type AuthSource interface {
	Status(ctx context.Context) (AuthStatus, error)
	Verify(ctx context.Context) (AuthStatus, error)
	Login(ctx context.Context, request LoginRequest) (AuthResult, error)
	// PersistSession retries saving a session that only lives in memory. The
	// host initiates it with a one-shot token; the source never asks for one.
	PersistSession(ctx context.Context, request PersistRequest) (bool, error)
	Logout(ctx context.Context) error
}

// LibrarySource imports provider collections and resolves playable streams.
type LibrarySource interface {
	Import(ctx context.Context, request ImportRequest) (ImportPage, error)
	Resolve(ctx context.Context, request ResolveRequest) (music.Playback, error)
}

// CatalogSource exposes the provider catalogue through neutral types.
type CatalogSource interface {
	Search(ctx context.Context, request SearchRequest) (Page[music.Track], error)
	Playlists(ctx context.Context, request PlaylistRequest) (Page[music.Playlist], error)
	PlaylistDetail(ctx context.Context, request EntityRequest) (PlaylistDetail, error)
	AlbumDetail(ctx context.Context, request EntityRequest) (AlbumDetail, error)
	ArtistDetail(ctx context.Context, request EntityRequest) (ArtistDetail, error)
	History(ctx context.Context, request PageRequest) (Page[music.Track], error)
}

// ManageSource covers provider-side writes the app still performs. Favorites
// are local data and never go through here; playlist edits only touch the
// remote playlist the user explicitly opened.
type ManageSource interface {
	ReportHistory(ctx context.Context, track music.Track, listenedSeconds int) error
	UpdatePlaylistDescription(ctx context.Context, ref EntityRef, description string) error
	DeletePlaylist(ctx context.Context, ref EntityRef) error
}

// LyricsSource returns lyrics for one track when the provider has them.
type LyricsSource interface {
	Lyrics(ctx context.Context, ref TrackRef) ([]lyrics.Line, error)
}

// Page is one opaque page of results.
type Page[T any] struct {
	Items   []T    `json:"items"`
	Next    string `json:"nextCursor,omitempty"`
	HasMore bool   `json:"hasMore"`
}

// PageRequest is the common pagination input.
type PageRequest struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// PlaylistRequest lists a provider's playlists.
type PlaylistRequest struct {
	Kind   string `json:"kind,omitempty"` // owned | favorite | chart; empty means the provider default
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// TrackRef identifies a track on exactly one source.
type TrackRef struct {
	Source music.SourceID `json:"source"`
	ID     string         `json:"id"`
}

// EntityRef identifies a non-track entity on one source.
type EntityRef struct {
	Source music.SourceID `json:"source"`
	Kind   string         `json:"kind"` // playlist | album | artist
	ID     string         `json:"id"`
	// PlaylistKind carries owned|favorite|chart for playlists, because the same
	// remote id can mean different things in different provider endpoints.
	PlaylistKind music.PlaylistKind `json:"playlistKind,omitempty"`
}

// EntityRequest pages one entity detail.
type EntityRequest struct {
	Ref EntityRef `json:"ref"`
	// Section selects which cursor a multi-section detail pages: "tracks" or
	// "albums" for artists; empty means the primary section.
	Section string `json:"section,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// AccessRequirement tells the UI whether an entity is readable anonymously.
type AccessRequirement string

const (
	AccessUnknown   AccessRequirement = ""
	AccessAnonymous AccessRequirement = "anonymous"
	AccessLogin     AccessRequirement = "login"
	AccessMember    AccessRequirement = "member"
)

// PlaylistDetail is one page of a playlist plus display metadata.
type PlaylistDetail struct {
	Playlist music.Playlist `json:"playlist"`
	Tracks   []music.Track  `json:"tracks"`
	Next     string         `json:"nextCursor,omitempty"`
	HasMore  bool           `json:"hasMore"`
	// Truncated marks a provider-computed finite list that cannot be paged.
	Truncated bool              `json:"truncated,omitempty"`
	Access    AccessRequirement `json:"access,omitempty"`
}

// AlbumDetail is one page of an album.
type AlbumDetail struct {
	Album     music.Album       `json:"album"`
	Tracks    []music.Track     `json:"tracks"`
	Next      string            `json:"nextCursor,omitempty"`
	HasMore   bool              `json:"hasMore"`
	Truncated bool              `json:"truncated,omitempty"`
	Access    AccessRequirement `json:"access,omitempty"`
}

// ArtistDetail pages songs and albums independently.
type ArtistDetail struct {
	Artist        music.Artist      `json:"artist"`
	Tracks        []music.Track     `json:"tracks"`
	Albums        []music.Album     `json:"albums"`
	NextTracks    string            `json:"nextTracksCursor,omitempty"`
	NextAlbums    string            `json:"nextAlbumsCursor,omitempty"`
	HasMoreTracks bool              `json:"hasMoreTracks"`
	HasMoreAlbums bool              `json:"hasMoreAlbums"`
	Truncated     bool              `json:"truncated,omitempty"`
	Access        AccessRequirement `json:"access,omitempty"`
}

// Host is what the application injects into an in-process source. External
// plugins receive the same services over JSON-RPC (see the protocol document).
type Host interface {
	Session() SessionStore
	LoginUI() LoginUI
	Progress() ProgressSink
	Log() Logger
}

// SessionStore keeps opaque, encrypted per-source credential blobs.
type SessionStore interface {
	Get(ctx context.Context, key string) (value []byte, revision int64, exists bool, err error)
	Set(ctx context.Context, key string, value []byte, ifRevision int64, writeToken string) (int64, error)
	Delete(ctx context.Context, key string, ifRevision int64, writeToken string) error
	List(ctx context.Context) ([]string, error)
}

// WebViewRequest asks the host to present a login window.
type WebViewRequest struct {
	AttemptID      string
	URL            string
	AllowedDomains []string
	Callback       CallbackRule
	Cookies        []NamedValue
	// RequireSessionExport makes the host fail instead of silently returning a
	// cookie-only result the provider flow cannot use.
	RequireSessionExport bool
}

// CallbackRule matches the redirect that ends a login attempt.
type CallbackRule struct {
	Scheme string
	Host   string
	Path   string
}

// NamedValue is one cookie preset for the temporary login window.
type NamedValue struct {
	Name   string
	Value  string
	Domain string
}

// WebViewResult is the redirect URL plus the temporary window's cookie snapshot.
type WebViewResult struct {
	CallbackURL string
	Cookies     []NamedValue
	// SessionSnapshot exposes storage that cannot be derived from cookies.
	// Values are opaque to the host and never contain playback URLs.
	SessionSnapshot map[string]string
	// Canceled reports that the user closed the window; ErrCanceled is also
	// returned so callers cannot mistake it for a provider failure.
	Canceled bool
}

// QRRequest asks the host to render a login QR payload.
type QRRequest struct {
	AttemptID string
	Payload   string
	ExpiresAt string
}

// LoginUI is the host-side presentation surface for authentication.
type LoginUI interface {
	WebView(ctx context.Context, request WebViewRequest) (WebViewResult, error)
	PresentQRCode(ctx context.Context, request QRRequest) error
}

// ProgressSink reports long-running work.
type ProgressSink interface {
	Report(ctx context.Context, phase string, current, total int)
}

// Logger is a minimal structured logger; implementations must redact secrets.
type Logger interface {
	Printf(format string, args ...any)
}

// Session is the host-visible account state. Fields are opaque to every layer
// above the source; the host only persists and hands them back untouched.
type Session struct {
	Source     music.SourceID `json:"source"`
	AccountKey string         `json:"accountKey,omitempty"`
	Token      string         `json:"token,omitempty"`
	UserID     string         `json:"userId,omitempty"`
	ExpiresAt  string         `json:"expiresAt,omitempty"`
}

// Account returns the stable, non-secret account label; empty when unknown.
func (s *Session) Account() string {
	if s == nil {
		return ""
	}
	return s.AccountKey
}
