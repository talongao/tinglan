package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
	"tinglan/native/internal/storage"
)

// Entry describes one discovered plugin on disk.
type Entry struct {
	Manifest Manifest
	Dir      string
	Enabled  bool
	Running  bool
	Err      string
}

// Manager discovers, launches and stops external source plugins.
type Manager struct {
	root   string
	store  *storage.Store
	logger source.Logger

	mu      sync.Mutex
	plugins map[music.SourceID]*Plugin
	enabled map[music.SourceID]bool
}

// NewManager prepares the plugin root; plugins live under <app data>/sources.
func NewManager(root string, store *storage.Store, logger source.Logger) *Manager {
	return &Manager{
		root:    root,
		store:   store,
		logger:  logger,
		plugins: map[music.SourceID]*Plugin{},
		enabled: map[music.SourceID]bool{},
	}
}

// Root returns the directory users install plugins into.
func (m *Manager) Root() string { return m.root }

// List scans the plugin root for source.json files.
func (m *Manager) List() []Entry {
	items, err := os.ReadDir(m.root)
	if err != nil {
		return nil
	}
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
	for _, item := range items {
		if !item.IsDir() {
			continue
		}
		dir := filepath.Join(m.root, item.Name())
		manifest, loadErr := LoadManifest(dir)
		if loadErr != nil {
			continue
		}
		entry := Entry{
			Manifest: manifest,
			Dir:      dir,
			Enabled:  enabled[music.SourceID(manifest.ID)],
			Running:  running[music.SourceID(manifest.ID)],
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
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
		if entry.Manifest.ID == string(id) {
			return entry, true
		}
	}
	return Entry{}, false
}

func (m *Manager) start(ctx context.Context, entry Entry) (*Plugin, error) {
	manifest := entry.Manifest
	relative, ok := manifest.EntryPath()
	if !ok {
		return nil, fmt.Errorf("插件 %s 不支持当前平台（%s）", manifest.ID, PlatformKey())
	}
	binary, err := secureJoin(entry.Dir, relative)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(binary); err != nil || info.IsDir() {
		return nil, fmt.Errorf("插件入口不存在：%s", relative)
	}
	host := NewHost(music.SourceID(manifest.ID), m.store, m.logger)
	proc, err := StartProcess(ctx, entry.Dir, binary, nil, nil, host)
	if err != nil {
		return nil, fmt.Errorf("启动插件失败：%w", err)
	}
	plugin, err := NewPlugin(ctx, manifest, proc, host)
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
