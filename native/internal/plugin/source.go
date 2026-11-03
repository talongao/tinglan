package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"tinglan/native/internal/lyrics"
	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
)

// Plugin is one enabled external source. It implements source.Source by
// forwarding every call over JSON-RPC; no provider payload reaches the host.
type Plugin struct {
	manifest Manifest
	proc     *Process
	desc     source.Descriptor
	host     *Host
}

// NewPlugin handshakes with a freshly started process and returns the source.
func NewPlugin(ctx context.Context, manifest Manifest, expectedID music.SourceID, proc *Process, host *Host) (*Plugin, error) {
	p := &Plugin{manifest: manifest, proc: proc, desc: source.Descriptor{SchemaVersion: 1, ID: expectedID, Name: string(expectedID)}, host: host}
	services := []string{"session", "webview", "log"}
	params := map[string]any{
		"assignedId":      string(expectedID),
		"protocolVersion": map[string]int{"min": 1, "max": 1},
		"hostServices":    services,
		"limits": map[string]int{
			"sessionValueBytes": 1 << 20,
			"sessionTotalBytes": 8 << 20,
			"sessionKeys":       64,
		},
		"hostVersion": "1.0",
	}
	var handshake struct {
		ProtocolVersion      int               `json:"protocolVersion"`
		Descriptor           source.Descriptor `json:"descriptor"`
		RequiredHostServices []string          `json:"requiredHostServices"`
	}
	if err := proc.Call(ctx, "initialize", params, &handshake); err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}
	if handshake.ProtocolVersion != 1 {
		return nil, fmt.Errorf("initialize: unsupported protocol %d", handshake.ProtocolVersion)
	}
	provided := make(map[string]bool, len(services))
	for _, service := range services {
		provided[service] = true
	}
	for _, service := range handshake.RequiredHostServices {
		if !provided[service] {
			return nil, fmt.Errorf("initialize: plugin requires unknown host service %q", service)
		}
	}
	runtime := handshake.Descriptor
	merged, err := acceptDescriptor(manifest, expectedID, runtime)
	if err != nil {
		return nil, err
	}
	p.desc = merged
	if err := proc.Notify("initialized", map[string]any{"generation": 1}); err != nil {
		return nil, err
	}
	return p, nil
}

// acceptDescriptor validates what the plugin reported during the handshake.
// Capabilities and display metadata come from the plugin itself; the host only
// insists on a stable id and a display name.
func acceptDescriptor(manifest Manifest, expectedID music.SourceID, runtime source.Descriptor) (source.Descriptor, error) {
	if runtime.ID == "" {
		return source.Descriptor{}, errors.New("initialize: plugin did not report an id")
	}
	if expectedID != "" && runtime.ID != expectedID {
		return source.Descriptor{}, fmt.Errorf("initialize: id %q does not match %q", runtime.ID, expectedID)
	}
	if manifest.ID != "" && string(runtime.ID) != manifest.ID {
		return source.Descriptor{}, fmt.Errorf("initialize: id %q does not match manifest %q", runtime.ID, manifest.ID)
	}
	if strings.TrimSpace(runtime.Name) == "" {
		return source.Descriptor{}, errors.New("initialize: plugin did not report a name")
	}
	if runtime.SchemaVersion == 0 {
		runtime.SchemaVersion = manifest.SchemaVersion
	}
	return runtime, nil
}

// ID returns the stable source identifier.
func (p *Plugin) ID() music.SourceID { return p.desc.ID }

// Describe returns the negotiated descriptor.
func (p *Plugin) Describe() source.Descriptor { return p.desc }

// Host exposes the injected services (kept for symmetry with in-process sources).
func (p *Plugin) Host() *Host { return p.host }

// Close terminates the plugin process.
func (p *Plugin) Close() error { return p.proc.Close() }

// Manage returns the remote write surface.
func (p *Plugin) Manage() source.ManageSource { return manageSource{p} }

// Auth returns the account lifecycle surface.
func (p *Plugin) Auth() source.AuthSource { return authSource{p} }

// Library returns import/resolve.
func (p *Plugin) Library() source.LibrarySource { return librarySource{p} }

// Catalog returns catalogue reads.
func (p *Plugin) Catalog() source.CatalogSource { return catalogSource{p} }

// Lyrics returns the lyrics surface.
func (p *Plugin) Lyrics() source.LyricsSource { return lyricsSource{p} }

type authSource struct{ p *Plugin }

func (a authSource) Status(ctx context.Context) (source.AuthStatus, error) {
	var out source.AuthStatus
	err := a.p.proc.Call(ctx, "source.auth.status", map[string]any{}, &out)
	return out, classify(a.p.desc.ID, "status", err)
}

func (a authSource) Verify(ctx context.Context) (source.AuthStatus, error) {
	var out source.AuthStatus
	err := a.p.proc.Call(ctx, "source.auth.verify", map[string]any{}, &out)
	return out, classify(a.p.desc.ID, "verify", err)
}

