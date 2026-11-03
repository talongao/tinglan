package plugin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
	"tinglan/native/internal/storage"
	"tinglan/native/internal/webauth"
)

// sessionDoc is the encrypted per-plugin credential namespace.
type sessionDoc struct {
	Version int                      `json:"version"`
	Entries map[string]sessionEntry  `json:"entries"`
}

type sessionEntry struct {
	Value    string `json:"value"` // base64
	Revision int64  `json:"revision"`
}

// Host implements the host.* services for one plugin process. Every service is
// bound to the plugin identity: a plugin can never name another plugin.
type Host struct {
	id     music.SourceID
	store  *storage.Store
	logger source.Logger

	mu     sync.Mutex
	tokens map[string]time.Time
}

// NewHost creates the host service surface for one plugin.
func NewHost(id music.SourceID, store *storage.Store, logger source.Logger) *Host {
	return &Host{id: id, store: store, logger: logger, tokens: map[string]time.Time{}}
}

// issueWriteToken mints a one-attempt token bound to a login attempt id.
func (h *Host) issueWriteToken(attemptID string) (string, error) {
	if attemptID == "" {
		return "", errors.New("missing attempt id")
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := "wt-" + hex.EncodeToString(buf)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tokens[token] = time.Now().Add(10 * time.Minute)
	return token, nil
}

func (h *Host) validToken(token string) bool {
	if token == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	expiry, ok := h.tokens[token]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(h.tokens, token)
		return false
	}
	return true
}

// Call dispatches one host.* request from the plugin.
func (h *Host) Call(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "host.session.get":
		return h.sessionGet(ctx, params)
	case "host.session.set":
		return h.sessionSet(ctx, params)
	case "host.session.delete":
		return h.sessionDelete(ctx, params)
	case "host.session.list":
		return h.sessionList(ctx)
	case "host.webview.open":
		return h.webviewOpen(ctx, params)
	case "host.webview.close":
		return map[string]any{}, nil
	case "host.auth.present":
		return nil, &RPCError{Code: -32004, Message: "二维码展示未实现"}
	case "host.log":
		return h.logCall(params)
	default:
		return nil, &RPCError{Code: -32601, Message: "unknown host method " + method}
	}
}

func (h *Host) sessionName() string { return "plugin.session." + string(h.id) }

func (h *Host) load() (sessionDoc, error) {
	doc := sessionDoc{Version: 1, Entries: map[string]sessionEntry{}}
	raw, err := h.store.LoadSecret(h.sessionName())
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return doc, err
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return sessionDoc{Version: 1, Entries: map[string]sessionEntry{}}, nil
	}
	if doc.Entries == nil {
		doc.Entries = map[string]sessionEntry{}
	}
	return doc, nil
}

func (h *Host) save(doc sessionDoc) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return h.store.SaveSecret(h.sessionName(), raw)
}

func (h *Host) sessionGet(_ context.Context, params json.RawMessage) (any, error) {
	var in struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, badParams(err)
	}
	doc, err := h.load()
	if err != nil {
		return nil, err
	}
	entry, ok := doc.Entries[in.Key]
	if !ok {
		return map[string]any{"exists": false, "revision": 0, "value": ""}, nil
	}
	return map[string]any{"exists": true, "revision": entry.Revision, "value": entry.Value}, nil
}

func (h *Host) sessionSet(_ context.Context, params json.RawMessage) (any, error) {
	var in struct {
		Key        string `json:"key"`
		Value      string `json:"value"`
		IfRevision int64  `json:"ifRevision"`
		WriteToken string `json:"writeToken"`
	}
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, badParams(err)
	}
	if in.Key == "" {
		return nil, badParams(errors.New("empty key"))
	}
	if !h.validToken(in.WriteToken) {
		return nil, &RPCError{Code: -32001, Message: "写令牌无效或已过期"}
	}
	if decoded, err := base64.StdEncoding.DecodeString(in.Value); err != nil || len(decoded) > 1<<20 {
		return nil, badParams(errors.New("value must be base64 under 1 MiB"))
	}
	doc, err := h.load()
	if err != nil {
		return nil, err
	}
	current := doc.Entries[in.Key]
	if in.IfRevision != 0 && current.Revision != in.IfRevision {
		return nil, &RPCError{Code: -32008, Message: "revision conflict"}
	}
	if in.IfRevision == 0 && current.Revision > 0 {
		return nil, &RPCError{Code: -32008, Message: "revision conflict"}
	}
	current.Value = in.Value
	current.Revision++
	doc.Entries[in.Key] = current
	if err := h.save(doc); err != nil {
		return nil, err
	}
	return map[string]any{"revision": current.Revision}, nil
}

