package plugin

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestZip writes an archive whose entries are name→content pairs.
func writeTestZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
}

func TestExtractZipKeepsLayoutAndFindsRoot(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "plugin.zip")
	writeTestZip(t, archive, map[string]string{
		"source.json":    `{"schemaVersion":1,"id":"demo"}`,
		"bin/run":        "#!/bin/sh\n",
		"lib/nested.txt": "hello",
		"README.md":      "demo",
	})
	dest := filepath.Join(dir, "extracted")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ExtractZip(archive, dest); err != nil {
		t.Fatalf("ExtractZip: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "run")); err != nil {
		t.Fatalf("nested file missing: %v", err)
	}
	root, err := PluginRoot(dest)
	if err != nil {
		t.Fatalf("PluginRoot: %v", err)
	}
	if root != dest {
		t.Fatalf("PluginRoot = %q, want %q", root, dest)
	}
}

func TestPluginRootUnwrapsSingleDirectory(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "inner")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, ManifestFile), []byte(`{"schemaVersion":1,"id":"demo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := PluginRoot(dir)
	if err != nil {
		t.Fatalf("PluginRoot: %v", err)
	}
	if root != nested {
		t.Fatalf("PluginRoot = %q, want %q", root, nested)
	}
}

func TestPluginRootRejectsSeveralPlugins(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a", "b"} {
		pluginDir := filepath.Join(dir, name)
		if err := os.MkdirAll(pluginDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pluginDir, ManifestFile), []byte(`{"schemaVersion":1,"id":"`+name+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := PluginRoot(dir); err == nil || !strings.Contains(err.Error(), "多个插件") {
		t.Fatalf("PluginRoot error = %v, want 多个插件", err)
	}
}

func TestExtractZipRejectsUnsafeEntries(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"parent traversal", "../escape"},
		{"nested traversal", "bin/../../escape"},
		{"absolute path", "/tmp/escape"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			archive := filepath.Join(dir, "plugin.zip")
			writeTestZip(t, archive, map[string]string{tc.path: "x"})
			dest := filepath.Join(dir, "extracted")
			if err := os.MkdirAll(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			err := ExtractZip(archive, dest)
			if err == nil {
				t.Fatalf("ExtractZip accepted %q", tc.path)
			}
			if !strings.Contains(err.Error(), "不安全") {
				t.Fatalf("error = %v, want 不安全", err)
			}
		})
	}
}

func TestExtractZipRejectsEmptyArchive(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "plugin.zip")
	writeTestZip(t, archive, map[string]string{})
	if err := ExtractZip(archive, dir); err == nil || !strings.Contains(err.Error(), "空") {
		t.Fatalf("error = %v, want 空", err)
	}
}