func (a authSource) Login(ctx context.Context, request source.LoginRequest) (source.AuthResult, error) {
	token, err := a.p.host.issueWriteToken(request.AttemptID)
	if err != nil {
		return source.AuthResult{}, err
	}
	params := map[string]any{
		"kind":       string(request.Kind),
		"attemptId":  request.AttemptID,
		"writeToken": token,
	}
	var out source.AuthResult
	err = a.p.proc.Call(ctx, "source.auth.login", params, &out)
	return out, classify(a.p.desc.ID, "login", err)
}

func (a authSource) PersistSession(ctx context.Context, request source.PersistRequest) (bool, error) {
	var out struct {
		Persisted bool `json:"persisted"`
	}
	err := a.p.proc.Call(ctx, "source.auth.persistSession", map[string]any{
		"operationId": request.OperationID,
		"writeToken":  request.WriteToken,
	}, &out)
	return out.Persisted, classify(a.p.desc.ID, "persistSession", err)
}

func (a authSource) Logout(ctx context.Context) error {
	err := a.p.proc.Call(ctx, "source.auth.logout", map[string]any{}, nil)
	return classify(a.p.desc.ID, "logout", err)
}

type librarySource struct{ p *Plugin }

func (l librarySource) Import(ctx context.Context, request source.ImportRequest) (source.ImportPage, error) {
	params := map[string]any{
		"collection": map[string]any{
			"kind":         request.Collection.Kind,
			"id":           request.Collection.ID,
			"name":         request.Collection.Name,
			"playlistKind": string(request.Collection.PlaylistKind),
		},
		"runId":  request.RunID,
		"cursor": request.Cursor,
		"limit":  request.Limit,
	}
	var out source.ImportPage
	err := l.p.proc.Call(ctx, "source.library.import", params, &out)
	if err != nil {
		return source.ImportPage{}, classify(l.p.desc.ID, "import", err)
	}
	for i := range out.Tracks {
		if out.Tracks[i].Source == "" {
			out.Tracks[i].Source = l.p.desc.ID
		}
	}
	return out, nil
}

func (l librarySource) Resolve(ctx context.Context, request source.ResolveRequest) (music.Playback, error) {
	var out playbackWire
	err := l.p.proc.Call(ctx, "source.library.resolve", map[string]any{
		"track":   request.Track,
		"quality": request.Quality,
	}, &out)
	if err != nil {
		return music.Playback{}, classify(l.p.desc.ID, "resolve", err)
	}
	return music.Playback{
		URL: out.URL, Quality: out.Quality, Access: parseAccess(out.Access),
		Evidence: out.Evidence, Duration: out.Duration,
		LyricURL: out.LyricURL, LyricText: out.LyricText,
	}, nil
}

// playbackWire mirrors the SDK's wire type: music.Playback hides URLs from
// JSON so they can never be persisted, but the plugin boundary must carry them.
type playbackWire struct {
	URL       string   `json:"url"`
	Quality   string   `json:"quality,omitempty"`
	Access    string   `json:"access"`
	Evidence  string   `json:"evidence,omitempty"`
	Duration  *float64 `json:"durationSeconds,omitempty"`
	LyricURL  string   `json:"lyricUrl,omitempty"`
	LyricText string   `json:"lyricText,omitempty"`
}

func parseAccess(v string) music.Access {
	switch v {
	case "full":
		return music.AccessFull
	case "preview":
		return music.AccessPreview
	default:
		return music.AccessUnknown
	}
}

type catalogSource struct{ p *Plugin }

func (c catalogSource) Search(ctx context.Context, request source.SearchRequest) (source.Page[music.Track], error) {
	var out source.Page[music.Track]
	err := c.p.proc.Call(ctx, "source.catalog.search", map[string]any{
		"query":  request.Query,
		"cursor": request.Cursor,
		"limit":  request.Limit,
	}, &out)
	if err != nil {
		return out, classify(c.p.desc.ID, "search", err)
	}
	c.p.stampSource(out.Items)
	return out, nil
}

func (c catalogSource) Playlists(ctx context.Context, request source.PlaylistRequest) (source.Page[music.Playlist], error) {
	var out source.Page[music.Playlist]
	err := c.p.proc.Call(ctx, "source.catalog.playlists", map[string]any{
		"kind":   request.Kind,
		"cursor": request.Cursor,
		"limit":  request.Limit,
	}, &out)
	if err != nil {
		return out, classify(c.p.desc.ID, "playlists", err)
	}
	for i := range out.Items {
		if out.Items[i].Source == "" {
			out.Items[i].Source = c.p.desc.ID
		}
	}
	return out, nil
}

func (c catalogSource) PlaylistDetail(ctx context.Context, request source.EntityRequest) (source.PlaylistDetail, error) {
	var out source.PlaylistDetail
	err := c.p.proc.Call(ctx, "source.catalog.playlistDetail", entityParams(request), &out)
	if err != nil {
		return out, classify(c.p.desc.ID, "playlistDetail", err)
	}
	c.p.stampSource(out.Tracks)
	return out, nil
}

