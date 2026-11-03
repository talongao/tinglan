package storage

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

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
type SourceSetting struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

// JevSettings is the optional same-recording enhancement. The API key is not
// stored here; it lives in the local secret file.
type JevSettings struct {
	Enabled   bool     `json:"enabled"`
	Endpoint  string   `json:"endpoint,omitempty"`
	Model     string   `json:"model,omitempty"`
	Threshold float64  `json:"threshold,omitempty"`
	Models    []string `json:"models,omitempty"`
}

type Settings struct {
	Appearance       Appearance      `json:"appearance"`
	Volume           float64         `json:"volume"`
	Quality          string          `json:"quality"`
	RepeatMode       string          `json:"repeatMode"`
	Shuffle          bool            `json:"shuffle"`
	CloseToTray      bool            `json:"closeToTray"`
	SyncHistory      bool            `json:"syncHistory"`
	ThirdPartyLyrics bool            `json:"thirdPartyLyrics"`
	Sources          []SourceSetting `json:"sources,omitempty"`
	Jev              JevSettings     `json:"jev,omitempty"`
}

func Defaults() Settings {
	return Settings{
		Appearance: Appearance{Accent: "red", PanelOpacity: 1, CornerRadius: 11, FontScale: 1},
		Volume:     .85, Quality: "standard", RepeatMode: "off", CloseToTray: true, ThirdPartyLyrics: true,
		Sources: []SourceSetting{{ID: "huawei", Enabled: true}},
		Jev:     JevSettings{Endpoint: "https://api.typesafe.ai", Threshold: .80},
	}
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
	sanitizeSources(&s.Sources)
	sanitizeJev(&s.Jev)
}

var sourceIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func sanitizeSources(sources *[]SourceSetting) {
	seen := make(map[string]bool)
	out := make([]SourceSetting, 0, len(*sources)+1)
	for _, source := range *sources {
		if !sourceIDPattern.MatchString(source.ID) || seen[source.ID] {
			continue
		}
		seen[source.ID] = true
		out = append(out, source)
	}
	if !seen["huawei"] {
		out = append(out, SourceSetting{ID: "huawei", Enabled: true})
	}
	if len(out) > 8 {
		out = out[:8]
	}
	*sources = out
}

func validModelID(model string) bool {
	if model == "" || len(model) > 200 {
		return false
	}
	for _, r := range model {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// normalizeJevEndpoint accepts https and, for self-hosted deployments, http.
// A base may be the service root or an OpenAI-style /v1 or /v1/systemone URL.
func normalizeJevEndpoint(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "https" && scheme != "http") || parsed.Host == "" || parsed.User != nil {
		return "", false
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || len(raw) > 300 {
		return "", false
	}
	path := strings.TrimRight(parsed.Path, "/")
	path = strings.TrimSuffix(path, "/v1/systemone")
	path = strings.TrimSuffix(path, "/v1")
	path = strings.TrimSuffix(path, "/systemone")
	parsed.Scheme = scheme
	parsed.Path = path
	return parsed.String(), true
}

func sanitizeJev(jev *JevSettings) {
	endpoint, ok := normalizeJevEndpoint(jev.Endpoint)
	if !ok {
		endpoint = "https://api.typesafe.ai"
	}
	jev.Endpoint = endpoint
	if !validModelID(strings.TrimSpace(jev.Model)) {
		jev.Model = ""
	}
	if math.IsNaN(jev.Threshold) || jev.Threshold < .5 || jev.Threshold > .99 {
		jev.Threshold = .80
	}
	seen := make(map[string]bool)
	out := make([]string, 0, len(jev.Models))
	for _, model := range jev.Models {
		model = strings.TrimSpace(model)
		if !validModelID(model) || seen[model] {
			continue
		}
		seen[model] = true
		out = append(out, model)
		if len(out) == 50 {
			break
		}
	}
	jev.Models = out
}

var accentPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Store owns only the explicit native profile. It never opens or
// imports old Electron data. JSON is replaced atomically under a process-local lock.
type Store struct {
	dir string
	mu  sync.Mutex
}

func New(dir string) *Store { return &Store{dir: dir} }

// Dir returns the profile directory that owns this store.
func (s *Store) Dir() string { return s.dir }
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
