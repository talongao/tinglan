package storage

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"tinglan/native/internal/music"
)

func TestSettingsQueueRestartAndClear(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	settings := Defaults()
	settings.Appearance.Accent = "blue"
	settings.Appearance.CornerRadius = 999
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveQueue(SavedQueue{Tracks: []music.Track{{ID: "fixture", Title: "Synthetic", CoverURL: "file:///private"}}, Position: 13}); err != nil {
		t.Fatal(err)
	}
	restarted := New(dir)
	value, err := restarted.LoadSettings()
	if err != nil || value.Appearance.Accent != "blue" || value.Appearance.CornerRadius != 18 {
		t.Fatal("settings restore")
	}
	queue, err := restarted.LoadQueue()
	if err != nil || queue.Position != 13 || queue.Tracks[0].CoverURL != "" {
		t.Fatal("queue restore")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "queue.json"))
	if strings.Contains(string(data), "file:") {
		t.Fatal("unsafe URL persisted")
	}
	if err := s.ClearQueue(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadSettings(); err != nil {
		t.Fatal("logout cleared settings")
	}
}
func TestCorruptProfileIsPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte("broken"), 0600)
	s := New(dir)
	value, err := s.LoadSettings()
	if err == nil || value.Appearance.Accent != "red" {
		t.Fatal("corrupt settings not reported")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "broken" {
		t.Fatal("corrupt document was overwritten during read")
	}
}

func TestSanitizeSourcesAndJev(t *testing.T) {
	settings := Defaults()
	settings.Sources = []SourceSetting{
		{ID: "huawei", Enabled: false},
		{ID: "huawei", Enabled: true},
		{ID: "Bad ID", Enabled: true},
		{ID: "netease", Enabled: true},
	}
	settings.Jev = JevSettings{
		Enabled:   true,
		Endpoint:  "http://insecure.example.com/",
		Model:     "  jev-latest  ",
		Threshold: 1.7,
		Models:    []string{"jev-1.13.0", "jev-1.13.0", "bad/id", ""},
	}
	settings.Sanitize()
	if len(settings.Sources) != 2 || settings.Sources[0].ID != "huawei" || settings.Sources[1].ID != "netease" {
		t.Fatalf("sources = %+v", settings.Sources)
	}
	if settings.Sources[0].Enabled {
		t.Fatal("duplicate source did not keep the first value")
	}
	if settings.Jev.Endpoint != "http://insecure.example.com" {
		t.Fatalf("endpoint = %q", settings.Jev.Endpoint)
	}
	if settings.Jev.Threshold != .80 {
		t.Fatalf("threshold = %v", settings.Jev.Threshold)
	}
	if len(settings.Jev.Models) != 2 || settings.Jev.Models[0] != "jev-1.13.0" || settings.Jev.Models[1] != "bad/id" {
		t.Fatalf("models = %+v", settings.Jev.Models)
	}
	if settings.Jev.Model != "  jev-latest  " {
		t.Fatalf("model should remain opaque, got %q", settings.Jev.Model)
	}
}

func TestSecretRoundTrip(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.LoadSecret("jev"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing secret err = %v", err)
	}
	if err := store.SaveSecret("jev", []byte("sk-test")); err != nil {
		t.Fatal(err)
	}
	value, err := store.LoadSecret("jev")
	if err != nil || string(value) != "sk-test" {
		t.Fatalf("value = %q, %v", value, err)
	}
	raw, err := os.ReadFile(filepath.Join(store.Dir(), "secrets.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("sk-test")) {
		t.Fatal("secret stored in plaintext")
	}
	if err := store.DeleteSecret("jev"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadSecret("jev"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted secret err = %v", err)
	}
}
