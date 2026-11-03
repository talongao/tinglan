// Package sdk implements the plugin side of the source plugin protocol. A
// plugin binary embeds one source.Source and serves it over stdio; calls that
// need host services go back through the same connection.
package sdk

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"tinglan/native/internal/lyrics"
	"tinglan/native/internal/music"
	"tinglan/native/internal/plugin/wire"
	"tinglan/native/internal/source"
)

// ProtocolVersion is the only version this SDK speaks.
const ProtocolVersion = 1

// WebViewRequest asks the host to show its embedded login window.
type WebViewRequest struct {
	AttemptID            string
	URL                  string
	AllowedDomains       []string
	Callback             source.CallbackRule
	RequireSessionExport bool
}

// WebViewResult is the host answer for a login window.
type WebViewResult struct {
	CallbackURL string
	Cookies     []source.NamedValue
	Canceled    bool
}

// Host exposes the host services a plugin may call.
type Host struct {
	conn *conn
}

// Plugin wraps the source implementation served over stdio.
type Plugin struct {
	Source source.Source
	// Auth overrides the source auth surface (for example a provider whose
	// in-process adapter reports login as unsupported).
	Auth source.AuthSource
	// HostServices lists the services the plugin requires. The host rejects an
	// enable when it cannot provide them.
	HostServices []string
	// OnReady runs once the connection is up so the source can capture the
	// host handle before the first request arrives.
	OnReady func(*Host)
}

func (p *Plugin) auth() source.AuthSource {
	if p.Auth != nil {
		return p.Auth
	}
	return p.Source.Auth()
}

// Serve runs the protocol until shutdown, EOF or a protocol error.
func (p *Plugin) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c := newConn(in, out)
	host := &Host{conn: c}
	if p.OnReady != nil {
		p.OnReady(host)
	}
	defer c.close()

	initialized := make(chan struct{})
	var initOnce sync.Once
	markInitialized := func() { initOnce.Do(func() { close(initialized) }) }

	go func() {
		select {
		case <-ctx.Done():
		case <-initialized:
		}
	}()

	for {
		msg, err := wire.Read(c.reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if msg.IsResponse() {
			c.deliver(msg)
			continue
		}
		switch msg.Method {
		case "initialize":
			var params struct {
				ProtocolVersion struct {
					Min int `json:"min"`
					Max int `json:"max"`
				} `json:"protocolVersion"`
				// AssignedID is set when the host imported this plugin and owns
				// its identity; the plugin adopts it instead of its built-in id.
				AssignedID string `json:"assignedId"`
			}
			_ = json.Unmarshal(msg.Params, &params)
			if params.ProtocolVersion.Min > ProtocolVersion || params.ProtocolVersion.Max < ProtocolVersion {
				_ = c.respondError(msg.IDString(), -32002, "unsupported protocol version")
				continue
			}
			descriptor := p.Source.Describe()
			if params.AssignedID != "" {
				descriptor.ID = music.SourceID(params.AssignedID)
			}
			result := map[string]any{
				"protocolVersion":      ProtocolVersion,
				"descriptor":           descriptor,
				"requiredHostServices": p.HostServices,
			}
			if err := c.respond(msg.IDString(), result); err != nil {
				return err
			}
		case "initialized":
			markInitialized()
		case "shutdown":
			_ = c.respond(msg.IDString(), map[string]any{})
			return nil
		case "$/cancelRequest":
			var params struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(msg.Params, &params)
			c.cancel(params.ID)
		default:
			if len(msg.Method) > 7 && msg.Method[:7] == "source." {
				go p.handle(ctx, c, host, msg)
				continue
			}
			_ = c.respondError(msg.IDString(), -32601, "unknown method "+msg.Method)
		}
	}
}

func (p *Plugin) handle(ctx context.Context, c *conn, host *Host, msg wire.Message) {
	id := msg.IDString()
	ctx, cancel := context.WithCancel(ctx)
	c.track(id, cancel)
	defer c.untrack(id)
	defer cancel()

	result, err := p.dispatch(ctx, host, msg.Method, msg.Params)
	if err != nil {
		code := -32010
		var rpcErr *wire.Error
		if errors.As(err, &rpcErr) {
			code = rpcErr.Code
		}
		_ = c.respondError(id, code, err.Error())
		return
	}
	if result == nil {
		_ = c.respond(id, map[string]any{})
		return
	}
	_ = c.respond(id, result)
}