func (c catalogSource) AlbumDetail(ctx context.Context, request source.EntityRequest) (source.AlbumDetail, error) {
	var out source.AlbumDetail
	err := c.p.proc.Call(ctx, "source.catalog.albumDetail", entityParams(request), &out)
	if err != nil {
		return out, classify(c.p.desc.ID, "albumDetail", err)
	}
	c.p.stampSource(out.Tracks)
	return out, nil
}

func (c catalogSource) ArtistDetail(ctx context.Context, request source.EntityRequest) (source.ArtistDetail, error) {
	var out source.ArtistDetail
	err := c.p.proc.Call(ctx, "source.catalog.artistDetail", entityParams(request), &out)
	if err != nil {
		return out, classify(c.p.desc.ID, "artistDetail", err)
	}
	c.p.stampSource(out.Tracks)
	return out, nil
}

func (c catalogSource) History(ctx context.Context, request source.PageRequest) (source.Page[music.Track], error) {
	var out source.Page[music.Track]
	err := c.p.proc.Call(ctx, "source.catalog.history", map[string]any{
		"cursor": request.Cursor,
		"limit":  request.Limit,
	}, &out)
	if err != nil {
		return out, classify(c.p.desc.ID, "history", err)
	}
	c.p.stampSource(out.Items)
	return out, nil
}

type lyricsSource struct{ p *Plugin }

func (l lyricsSource) Lyrics(ctx context.Context, ref source.TrackRef) ([]lyrics.Line, error) {
	var out []wireLine
	err := l.p.proc.Call(ctx, "source.lyrics.get", map[string]any{"ref": ref}, &out)
	if err != nil {
		return nil, classify(l.p.desc.ID, "lyrics", err)
	}
	lines := make([]lyrics.Line, 0, len(out))
	for _, line := range out {
		converted := lyrics.Line{Time: line.Time, Text: line.Text, Translation: line.Translation, Romanization: line.Romanization}
		for _, word := range line.Words {
			converted.Words = append(converted.Words, lyrics.Word{Time: word.Time, Text: word.Text})
		}
		lines = append(lines, converted)
	}
	return lines, nil
}

type manageSource struct{ p *Plugin }

func (m manageSource) ReportHistory(ctx context.Context, track music.Track, listenedSeconds int) error {
	err := m.p.proc.Call(ctx, "source.manage.reportHistory", map[string]any{
		"track":           track,
		"listenedSeconds": listenedSeconds,
	}, nil)
	return classify(m.p.desc.ID, "reportHistory", err)
}

func (m manageSource) UpdatePlaylistDescription(ctx context.Context, ref source.EntityRef, description string) error {
	err := m.p.proc.Call(ctx, "source.manage.updatePlaylistDescription", map[string]any{
		"ref":         ref,
		"description": description,
	}, nil)
	return classify(m.p.desc.ID, "updatePlaylistDescription", err)
}

func (m manageSource) DeletePlaylist(ctx context.Context, ref source.EntityRef) error {
	err := m.p.proc.Call(ctx, "source.manage.deletePlaylist", map[string]any{"ref": ref}, nil)
	return classify(m.p.desc.ID, "deletePlaylist", err)
}

type wireLine struct {
	Time         float64    `json:"time"`
	Text         string     `json:"text"`
	Translation  string     `json:"translation,omitempty"`
	Romanization string     `json:"romanization,omitempty"`
	Words        []wireWord `json:"words,omitempty"`
}

type wireWord struct {
	Time float64 `json:"time"`
	Text string  `json:"text"`
}

func entityParams(request source.EntityRequest) map[string]any {
	return map[string]any{
		"ref":     request.Ref,
		"section": request.Section,
		"cursor":  request.Cursor,
		"limit":   request.Limit,
	}
}

func (p *Plugin) stampSource(tracks []music.Track) {
	for i := range tracks {
		if tracks[i].Source == "" {
			tracks[i].Source = p.desc.ID
		}
	}
}

// classify maps JSON-RPC error codes onto the neutral source error kinds.
func classify(id music.SourceID, op string, err error) error {
	if err == nil {
		return nil
	}
	var rpc *RPCError
	if !errors.As(err, &rpc) {
		return err
	}
	kind := source.KindUnavailable
	switch rpc.Code {
	case -32001:
		kind = source.KindLogin
	case -32002:
		kind = source.KindNotFound
	case -32003:
		kind = source.KindRateLimit
	case -32004:
		kind = source.KindUnsupported
	case -32005:
		kind = source.KindNetwork
	case -32006:
		kind = source.KindPermission
	case -32007:
		kind = source.KindTransient
	case -32008:
		kind = source.KindProtocol
	case -32009:
		kind = source.KindCanceled
	case -32010:
		kind = source.KindUnavailable
	}
	return source.NewError(id, op, kind, errors.New(rpc.Message))
}
