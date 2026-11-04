package plugin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
	"tinglan/native/internal/storage"
)

// Entry describes one discovered plugin on disk.
type Entry struct {
	Manifest Manifest
	// Descriptor is the host cache written the last time the plugin ran. It
	// supplies the display name and capabilities while the plugin is stopped.
	Descriptor source.Descriptor
	Dir        string
	Enabled    bool
	Running    bool
	Err        string
}

// ID is the plugin's assigned source id.
func (e Entry) ID() music.SourceID { return e.Descriptor.ID }

// Name is the display name reported by the plugin.
func (e Entry) Name() string {
	if e.Descriptor.Name != "" {
		return e.Descriptor.Name
	}
	return string(e.Descriptor.ID)
}

// LoginKinds reports which login methods the plugin declared.
func (e Entry) LoginKinds() []source.LoginKind { return e.Descriptor.Capabilities.LoginKinds }

// Manager discovers, launches and stops external source plugins.
type Manager struct {
	roots  []string
	store  *storage.Store
	logger source.Logger

	mu      sync.Mutex
	plugins map[music.SourceID]*Plugin
	enabled map[music.SourceID]bool
}

// NewManager prepares the plugin root; plugins live under <app data>/sources.
func NewManager(root string, store *storage.Store, logger source.Logger) *Manager {
	return &Manager{
		roots:   []string{root},
		store:   store,
		logger:  logger,
		plugins: map[music.SourceID]*Plugin{},
		enabled: map[music.SourceID]bool{},
	}
}

// Root returns the directory users install plugins into.
func (m *Manager) Root() string { return m.roots[0] }

// AddRoot adds a read-only search root such as the app bundle's plugins. Earlier
// roots win when two roots ship the same source id.
func (m *Manager) AddRoot(dir string) {
	if dir == "" {
		return
	}
	for _, existing := range m.roots {
		if existing == dir {
			return
		}
	}
	m.roots = append(m.roots, dir)
}