func (p *Plugin) dispatch(ctx context.Context, host *Host, method string, params json.RawMessage) (any, error) {
	switch method {
	case "source.describe":
		return p.Source.Describe(), nil
	case "source.auth.status":
		return p.auth().Status(ctx)
	case "source.auth.verify":
		return p.auth().Verify(ctx)
	case "source.auth.login":
		var request source.LoginRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, fmt.Errorf("login params: %w", err)
		}
		return p.auth().Login(ctx, request)
	case "source.auth.persistSession":
		var request source.PersistRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, fmt.Errorf("persist params: %w", err)
		}
		ok, err := p.auth().PersistSession(ctx, request)
		if err != nil {
			return nil, err
		}
		return map[string]any{"persisted": ok}, nil
	case "source.auth.logout":
		return nil, p.auth().Logout(ctx)
	case "source.library.import":
		var request source.ImportRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, fmt.Errorf("import params: %w", err)
		}
		return p.Source.Library().Import(ctx, request)
	case "source.library.resolve":
		var request source.ResolveRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, fmt.Errorf("resolve params: %w", err)
		}
		playback, err := p.Source.Library().Resolve(ctx, request)
		if err != nil {
			return nil, err
		}
		// music.Playback keeps URLs out of JSON so they never reach disk;
		// the plugin wire has to carry them explicitly.
		return wirePlayback(playback), nil
	case "source.catalog.search":
		var request source.SearchRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, fmt.Errorf("search params: %w", err)
		}
		return p.Source.Catalog().Search(ctx, request)
	case "source.catalog.playlists":
		var request source.PlaylistRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, fmt.Errorf("playlists params: %w", err)
		}
		return p.Source.Catalog().Playlists(ctx, request)
	case "source.catalog.playlistDetail":
		var request source.EntityRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, fmt.Errorf("playlist detail params: %w", err)
		}
		return p.Source.Catalog().PlaylistDetail(ctx, request)
	case "source.catalog.albumDetail":
		var request source.EntityRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, fmt.Errorf("album detail params: %w", err)
		}
		return p.Source.Catalog().AlbumDetail(ctx, request)
	case "source.catalog.artistDetail":
		var request source.EntityRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, fmt.Errorf("artist detail params: %w", err)
		}
		return p.Source.Catalog().ArtistDetail(ctx, request)
	case "source.catalog.history":
		var request source.PageRequest
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, fmt.Errorf("history params: %w", err)
		}
		return p.Source.Catalog().History(ctx, request)
	case "source.lyrics.get":
		var ref source.TrackRef
		if err := json.Unmarshal(params, &ref); err != nil {
			return nil, fmt.Errorf("lyrics params: %w", err)
		}
		lines, err := p.Source.Lyrics().Lyrics(ctx, ref)
		if err != nil {
			return nil, err
		}
		if lines == nil {
			lines = []lyrics.Line{}
		}
		return lines, nil
	case "source.manage.reportHistory":
		var request struct {
			Track           music.Track `json:"track"`
			ListenedSeconds int         `json:"listenedSeconds"`
		}
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, fmt.Errorf("report params: %w", err)
		}
		return nil, p.Source.Manage().ReportHistory(ctx, request.Track, request.ListenedSeconds)
	case "source.manage.updatePlaylistDescription":
		var request struct {
			Ref         source.EntityRef `json:"ref"`
			Description string           `json:"description"`
		}
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, fmt.Errorf("update params: %w", err)
		}
		return nil, p.Source.Manage().UpdatePlaylistDescription(ctx, request.Ref, request.Description)
	case "source.manage.deletePlaylist":
		var request struct {
			Ref source.EntityRef `json:"ref"`
		}
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, fmt.Errorf("delete params: %w", err)
		}
		return nil, p.Source.Manage().DeletePlaylist(ctx, request.Ref)
	default:
		return nil, fmt.Errorf("unknown method %s", method)
	}
}

// SessionGet reads one value from the plugin's credential namespace.
func (h *Host) SessionGet(ctx context.Context, key string) (string, int64, error) {
	var out struct {
		Value    string `json:"value"`
		Revision int64  `json:"revision"`
	}
	if err := h.conn.call(ctx, "host.session.get", map[string]any{"key": key}, &out); err != nil {
		return "", 0, err
	}
	if out.Value == "" {
		return "", out.Revision, nil
	}
	raw, err := base64.StdEncoding.DecodeString(out.Value)
	if err != nil {
		return "", out.Revision, fmt.Errorf("session value: %w", err)
	}
	return string(raw), out.Revision, nil
}

// SessionSet writes one value, optionally guarded by the expected revision.
func (h *Host) SessionSet(ctx context.Context, key, value string, ifRevision int64, writeToken string) (int64, error) {
	var out struct {
		Revision int64 `json:"revision"`
	}
	params := map[string]any{
		"key":        key,
		"value":      base64.StdEncoding.EncodeToString([]byte(value)),
		"ifRevision": ifRevision,
		"writeToken": writeToken,
	}
	if err := h.conn.call(ctx, "host.session.set", params, &out); err != nil {
		return 0, err
	}
	return out.Revision, nil
}

// SessionDelete removes one value.
func (h *Host) SessionDelete(ctx context.Context, key, writeToken string) error {
	return h.conn.call(ctx, "host.session.delete", map[string]any{"key": key, "writeToken": writeToken}, nil)
}

