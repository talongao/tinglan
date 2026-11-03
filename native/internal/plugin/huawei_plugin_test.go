package plugin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"tinglan/native/internal/source"
	"tinglan/native/internal/storage"
)

// TestHuaweiSourcePluginRunsOutOfProcess builds the preinstalled source plugin
// and verifies the host can enable it and talk to it over the protocol.
func TestHuaweiSourcePluginRunsOutOfProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the plugin binary")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "huawei")
	bin := filepath.Join(dir, "bin", "huawei-source")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	moduleRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", bin, "tinglan/native/cmd/huawei-source")
	build.Dir = moduleRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build plugin: %v\n%s", err, out)
	}
	manifest := `{
  "schemaVersion": 1,
  "id": "huawei",
  "name": "华为音乐",
  "shortName": "华",
  "accent": "#C7000B",
  "entry": {"` + runtime.GOOS + `-` + archKey(runtime.GOARCH) + `": "bin/huawei-source"},
  "protocol": {"min": 1, "max": 1},
  "capabilities": {
    "search": true, "playlists": true, "playlistDetail": true,
    "albumDetail": true, "artistDetail": true, "history": true,
    "lyrics": false, "import": true, "resolve": true,
    "loginKinds": ["webview"],
    "collections": [{"kind": "favorites", "name": "收藏"}],
    "maxPageSize": 50, "qualities": ["standard", "lossless"]
  },
  "hostServices": ["session", "webview", "log"],
  "resourceDomains": [".dbankcdn.cn", ".tingmall.com", ".kuke.com"]
}`
	if err := os.WriteFile(filepath.Join(dir, "source.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(root, storage.New(t.TempDir()), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	registry := source.NewRegistry()
	if err := manager.Enable(ctx, registry, "huawei"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	defer manager.Close()
	src, ok := registry.Source("huawei")
	if !ok {
		t.Fatal("huawei source not registered")
	}
	if desc := src.Describe(); desc.Name != "华为音乐" {
		t.Fatalf("descriptor = %+v", desc)
	}
	status, err := src.Auth().Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.State != source.AuthSignedOut {
		t.Fatalf("status = %+v, want signedOut", status)
	}
	if _, err := src.Catalog().Playlists(ctx, source.PlaylistRequest{Limit: 5}); err == nil {
		t.Fatal("playlists without a session should fail")
	}
	if _, err := src.Catalog().Search(ctx, source.SearchRequest{Query: "测试", Limit: 5}); err != nil {
		t.Fatalf("anonymous search: %v", err)
	}
}

func archKey(arch string) string {
	if arch == "amd64" {
		return "x64"
	}
	return arch
}