func (h *Host) sessionDelete(_ context.Context, params json.RawMessage) (any, error) {
	var in struct {
		Key        string `json:"key"`
		IfRevision int64  `json:"ifRevision"`
		WriteToken string `json:"writeToken"`
	}
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, badParams(err)
	}
	if !h.validToken(in.WriteToken) {
		return nil, &RPCError{Code: -32001, Message: "写令牌无效或已过期"}
	}
	doc, err := h.load()
	if err != nil {
		return nil, err
	}
	if current, ok := doc.Entries[in.Key]; ok && in.IfRevision != 0 && current.Revision != in.IfRevision {
		return nil, &RPCError{Code: -32008, Message: "revision conflict"}
	}
	delete(doc.Entries, in.Key)
	if err := h.save(doc); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (h *Host) sessionList(context.Context) (any, error) {
	doc, err := h.load()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(doc.Entries))
	for key := range doc.Entries {
		keys = append(keys, key)
	}
	return map[string]any{"keys": keys}, nil
}

func (h *Host) logCall(params json.RawMessage) (any, error) {
	var in struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, badParams(err)
	}
	if h.logger != nil {
		h.logger.Printf("[%s] %s: %s", h.id, in.Level, in.Message)
	}
	return map[string]any{}, nil
}

func (h *Host) webviewOpen(ctx context.Context, params json.RawMessage) (any, error) {
	var in struct {
		AttemptID            string   `json:"attemptId"`
		URL                  string   `json:"url"`
		AllowedDomains       []string `json:"allowedDomains"`
		Callback             struct {
			Scheme string `json:"scheme"`
			Host   string `json:"host"`
			Path   string `json:"path"`
		} `json:"callback"`
		RequireSessionExport bool `json:"requireSessionExport"`
	}
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, badParams(err)
	}
	if in.RequireSessionExport {
		return nil, &RPCError{Code: -32004, Message: "宿主不导出浏览器存储；请改用回调参数或 cookies"}
	}
	parsed, err := url.Parse(in.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, badParams(errors.New("url must be https"))
	}
	flow := &webviewFlow{
		address:  in.URL,
		allowed:  in.AllowedDomains,
		callback: source.CallbackRule{Scheme: in.Callback.Scheme, Host: in.Callback.Host, Path: in.Callback.Path},
	}
	callback, err := webauth.Authorize(ctx, flow)
	if err != nil {
		return nil, &RPCError{Code: -32009, Message: err.Error()}
	}
	return map[string]any{
		"callbackUrl":     callback,
		"cookies":         []any{},
		"sessionSnapshot": map[string]string{},
		"canceled":        false,
	}, nil
}

// webviewFlow adapts the host login window to webauth.Flow.
type webviewFlow struct {
	address  string
	allowed  []string
	callback source.CallbackRule
}

func (f *webviewFlow) AuthorizeURL() string { return f.address }

func (f *webviewFlow) Accept(raw string, _ time.Time) error { return nil }

func (f *webviewFlow) Cancel() {}

func (f *webviewFlow) IsCallback(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	rule := f.callback
	if rule.Scheme != "" && !strings.EqualFold(parsed.Scheme, rule.Scheme) {
		return false
	}
	if rule.Host != "" && !strings.EqualFold(parsed.Host, rule.Host) {
		return false
	}
	if rule.Path != "" && !strings.HasPrefix(parsed.Path, rule.Path) {
		return false
	}
	return rule.Scheme != "" || rule.Host != "" || rule.Path != ""
}

func (f *webviewFlow) AllowedURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	for _, allowed := range f.allowed {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if allowed == "" {
			continue
		}
		if allowed == host {
			return true
		}
		if strings.HasPrefix(allowed, "*.") && strings.HasSuffix(host, allowed[1:]) {
			return true
		}
	}
	return false
}

func badParams(err error) *RPCError {
	return &RPCError{Code: -32602, Message: fmt.Sprintf("invalid params: %v", err)}
}