// List scans the plugin root for source.json files.
func (m *Manager) List() []Entry {
	m.mu.Lock()
	enabled := make(map[music.SourceID]bool, len(m.enabled))
	for id, on := range m.enabled {
		enabled[id] = on
	}
	running := make(map[music.SourceID]bool, len(m.plugins))
	for id := range m.plugins {
		running[id] = true
	}
	m.mu.Unlock()

	var out []Entry
	seen := map[music.SourceID]bool{}
	for _, root := range m.roots {
		items, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, item := range items {
			if !item.IsDir() {
				continue
			}
			dir := filepath.Join(root, item.Name())
			manifest, loadErr := LoadManifest(dir)
			if loadErr != nil {
				continue
			}
			descriptor, _ := LoadDescriptor(dir)
			if descriptor.ID == "" {
				descriptor.ID = music.SourceID(manifest.ID)
			}
			if descriptor.ID == "" {
				descriptor.ID = music.SourceID(item.Name())
			}
			if descriptor.Name == "" {
				descriptor.Name = string(descriptor.ID)
			}
			id := descriptor.ID
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, Entry{
				Manifest:   manifest,
				Descriptor: descriptor,
				Dir:        dir,
				Enabled:    enabled[id],
				Running:    running[id],
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// Enable launches a plugin and registers it. The caller is responsible for
// showing the consent prompt before calling this.
func (m *Manager) Enable(ctx context.Context, registry *source.Registry, id music.SourceID) error {
	m.mu.Lock()
	runningPlugin := m.plugins[id]
	m.mu.Unlock()
	if runningPlugin != nil {
		// A fresh registry (e.g. the library reopened after login) still has to
		// learn about a plugin that keeps running.
		return registry.RegisterSource(runningPlugin)
	}
	entry, ok := m.find(id)
	if !ok {
		return fmt.Errorf("plugin %q not installed", id)
	}
	plugin, err := m.start(ctx, entry)
	if err != nil {
		return err
	}
	if err := registry.RegisterSource(plugin); err != nil {
		_ = plugin.Close()
		return err
	}
	m.mu.Lock()
	m.plugins[id] = plugin
	m.enabled[id] = true
	m.mu.Unlock()
	m.saveEnabled()
	return nil
}

// Disable stops a plugin and unregisters it. Local library bindings are kept.
func (m *Manager) Disable(registry *source.Registry, id music.SourceID) error {
	m.mu.Lock()
	plugin := m.plugins[id]
	delete(m.plugins, id)
	delete(m.enabled, id)
	m.mu.Unlock()
	if plugin != nil {
		registry.UnregisterSource(id)
		_ = plugin.Close()
	}
	m.saveEnabled()
	return nil
}

// LoadEnabled starts every previously enabled plugin and registers it.
func (m *Manager) LoadEnabled(ctx context.Context, registry *source.Registry) []string {
	m.loadEnabledState()
	// Sources shipped with the app start once, until the user disables them.
	for _, entry := range m.List() {
		id := entry.ID()
		if !entry.Manifest.Builtin {
			continue
		}
		if _, decided := m.enabled[id]; !decided {
			m.enabled[id] = true
		}
	}
	var failures []string
	for _, id := range m.enabledIDs() {
		if err := m.Enable(ctx, registry, id); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", id, err))
			m.mu.Lock()
			delete(m.enabled, id)
			m.mu.Unlock()
		}
	}
	if len(failures) > 0 {
		m.saveEnabled()
	}
	return failures
}

// Close stops every running plugin.
func (m *Manager) Close() {
	m.mu.Lock()
	plugins := make([]*Plugin, 0, len(m.plugins))
	for _, plugin := range m.plugins {
		plugins = append(plugins, plugin)
	}
	m.plugins = map[music.SourceID]*Plugin{}
	m.mu.Unlock()
	for _, plugin := range plugins {
		_ = plugin.Close()
	}
}

// Running reports the number of live plugin processes.
func (m *Manager) Running() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.plugins)
}

func (m *Manager) find(id music.SourceID) (Entry, bool) {
	for _, entry := range m.List() {
		if entry.ID() == id {
			return entry, true
		}
	}
	return Entry{}, false
}

func (m *Manager) start(ctx context.Context, entry Entry) (*Plugin, error) {
	manifest := entry.Manifest
	relative, ok := manifest.EntryPath()
	if !ok {
		return nil, fmt.Errorf("插件 %s 不支持当前平台（%s）", entry.ID(), PlatformKey())
	}
	binary, err := secureJoin(entry.Dir, relative)
	if err != nil {
		return nil, err
	}
	if binary, err = filepath.Abs(binary); err != nil {
		return nil, err
	}
	if info, err := os.Stat(binary); err != nil || info.IsDir() {
		return nil, fmt.Errorf("插件入口不存在：%s", relative)
	}
	host := NewHost(entry.ID(), m.store, m.logger)
	proc, err := StartProcess(ctx, entry.Dir, binary, nil, nil, host)
	if err != nil {
		return nil, fmt.Errorf("启动插件失败：%w", err)
	}
	plugin, err := NewPlugin(ctx, manifest, entry.ID(), proc, host)
	if err != nil {
		_ = proc.Close()
		if tail := proc.Stderr(); tail != "" {
			return nil, fmt.Errorf("%w（%s）", err, firstLine(tail))
		}
		return nil, err
	}
	return plugin, nil
}

// secureJoin resolves a manifest-relative path and rejects symlinks escaping
// the plugin directory.
func secureJoin(dir, relative string) (string, error) {
	if !insideDir(relative) {
		return "", errors.New("entry escapes the plugin directory")
	}
	path := filepath.Join(dir, relative)
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || len(rel) > 2 && rel[:3] == ".."+string(filepath.Separator) {
		return "", errors.New("entry escapes the plugin directory")
	}
	return resolved, nil
}

func firstLine(text string) string {
	for i, r := range text {
		if r == '\n' {
			return text[:i]
		}
	}
	return text
}

const enabledSecret = "plugin.enabled"

func (m *Manager) loadEnabledState() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = map[music.SourceID]bool{}
	raw, err := m.store.LoadSecret(enabledSecret)
	if err != nil {
		return
	}
	var ids []string
	if json.Unmarshal(raw, &ids) != nil {
		return
	}
	for _, id := range ids {
		m.enabled[music.SourceID(id)] = true
	}
}

func (m *Manager) enabledIDs() []music.SourceID {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]music.SourceID, 0, len(m.enabled))
	for id := range m.enabled {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func (m *Manager) saveEnabled() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.enabled))
	for id := range m.enabled {
		ids = append(ids, string(id))
	}
	m.mu.Unlock()
	sort.Strings(ids)
	raw, err := json.Marshal(ids)
	if err != nil {
		return
	}
	_ = m.store.SaveSecret(enabledSecret, raw)
}

// SeedSession writes one value into a plugin's credential namespace. It exists
// only for the built-in source upgrade path, where a session saved by an older
// build has to move into the plugin's namespace before its first start.
// The value uses the same base64 form as host.session.set, because plugins
// decode whatever host.session.get returns.
func (m *Manager) SeedSession(id music.SourceID, key, value string) error {
	if m.store == nil {
		return errors.New("plugin session storage unavailable")
	}
	host := NewHost(id, m.store, m.logger)
	doc, err := host.load()
	if err != nil {
		return err
	}
	if _, exists := doc.Entries[key]; exists {
		return nil
	}
	doc.Entries[key] = sessionEntry{Value: value, Revision: 1}
	return host.save(doc)
}

