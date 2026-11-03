package plugin

import (
	"context"
	"os"
	"testing"
	"time"

	"tinglan/native/internal/source"
	"tinglan/native/internal/storage"
)

func TestRealSamplePluginFromProfile(t *testing.T) {
	root := os.Getenv("TINGLAN_PLUGIN_ROOT")
	if root == "" {
		t.Skip("set TINGLAN_PLUGIN_ROOT to the profile sources directory")
	}
	manager := NewManager(root, storage.New(t.TempDir()), nil)
	entries := manager.List()
	t.Logf("entries: %+v", entries)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	registry := source.NewRegistry()
	if err := manager.Enable(ctx, registry, "sample"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	defer manager.Close()
	for _, e := range manager.List() {
		t.Logf("after enable: id=%s enabled=%v running=%v err=%v", e.Manifest.ID, e.Enabled, e.Running, e.Err)
	}
	src, ok := registry.Source("sample")
	if !ok {
		t.Fatal("not registered")
	}
	t.Logf("descriptor: %+v", src.Describe())
	page, err := src.Catalog().Search(ctx, source.SearchRequest{Query: "x", Limit: 5})
	t.Logf("search err=%v items=%+v", err, page.Items)
}
