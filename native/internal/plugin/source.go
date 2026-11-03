package plugin

import (
	"context"
	"errors"
	"fmt"

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
func NewPlugin(ctx context.Context, manifest Manifest, proc *Process, host *Host) (*Plugin, error) {
	p := &Plugin{manifest: manifest, proc: proc, desc: descriptorFor(manifest), host: host}
	var runtime source.Descriptor
	params := map[string]any{
		"protocolMin": 1,
		"protocolMax": 1,
		"hostServices": []string{
			"session", "webview", "auth.present", "log",
		},
	}
	if err := proc.Call(ctx, "initialize", params, &runtime); err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}
	merged, err := mergeDescriptor(manifest, runtime)
	if err != nil {
		return nil, err
	}
	p.desc = merged
	if err := proc.Notify("initialized", map[string]any{"protocolVersion": 1}); err != nil {
		return nil, err
	}
	return p, nil
}

func descriptorFor(manifest Manifest) source.Descriptor {
	return source.Descriptor{
		SchemaVersion: manifest.SchemaVersion,
		ID:            music.SourceID(manifest.ID),
		Name:          manifest.Name,
		ShortName:     manifest.ShortName,
		Accent:        manifest.Accent,
		Capabilities: source.Capabilities{
			Import:      manifest.Capabilities.Import,
			Resolve:     manifest.Capabilities.Resolve,
			Search:      manifest.Capabilities.Search,
			Lyrics:      manifest.Capabilities.Lyrics,
			Playlist:    manifest.Capabilities.PlaylistDetail,
			Collections: collectionsFor(manifest),
			MaxPageSize: manifest.Capabilities.MaxPageSize,
		},
		ResourceDomains: manifest.ResourceDomains,
	}
}

func collectionsFor(manifest Manifest) []source.Collection {
	out := make([]source.Collection, 0, len(manifest.Capabilities.Collections))
	for _, c := range manifest.Capabilities.Collections {
		out = append(out, source.Collection{Kind: c.Kind, ID: c.ID, Name: c.Name, PlaylistKind: music.PlaylistKind(c.PlaylistKind)})
	}
	return out
}

// mergeDescriptor validates the runtime descriptor against the manifest: the id
// must match and runtime capabilities may only shrink.
func mergeDescriptor(manifest Manifest, runtime source.Descriptor) (source.Descriptor, error) {
	if runtime.SchemaVersion == 0 {
		runtime.SchemaVersion = manifest.SchemaVersion
	}
	if runtime.ID == "" {
		runtime.ID = music.SourceID(manifest.ID)
	}
	if music.SourceID(manifest.ID) != runtime.ID {
		return source.Descriptor{}, fmt.Errorf("descriptor id %q does not match manifest id %q", runtime.ID, manifest.ID)
	}
	if runtime.Name == "" {
		runtime.Name = manifest.Name
	}
	if runtime.ShortName == "" {
		runtime.ShortName = manifest.ShortName
	}
	if runtime.Accent == "" {
		runtime.Accent = manifest.Accent
	}
	base := descriptorFor(manifest)
	cap := runtime.Capabilities
	cap.Import = cap.Import && base.Capabilities.Import
	cap.Resolve = cap.Resolve && base.Capabilities.Resolve
	cap.Search = cap.Search && base.Capabilities.Search
	cap.Lyrics = cap.Lyrics && base.Capabilities.Lyrics
	cap.Playlist = cap.Playlist && base.Capabilities.Playlist
	if cap.MaxPageSize == 0 || (base.Capabilities.MaxPageSize > 0 && cap.MaxPageSize > base.Capabilities.MaxPageSize) {
		cap.MaxPageSize = base.Capabilities.MaxPageSize
	}
	if len(cap.Collections) == 0 {
		cap.Collections = base.Capabilities.Collections
	}
	runtime.Capabilities = cap
	if len(runtime.ResourceDomains) == 0 {
		runtime.ResourceDomains = manifest.ResourceDomains
	}
	if !domainsWithin(runtime.ResourceDomains, manifest.ResourceDomains) {
		return source.Descriptor{}, errors.New("runtime resourceDomains exceed the manifest")
	}
	return runtime, nil
}

// domainsWithin reports whether every runtime domain is allowed by the manifest.
func domainsWithin(runtime, allowed []string) bool {
	if len(allowed) == 0 {
		return len(runtime) == 0
	}
	for _, want := range runtime {
		found := false
		for _, have := range allowed {
			if domainMatch(have, want) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func domainMatch(allowed, want string) bool {
	if allowed == want {
		return true
	}
	if len(allowed) > 2 && allowed[:2] == "*." {
		suffix := allowed[1:]
		return len(want) > len(suffix) && want[len(want)-len(suffix):] == suffix
	}
	return false
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
	var out music.Playback
	err := l.p.proc.Call(ctx, "source.library.resolve", map[string]any{
		"track":   request.Track,
		"quality": request.Quality,
	}, &out)
	return out, classify(l.p.desc.ID, "resolve", err)
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