// ClearSession removes a plugin's stored session values. Used when the user
// logs out: the source cannot erase the host namespace itself.
func (m *Manager) ClearSession(id music.SourceID) error {
	if m.store == nil {
		return nil
	}
	if err := m.store.DeleteSecret("plugin.session." + string(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Probe launches a plugin from dir just long enough to read the descriptor it
// reports during initialize. It is used when importing a plugin, before the
// host decides where the plugin lives.
func (m *Manager) Probe(ctx context.Context, dir string) (source.Descriptor, error) {
	manifest, err := LoadManifest(dir)
	if err != nil {
		return source.Descriptor{}, err
	}
	relative, ok := manifest.EntryPath()
	if !ok {
		return source.Descriptor{}, fmt.Errorf("插件不支持当前平台（%s）", PlatformKey())
	}
	binary, err := secureJoin(dir, relative)
	if err != nil {
		return source.Descriptor{}, err
	}
	host := NewHost("", m.store, m.logger)
	proc, err := StartProcess(ctx, dir, binary, nil, nil, host)
	if err != nil {
		return source.Descriptor{}, fmt.Errorf("启动插件失败：%w", err)
	}
	plugin, err := NewPlugin(ctx, manifest, "", proc, host)
	if err != nil {
		_ = proc.Close()
		return source.Descriptor{}, err
	}
	defer plugin.Close()
	return plugin.Describe(), nil
}

// Install imports a plugin directory into the managed root. The host assigns
// the source id so two plugins authored on the same machine cannot collide.
// ErrDuplicate means the same plugin files are already installed.
var ErrDuplicate = errors.New("plugin already installed")

func (m *Manager) Install(ctx context.Context, srcDir string) (Entry, error) {
	srcDir, err := filepath.Abs(srcDir)
	if err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(srcDir)
	if err != nil || !info.IsDir() {
		return Entry{}, errors.New("不是有效的插件目录")
	}
	digest, err := pluginDigest(srcDir)
	if err != nil {
		return Entry{}, err
	}
	for _, existing := range m.List() {
		sum := existing.Manifest.Digest
		if sum == "" {
			// Older installs shipped without a digest; fingerprint them now so a
			// copy of the same directory is still recognised as a duplicate.
			if computed, err := pluginDigest(existing.Dir); err == nil {
				sum = computed
				existing.Manifest.Digest = computed
				_ = SaveManifest(existing.Dir, existing.Manifest)
			}
		}
		if sum != "" && sum == digest {
			return existing, fmt.Errorf("%w：%s", ErrDuplicate, existing.Name())
		}
	}
	desc, err := m.Probe(ctx, srcDir)
	if err != nil {
		return Entry{}, err
	}
	id := m.assignID(desc.Name)
	dest := filepath.Join(m.Root(), id)
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return Entry{}, err
	}
	if err := copyPluginTree(srcDir, dest); err != nil {
		_ = os.RemoveAll(dest)
		return Entry{}, err
	}
	cached := descriptorRecord(id, desc)
	if err := SaveDescriptor(dest, cached); err != nil {
		_ = os.RemoveAll(dest)
		return Entry{}, err
	}
	manifest, err := LoadManifest(dest)
	if err != nil {
		_ = os.RemoveAll(dest)
		return Entry{}, err
	}
	// The copied manifest belongs to the host now: pin the assigned id so a
	// later plugin update cannot silently rename itself.
	manifest.ID = id
	manifest.Digest = digest
	if err := SaveManifest(dest, manifest); err != nil {
		_ = os.RemoveAll(dest)
		return Entry{}, err
	}
	return Entry{Manifest: manifest, Descriptor: cached, Dir: dest, Enabled: false}, nil
}

func descriptorRecord(id string, desc source.Descriptor) source.Descriptor {
	desc.ID = music.SourceID(id)
	desc.SchemaVersion = 1
	return desc
}

// assignID builds a readable, collision-free directory/source id.
func (m *Manager) assignID(name string) string {
	base := slug(name)
	if base == "" {
		base = "source"
	}
	existing := map[string]bool{}
	for _, entry := range m.List() {
		existing[string(entry.ID())] = true
	}
	for i := 0; i < 100; i++ {
		suffix := make([]byte, 2)
		_, _ = rand.Read(suffix)
		id := fmt.Sprintf("%s-%x", base, suffix)
		if !existing[id] {
			return id
		}
	}
	return fmt.Sprintf("%s-%d", base, time.Now().UnixNano())
}

func slug(name string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ':
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
		if b.Len() >= 24 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// copyPluginTree copies regular files, refusing overly large trees.
func copyPluginTree(src, dest string) error {
	const maxBytes = 256 << 20
	var total int64
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.Name() == DescriptorFile || d.Name() == ".DS_Store" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if total > maxBytes {
			return errors.New("插件目录过大")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if info.Mode()&0o111 != 0 {
			mode = 0o700
		}
		return os.WriteFile(target, data, mode)
	})
}

// pluginDigest hashes the plugin tree (paths and bytes) so a second import of
// the same folder is recognised before anything is copied.
func pluginDigest(dir string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == DescriptorFile || d.Name() == ".DS_Store" {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hash.Write([]byte(rel))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
