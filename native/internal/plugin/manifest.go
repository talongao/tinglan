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
)

// Manifest is the on-disk source.json. It also carries the display metadata
// used before the process starts, so a disabled or broken plugin still renders.
type Manifest struct {
	SchemaVersion   int               `json:"schemaVersion"`
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	ShortName       string            `json:"shortName"`
	Accent          string            `json:"accent"`
	Icon            string            `json:"icon"`
	Entry           map[string]string `json:"entry"`
	Protocol        ProtocolRange     `json:"protocol"`
	Capabilities    Capabilities      `json:"capabilities"`
	HostServices    []string          `json:"hostServices"`
	ResourceDomains []string          `json:"resourceDomains"`
	Digest          Digest            `json:"digest,omitempty"`
	// Builtin marks a source shipped with the app. The host enables it on first
	// run; the user may still disable it afterwards.
	Builtin bool `json:"builtin,omitempty"`
}

// ProtocolRange is the wire protocol range the plugin supports.
type ProtocolRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Digest records expected file hashes. It is verification input, not trust.
type Digest struct {
	Algorithm string            `json:"algorithm"`
	Files     map[string]string `json:"files"`
}

// Hashes returns the declared file digests.
func (d Digest) Hashes() map[string]string { return d.Files }

// Capabilities mirrors the contract. Runtime values must be a subset.
type Capabilities struct {
	Search         bool         `json:"search"`
	Playlists      bool         `json:"playlists"`
	PlaylistDetail bool         `json:"playlistDetail"`
	AlbumDetail    bool         `json:"albumDetail"`
	ArtistDetail   bool         `json:"artistDetail"`
	History        bool         `json:"history"`
	Lyrics         bool         `json:"lyrics"`
	Import         bool         `json:"import"`
	Resolve        bool         `json:"resolve"`
	LoginKinds     []string     `json:"loginKinds,omitempty"`
	LoginRequired  []string     `json:"loginRequired,omitempty"`
	Collections    []Collection `json:"collections,omitempty"`
	MaxPageSize    int          `json:"maxPageSize,omitempty"`
	Qualities      []string     `json:"qualities,omitempty"`
}

// Collection is one importable list declared by the plugin.
type Collection struct {
	Kind         string `json:"kind"`
	ID           string `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	PlaylistKind string `json:"playlistKind,omitempty"`
}

// LoadManifest reads and validates source.json from dir.
func LoadManifest(dir string) (Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "source.json"))
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
	if !validID(m.ID) {
		return fmt.Errorf("invalid plugin id %q", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("missing name")
	}
	if m.Protocol.Min <= 0 || m.Protocol.Max < m.Protocol.Min {
		return fmt.Errorf("invalid protocol range %d-%d", m.Protocol.Min, m.Protocol.Max)
	}
	if len(m.Entry) == 0 {
		return errors.New("missing entry")
	}
	for _, path := range m.Entry {
		if !insideDir(path) {
			return fmt.Errorf("entry %q escapes the plugin directory", path)
		}
	}
	if icon := m.Icon; icon != "" && !insideDir(icon) {
		return fmt.Errorf("icon %q escapes the plugin directory", icon)
	}
	return nil
}

// EntryPath resolves the executable for the current platform. macOS arm64 and
// amd64 map to darwin-arm64/darwin-x64; Windows uses windows-x64.
func (m Manifest) EntryPath() (string, bool) {
	key := PlatformKey()
	path, ok := m.Entry[key]
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
