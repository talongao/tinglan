package storage

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"huawei-music-desktop/native/internal/huawei"
)

type Appearance struct {
	Theme        string  `json:"theme,omitempty"`
	Accent       string  `json:"accent"`
	PanelOpacity float64 `json:"panelOpacity"`
	CornerRadius float64 `json:"cornerRadius"`
	FontScale    float64 `json:"fontScale"`
	FontFamily   string  `json:"fontFamily,omitempty"`
	CustomAccent string  `json:"customAccent,omitempty"`
}
type Settings struct {
	Appearance       Appearance `json:"appearance"`
	Volume           float64    `json:"volume"`
	Quality          string     `json:"quality"`
	RepeatMode       string     `json:"repeatMode"`
	Shuffle          bool       `json:"shuffle"`
	CloseToTray      bool       `json:"closeToTray"`
	SyncHistory      bool       `json:"syncHistory"`
	ThirdPartyLyrics bool       `json:"thirdPartyLyrics"`
}

func Defaults() Settings {
	return Settings{Appearance: Appearance{Accent: "red", PanelOpacity: 1, CornerRadius: 11, FontScale: 1}, Volume: .85, Quality: "standard", RepeatMode: "off", CloseToTray: true, ThirdPartyLyrics: true}
}
func clamp(value, low, high, fallback float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fallback
	}
	return max(low, min(high, value))
}
func (s *Settings) Sanitize() {
	switch s.Appearance.Theme {
	case "midnight", "forest", "graphite", "ocean", "sage", "sand", "rose", "ice", "wine", "coffee", "obsidian":
	default:
		s.Appearance.Theme = "light"
	}
	if s.Shuffle {
		s.RepeatMode = "off"
	}
	if len(s.Appearance.FontFamily) > 200 {
		s.Appearance.FontFamily = ""
	}
	s.Volume = clamp(s.Volume, 0, 1, .85)
	s.Appearance.PanelOpacity = clamp(s.Appearance.PanelOpacity, .55, 1, 1)
	s.Appearance.CornerRadius = clamp(s.Appearance.CornerRadius, 0, 18, 11)
	s.Appearance.FontScale = clamp(s.Appearance.FontScale, .85, 1.2, 1)
	switch s.Appearance.Accent {
	case "red", "blue", "green", "amber", "violet", "custom":
	default:
		s.Appearance.Accent = "red"
	}
	if !accentPattern.MatchString(s.Appearance.CustomAccent) {
		s.Appearance.CustomAccent = "#c8202e"
	}
	if s.Quality != "lossless" {
		s.Quality = "standard"
	}
	switch s.RepeatMode {
	case "all", "one":
	default:
		s.RepeatMode = "off"
	}
}

var accentPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type SavedQueue struct {
	Tracks   []huawei.Track `json:"tracks"`
	Index    int            `json:"index"`
	Position float64        `json:"positionSeconds"`
}

func (q *SavedQueue) Sanitize() {
	if len(q.Tracks) > 10000 {
		q.Tracks = q.Tracks[:10000]
	}
	filtered := make([]huawei.Track, 0, len(q.Tracks))
	for _, track := range q.Tracks {
		if track.ID == "" || len(track.ID) > 200 || track.Title == "" {
			continue
		}
		if !huawei.SafeResourceURL(track.CoverURL) {
			track.CoverURL = ""
		}
		filtered = append(filtered, track)
	}
	q.Tracks = filtered
	q.Index = max(0, min(q.Index, len(q.Tracks)-1))
	q.Position = clamp(q.Position, 0, 86400, 0)
}

// Store owns only the explicit native profile. It never opens or
// imports old Electron data. JSON is replaced atomically under a process-local lock.
type Store struct {
	dir string
	mu  sync.Mutex
}

func New(dir string) *Store { return &Store{dir: dir} }
func (s *Store) read(name string, out any) error {
	f, err := os.Open(filepath.Join(s.dir, name))
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 5*1024*1024+1))
	if err != nil || len(data) > 5*1024*1024 {
		return errors.New("profile document unreadable")
	}
	return json.Unmarshal(data, out)
}
func (s *Store) write(name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > 5*1024*1024 {
		return errors.New("profile document too large")
	}
	if err = os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, ".native-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp, filepath.Join(s.dir, name))
}
func (s *Store) LoadSettings() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := Defaults()
	err := s.read("settings.json", &value)
	value.Sanitize()
	return value, err
}
func (s *Store) SaveSettings(value Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value.Sanitize()
	return s.write("settings.json", value)
}
func (s *Store) LoadQueue() (SavedQueue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var value SavedQueue
	err := s.read("queue.json", &value)
	value.Sanitize()
	return value, err
}
func (s *Store) SaveQueue(value SavedQueue) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value.Sanitize()
	return s.write("queue.json", value)
}
func (s *Store) ClearQueue() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(filepath.Join(s.dir, "queue.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
