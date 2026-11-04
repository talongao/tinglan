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
	SchemaVersion int                  `json:"schemaVersion"`
	Entry         map[string]EntrySpec `json:"entry"`
	// ID is the plugin's stable identity, maintained by its author. The host
	// uses it to tell an update from a different plugin: importing a manifest
	// with an id that is already installed replaces that copy instead of
	// creating a second one.
	ID string `json:"id"`
	// Version is for display and update prompts only; identity stays the id.
	Version string `json:"version,omitempty"`
	Builtin bool   `json:"builtin,omitempty"`
}

// EntrySpec describes how to launch a plugin on one platform. It accepts both
// the MCP-style object form and the legacy plain string form:
//
//	"darwin-arm64": "bin/huawei-source"
//	"any":          {"command": "node", "args": ["main.mjs"]}
//
// When Args is empty and Command is relative, the command is resolved inside
// the plugin directory. With Args set, Command is looked up on PATH (or used
// as-is when absolute) and relative args that exist inside the plugin
// directory are resolved there.
type EntrySpec struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Interpreter reports whether the entry launches a separate runtime.
func (e EntrySpec) Interpreter() bool { return len(e.Args) > 0 }

func (e *EntrySpec) UnmarshalJSON(raw []byte) error {
	var command string
	if err := json.Unmarshal(raw, &command); err == nil {
		e.Command = command
		e.Args = nil
		e.Env = nil
		return nil
	}
	type plain EntrySpec
	var spec plain
	if err := json.Unmarshal(raw, &spec); err != nil {
		return fmt.Errorf("entry: %w", err)
	}
	*e = EntrySpec(spec)
	return nil
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
	if !validID(m.ID) {
		return fmt.Errorf("source.json 缺少合法的 id（%q）", m.ID)
	}
	if len(m.Entry) == 0 {
		return errors.New("missing entry")
	}
	for _, spec := range m.Entry {
		if strings.TrimSpace(spec.Command) == "" {
			return errors.New("entry command is empty")
		}
		if !spec.Interpreter() && !insideDir(spec.Command) {
			return fmt.Errorf("entry %q escapes the plugin directory", spec.Command)
		}
		for _, arg := range spec.Args {
			if filepath.IsAbs(arg) {
				return fmt.Errorf("entry arg %q must be relative", arg)
			}
			if strings.Contains(arg, "..") {
				return fmt.Errorf("entry arg %q escapes the plugin directory", arg)
			}
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
// EntryFor returns the launch spec for the current platform, falling back to
// a platform-independent "any" entry.
func (m Manifest) EntryFor() (EntrySpec, bool) {
	if spec, ok := m.Entry[PlatformKey()]; ok {
		return spec, true
	}
	spec, ok := m.Entry["any"]
	return spec, ok
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
