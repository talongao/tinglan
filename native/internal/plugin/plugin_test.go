package plugin

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
	"tinglan/native/internal/storage"
)

// TestMain doubles as the fake plugin entry point. The test builds a launcher
// script pointing back at this binary, so no Go toolchain is needed at runtime.
func TestMain(m *testing.M) {
	if os.Getenv("TINGLAN_TEST_PLUGIN") == "1" {
		serveFakePlugin()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func serveFakePlugin() {
	reader := bufio.NewReaderSize(os.Stdin, 64<<10)
	out := os.Stdout
	for {
		payload, err := readFrame(reader)
		if err != nil {
			return
		}
		var msg rpcMessage
		if json.Unmarshal(payload, &msg) != nil {
			return
		}
		if msg.Method == "" {
			continue
		}
		if msg.Method == "shutdown" {
			return
		}
		var result any
		switch msg.Method {
		case "initialize":
			var handshake struct {
				AssignedID string `json:"assignedId"`
			}
			_ = json.Unmarshal(msg.Params, &handshake)
			id := "fake"
			if handshake.AssignedID != "" {
				id = handshake.AssignedID
			}
			result = map[string]any{
				"protocolVersion":      1,
				"requiredHostServices": []string{},
				"descriptor": map[string]any{
					"schemaVersion": 1,
					"id":            id,
					"name":          "测试音源",
					"shortName":     "测",
					"accent":        "#336699",
					"capabilities": map[string]any{
						"search": true, "import": true, "collections": []any{
							map[string]any{"kind": "favorites", "name": "我喜欢的音乐"},
						},
						"maxPageSize": 20,
					},
					"resourceDomains": []string{"*.example.test"},
				},
			}
		case "source.auth.status":
			result = map[string]any{"state": "signedOut"}
		case "source.catalog.search":
			result = map[string]any{
				"items":   []any{map[string]any{"id": "t1", "title": "第一首"}},
				"hasMore": false,
			}
		case "source.library.import":
			var in struct {
				Cursor string `json:"cursor"`
			}
			_ = json.Unmarshal(msg.Params, &in)
			if in.Cursor == "" {
				result = map[string]any{
					"tracks": []any{
						map[string]any{"id": "h1", "title": "第一首"},
						map[string]any{"id": "h2", "title": "第二首"},
					},
					"nextCursor": "1",
				}
			} else {
				result = map[string]any{"tracks": []any{map[string]any{"id": "h3", "title": "第三首"}}}
			}
		case "host.session.set", "host.session.get":
			result = map[string]any{"revision": 1, "exists": false}
		case "source.manage.reportHistory", "source.manage.updatePlaylistDescription", "source.manage.deletePlaylist":
			if path := os.Getenv("TINGLAN_TEST_PLUGIN_LOG"); path != "" {
				if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
					fmt.Fprintf(f, "%s %s\n", msg.Method, string(msg.Params))
					f.Close()
				}
			}
			result = map[string]any{}
		default:
			result = map[string]any{}
		}
		raw, _ := json.Marshal(result)
		reply := rpcMessage{JSONRPC: "2.0", ID: msg.ID}
		reply.Result = raw
		body, _ := json.Marshal(reply)
		fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}
}

func writeFakePlugin(t *testing.T, root string) Manifest {
	t.Helper()
	dir := filepath.Join(root, "fake")
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nTINGLAN_TEST_PLUGIN=1 exec \"" + exe + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "bin", "plugin"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		SchemaVersion: 1,
		ID:            "fake",
		Entry:         map[string]EntrySpec{PlatformKey(): {Command: "bin/plugin"}},
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "source.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func newTestManager(t *testing.T) (*Manager, *source.Registry) {
	t.Helper()
	root := t.TempDir()
	writeFakePlugin(t, root)
	store := storage.New(t.TempDir())
	manager := NewManager(root, store, nil)
	return manager, source.NewRegistry()
}

func TestManagerEnablesAndRegistersPlugin(t *testing.T) {
	manager, registry := newTestManager(t)
	defer manager.Close()

	entries := manager.List()
	if len(entries) != 1 || entries[0].Manifest.ID != "fake" {
		t.Fatalf("discovered entries = %+v", entries)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := manager.Enable(ctx, registry, "fake"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	src, ok := registry.Source("fake")
	if !ok {
		t.Fatal("source not registered")
	}
	if desc := src.Describe(); desc.Name != "测试音源" || desc.Accent != "#336699" {
		t.Fatalf("descriptor = %+v", desc)
	}
	page, err := src.Catalog().Search(ctx, source.SearchRequest{Query: "任意", Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Source != music.SourceID("fake") {
		t.Fatalf("search page = %+v", page)
	}
	first, err := src.Library().Import(ctx, source.ImportRequest{Collection: source.Collection{Kind: "favorites"}, Limit: 20, RunID: "run-1"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(first.Tracks) != 2 || first.Next != "1" {
		t.Fatalf("first page = %+v", first)
	}
	second, err := src.Library().Import(ctx, source.ImportRequest{Collection: source.Collection{Kind: "favorites"}, Cursor: first.Next, Limit: 20, RunID: "run-1"})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Tracks) != 1 || second.Next != "" {
		t.Fatalf("second page = %+v", second)
	}
	if err := manager.Disable(registry, "fake"); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Source("fake"); ok {
		t.Fatal("source still registered after disable")
	}
	if manager.Running() != 0 {
		t.Fatal("plugin process still running")
	}
}

func TestManagerLoadEnabledRestoresState(t *testing.T) {
	root := t.TempDir()
	writeFakePlugin(t, root)
	store := storage.New(t.TempDir())
	first := NewManager(root, store, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	registry := source.NewRegistry()
	if err := first.Enable(ctx, registry, "fake"); err != nil {
		t.Fatal(err)
	}
	first.Close()

	second := NewManager(root, store, nil)
	defer second.Close()
	registry = source.NewRegistry()
	if failures := second.LoadEnabled(ctx, registry); len(failures) != 0 {
		t.Fatalf("load failures = %v", failures)
	}
	if _, ok := registry.Source("fake"); !ok {
		t.Fatal("enabled plugin not restored")
	}
}

func TestHostSessionStoreRequiresWriteToken(t *testing.T) {
	store := storage.New(t.TempDir())
	host := NewHost("fake", store, nil)
	ctx := context.Background()

	token, err := host.issueWriteToken("attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	value := []byte("opaque-session")
	encoded := encodeBase64(value)
	result, err := host.Call(ctx, "host.session.set", mustJSON(t, map[string]any{
		"key": "account", "value": encoded, "ifRevision": 0, "writeToken": token,
	}))
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if rev := revisionOf(result); rev != 1 {
		t.Fatalf("revision = %v", result)
	}
	if _, err := host.Call(ctx, "host.session.set", mustJSON(t, map[string]any{
		"key": "account", "value": encoded, "ifRevision": 0, "writeToken": "bad",
	})); err == nil {
		t.Fatal("write without a valid token must fail")
	}
	got, err := host.Call(ctx, "host.session.get", mustJSON(t, map[string]any{"key": "account"}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	entry := got.(map[string]any)
	if entry["exists"] != true || entry["value"] != encoded {
		t.Fatalf("entry = %v", entry)
	}
	// A different plugin must not see this namespace.
	other := NewHost("other", store, nil)
	otherValue, err := other.Call(ctx, "host.session.get", mustJSON(t, map[string]any{"key": "account"}))
	if err != nil {
		t.Fatal(err)
	}
	if otherValue.(map[string]any)["exists"] != false {
		t.Fatal("plugin session namespaces must be isolated")
	}
}

func TestSecureJoinRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	if _, err := secureJoin(dir, "../escape"); err == nil {
		t.Fatal("escaping entry must be rejected")
	}
	inside := filepath.Join(dir, "bin")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(inside, "plugin")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := secureJoin(dir, "bin/plugin"); err != nil {
		t.Fatalf("inside entry rejected: %v", err)
	}
}

func TestLoadManifestRejectsEscapingEntry(t *testing.T) {
	dir := t.TempDir()
	raw := `{"schemaVersion":1,"id":"x","name":"X","entry":{"darwin-arm64":"../bin/x"},"protocol":{"min":1,"max":1}}`
	if err := os.WriteFile(filepath.Join(dir, "source.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("manifest with escaping entry accepted")
	}
}

func revisionOf(value any) int64 {
	switch rev := value.(map[string]any)["revision"].(type) {
	case int64:
		return rev
	case float64:
		return int64(rev)
	default:
		return 0
	}
}

func encodeBase64(value []byte) string { return base64.StdEncoding.EncodeToString(value) }

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestManageMethodsForwardToPlugin proves the write path (history report,
// playlist description, playlist delete) reaches the plugin process unchanged.
func TestManageMethodsForwardToPlugin(t *testing.T) {
	manager, registry := newTestManager(t)
	defer manager.Close()
	logPath := filepath.Join(t.TempDir(), "manage.log")
	t.Setenv("TINGLAN_TEST_PLUGIN_LOG", logPath)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := manager.Enable(ctx, registry, "fake"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	src, ok := registry.Source("fake")
	if !ok {
		t.Fatal("source not registered")
	}
	manage := src.Manage()
	track := music.Track{Source: "fake", ID: "t1", Title: "第一首"}
	if err := manage.ReportHistory(ctx, track, 42); err != nil {
		t.Fatalf("report history: %v", err)
	}
	ref := source.EntityRef{Source: "fake", Kind: "playlist", ID: "pl-9"}
	if err := manage.UpdatePlaylistDescription(ctx, ref, "新的简介"); err != nil {
		t.Fatalf("update description: %v", err)
	}
	if err := manage.DeletePlaylist(ctx, ref); err != nil {
		t.Fatalf("delete playlist: %v", err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read plugin log: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		"source.manage.reportHistory",
		`"listenedSeconds":42`,
		"source.manage.updatePlaylistDescription",
		"新的简介",
		"source.manage.deletePlaylist",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin log missing %q:\n%s", want, text)
		}
	}
}

func TestManagerInstallsPluginWithAuthorID(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	writeFakePlugin(t, src)
	src = filepath.Join(src, "fake")
	manager := NewManager(t.TempDir(), storage.New(t.TempDir()), nil)
	entry, updated, err := manager.Install(ctx, src)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if updated {
		t.Fatal("first install reported as update")
	}
	if string(entry.ID()) != "fake" {
		t.Fatalf("id = %q, want the id from source.json", entry.ID())
	}
	if entry.Name() != "测试音源" {
		t.Fatalf("name = %q", entry.Name())
	}
	if _, err := LoadDescriptor(entry.Dir); err != nil {
		t.Fatalf("descriptor not cached: %v", err)
	}
	registry := source.NewRegistry()
	if err := manager.Enable(ctx, registry, entry.ID()); err != nil {
		t.Fatalf("enable after install: %v", err)
	}
	if _, ok := registry.Source(entry.ID()); !ok {
		t.Fatal("installed plugin not registered")
	}
	manager.Disable(registry, entry.ID())
}

func TestManagerUpdatesPluginWithSameID(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	writeFakePlugin(t, src)
	src = filepath.Join(src, "fake")
	manager := NewManager(t.TempDir(), storage.New(t.TempDir()), nil)
	first, _, err := manager.Install(ctx, src)
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	second, updated, err := manager.Install(ctx, src)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if !updated || second.ID() != first.ID() {
		t.Fatalf("second install = (%q, updated=%v), want (%q, true)", second.ID(), updated, first.ID())
	}
	entries := manager.List()
	count := 0
	for _, entry := range entries {
		if entry.ID() == first.ID() {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("installed copies of %q = %d, want 1", first.ID(), count)
	}
}

func TestManagerRejectsMissingID(t *testing.T) {
	src := t.TempDir()
	writeFakePlugin(t, src)
	src = filepath.Join(src, "fake")
	raw, err := os.ReadFile(filepath.Join(src, "source.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	delete(manifest, "id")
	raw, _ = json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(src, "source.json"), raw, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if _, _, err := NewManager(t.TempDir(), storage.New(t.TempDir()), nil).Install(context.Background(), src); err == nil {
		t.Fatal("install without id should fail")
	}
}

func TestManagerLaunchesInterpreterEntry(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "fake")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		SchemaVersion: 1,
		ID:            "fake",
		Entry: map[string]EntrySpec{"any": {
			Command: "/bin/sh",
			Args:    []string{"-c", "TINGLAN_TEST_PLUGIN=1 exec " + exe},
		}},
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(root, storage.New(t.TempDir()), nil)
	registry := source.NewRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := manager.Enable(ctx, registry, "fake"); err != nil {
		t.Fatalf("enable interpreter entry: %v", err)
	}
	src, ok := registry.Source("fake")
	if !ok {
		t.Fatal("source not registered")
	}
	if desc := src.Describe(); desc.Name == "" {
		t.Fatalf("descriptor = %+v", desc)
	}
	manager.Disable(registry, "fake")
}