// SessionList lists the keys in the plugin namespace.
func (h *Host) SessionList(ctx context.Context) ([]string, error) {
	var out struct {
		Keys []string `json:"keys"`
	}
	if err := h.conn.call(ctx, "host.session.list", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Keys, nil
}

// WebViewOpen asks the host to run its embedded login window.
func (h *Host) WebViewOpen(ctx context.Context, request WebViewRequest) (WebViewResult, error) {
	params := map[string]any{
		"attemptId":      request.AttemptID,
		"url":            request.URL,
		"allowedDomains": request.AllowedDomains,
		"callback": map[string]string{
			"scheme": request.Callback.Scheme,
			"host":   request.Callback.Host,
			"path":   request.Callback.Path,
		},
		"requireSessionExport": request.RequireSessionExport,
	}
	var out struct {
		CallbackURL string              `json:"callbackUrl"`
		Cookies     []source.NamedValue `json:"cookies"`
		Canceled    bool                `json:"canceled"`
	}
	if err := h.conn.call(ctx, "host.webview.open", params, &out); err != nil {
		return WebViewResult{}, err
	}
	return WebViewResult{CallbackURL: out.CallbackURL, Cookies: out.Cookies, Canceled: out.Canceled}, nil
}

// Log forwards a line to the host log.
func (h *Host) Log(ctx context.Context, level, message string) {
	_ = h.conn.call(ctx, "host.log", map[string]string{"level": level, "message": message}, nil)
}

type conn struct {
	reader *bufio.Reader
	out    io.Writer
	write  sync.Mutex
	mu     sync.Mutex
	next   atomic.Int64
	pend   map[string]chan wire.Message
	calls  map[string]context.CancelFunc
	done   bool
}

func newConn(in io.Reader, out io.Writer) *conn {
	return &conn{
		reader: bufio.NewReaderSize(in, 64<<10),
		out:    out,
		pend:   make(map[string]chan wire.Message),
		calls:  make(map[string]context.CancelFunc),
	}
}

func (c *conn) close() {
	c.mu.Lock()
	c.done = true
	for id, ch := range c.pend {
		close(ch)
		delete(c.pend, id)
	}
	c.mu.Unlock()
}

func (c *conn) send(msg wire.Message) error {
	c.write.Lock()
	defer c.write.Unlock()
	return wire.Write(c.out, msg)
}

func (c *conn) respond(id string, result any) error {
	msg, err := wire.Result(id, result)
	if err != nil {
		return err
	}
	return c.send(msg)
}

func (c *conn) respondError(id string, code int, message string) error {
	return c.send(wire.Failure(id, &wire.Error{Code: code, Message: message}))
}

func (c *conn) deliver(msg wire.Message) {
	id := msg.IDString()
	c.mu.Lock()
	ch := c.pend[id]
	delete(c.pend, id)
	c.mu.Unlock()
	if ch != nil {
		ch <- msg
	}
}

func (c *conn) call(ctx context.Context, method string, params any, out any) error {
	id := fmt.Sprintf("p:%d", c.next.Add(1))
	msg, err := wire.Request(id, method, params)
	if err != nil {
		return err
	}
	ch := make(chan wire.Message, 1)
	c.mu.Lock()
	if c.done {
		c.mu.Unlock()
		return errors.New("plugin: connection closed")
	}
	c.pend[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pend, id)
		c.mu.Unlock()
	}()
	if err := c.send(msg); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		if note, err := wire.Notification("$/cancelRequest", map[string]string{"id": id}); err == nil {
			_ = c.send(note)
		}
		return ctx.Err()
	case reply, ok := <-ch:
		if !ok {
			return errors.New("plugin: connection closed")
		}
		if reply.Error != nil {
			return reply.Error
		}
		if out != nil && len(reply.Result) > 0 {
			return json.Unmarshal(reply.Result, out)
		}
		return nil
	case <-time.After(60 * time.Second):
		return errors.New("plugin: host call timed out")
	}
}

func (c *conn) track(id string, cancel context.CancelFunc) {
	c.mu.Lock()
	c.calls[id] = cancel
	c.mu.Unlock()
}

func (c *conn) untrack(id string) {
	c.mu.Lock()
	delete(c.calls, id)
	c.mu.Unlock()
}

type playbackWire struct {
	URL       string   `json:"url"`
	Quality   string   `json:"quality,omitempty"`
	Access    string   `json:"access"`
	Evidence  string   `json:"evidence,omitempty"`
	Duration  *float64 `json:"durationSeconds,omitempty"`
	LyricURL  string   `json:"lyricUrl,omitempty"`
	LyricText string   `json:"lyricText,omitempty"`
}

func wirePlayback(p music.Playback) playbackWire {
	return playbackWire{
		URL: p.URL, Quality: p.Quality, Access: p.Access.String(), Evidence: p.Evidence,
		Duration: p.Duration, LyricURL: p.LyricURL, LyricText: p.LyricText,
	}
}

func (c *conn) cancel(id string) {
	c.mu.Lock()
	cancel := c.calls[id]
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
