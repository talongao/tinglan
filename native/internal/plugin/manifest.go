// Package plugin hosts external music sources that speak the JSON-RPC source
// protocol over stdio. The host never trusts a plugin with provider payloads:
// everything crossing the boundary is the neutral contract in internal/source.
package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"tinglan/native/internal/source"
)

// Manifest is the on-disk source.json. It stays deliberately small: the host
// discovers display metadata and capabilities from the plugin during the
// initialize handshake, then caches them in descriptor.json so a disabled
// plugin still renders in settings.
type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	Entry         map[string]string `json:"entry"`
	// ID is only honoured for plugins shipped inside the app bundle, where the
	// identity must stay stable across upgrades. Imported plugins get an id
	// assigned by the host, so user-supplied manifests cannot collide.
	ID      string `json:"id,omitempty"`
	Builtin bool   `json:"builtin,omitempty"`
	// Digest fingerprints the files copied at install time so importing the
	// same plugin twice is detected before anything is written.
	Digest string `json:"digest,omitempty"`
}

// DescriptorFile is the host-written cache of the plugin descriptor.
const DescriptorFile = "descriptor.json"

// ManifestFile is the plugin manifest shipped inside the plugin directory.
const ManifestFile = "source.json"

// LoadManifest reads and validates source.json from dir.
func LoadManifest(dir string) (Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("source.json: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate checks the fields the host relies on before launching anything.
func (m Manifest) Validate() error {
	if m.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schemaVersion %d", m.SchemaVersion)
	}
	if m.ID != "" && !validID(m.ID) {
		return fmt.Errorf("invalid plugin id %q", m.ID)
	}
	if len(m.Entry) == 0 {
		return errors.New("missing entry")
	}
	for _, path := range m.Entry {
		if !insideDir(path) {
			return fmt.Errorf("entry %q escapes the plugin directory", path)
		}
	}
	return nil
}

// LoadDescriptor reads the host-written descriptor cache. A missing cache is
// not an error: it only means the plugin has not run on this machine yet.
func LoadDescriptor(dir string) (source.Descriptor, error) {
	raw, err := os.ReadFile(filepath.Join(dir, DescriptorFile))
	if err != nil {
		return source.Descriptor{}, err
	}
	var descriptor source.Descriptor
	if err := json.Unmarshal(raw, &descriptor); err != nil {
		return source.Descriptor{}, fmt.Errorf("descriptor.json: %w", err)
	}
	return descriptor, nil
}

// SaveDescriptor writes the descriptor cache. It is host-owned: plugins never
// write here, so a stopped plugin cannot impersonate another one.
func SaveDescriptor(dir string, descriptor source.Descriptor) error {
	raw, err := json.MarshalIndent(descriptor, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, DescriptorFile+".tmp")
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, DescriptorFile))
}

// EntryPath resolves the executable for the current platform. macOS arm64 and
// amd64 map to darwin-arm64/darwin-x64; Windows uses windows-x64.
func (m Manifest) EntryPath() (string, bool) {
	path, ok := m.Entry[PlatformKey()]
	if !ok {
		return "", false
	}
	return path, true
}

// PlatformKey is the manifest entry key for the running host.
func PlatformKey() string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	return runtime.GOOS + "-" + arch
}

func validID(id string) bool {
	if id == "" || len(id) > 40 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// insideDir rejects absolute paths and anything that walks out of the plugin
// directory. Symlinks are resolved by the caller before launching.
func insideDir(path string) bool {
	if path == "" || filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(path)
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

// SaveManifest writes the host-owned copy of a plugin manifest.
func SaveManifest(dir string, manifest Manifest) error {
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ManifestFile), append(raw, '\n'), 0o600)
}
