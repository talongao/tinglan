package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"tinglan/native/internal/lyrics"
)

type LyricDocument struct {
	Text, Source string
	SavedAt      time.Time
	// Lines keeps word-level timing when the source supplied it. Text stays in
	// place for plain documents so older caches keep working.
	Lines []lyrics.Line `json:"lines,omitempty"`
}

type LyricStats struct {
	Network, Local int
	Bytes          int64
}

func (s *Store) LyricsInfo() LyricStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result LyricStats
	for _, kind := range []string{"lyrics-cache", "lyrics-local"} {
		entries, _ := os.ReadDir(filepath.Join(s.dir, kind))
		for _, entry := range entries {
			if entry.IsDir() || len(entry.Name()) != 69 || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			result.Bytes += info.Size()
			if kind == "lyrics-local" {
				result.Local++
			} else {
				result.Network++
			}
		}
	}
	return result
}

func (s *Store) LyricOffset(id string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var offsets map[string]float64
	if s.read("lyric-offsets.json", &offsets) != nil {
		return 0
	}
	return clamp(offsets[lyricKey(id)], -10, 10, 0)
}

// LyricOffsetSaved reports whether an offset was stored for exactly this key.
// Callers use it to fall back to a broader key without guessing.
func (s *Store) LyricOffsetSaved(id string) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var offsets map[string]float64
	if s.read("lyric-offsets.json", &offsets) != nil {
		return 0, false
	}
	value, ok := offsets[lyricKey(id)]
	if !ok {
		return 0, false
	}
	return clamp(value, -10, 10, 0), true
}

func (s *Store) SaveLyricOffset(id string, value float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	offsets := make(map[string]float64)
	err := s.read("lyric-offsets.json", &offsets)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if offsets == nil {
		offsets = make(map[string]float64)
	}
	if len(offsets) >= 3000 {
		delete(offsets, lyricKey(id))
		if len(offsets) >= 3000 {
			return errors.New("lyric offset limit reached")
		}
	}
	offsets[lyricKey(id)] = clamp(value, -10, 10, 0)
	return s.write("lyric-offsets.json", offsets)
}

func lyricKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:]) + ".json"
}
func (s *Store) SaveLyrics(id string, doc LyricDocument, local bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(doc.Text) > 512*1024 || len(doc.Lines) > 5000 || id == "" {
		return errors.New("invalid lyric document")
	}
	doc.SavedAt = time.Now()
	kind := "lyrics-cache"
	if local {
		kind = "lyrics-local"
	}
	child := New(filepath.Join(s.dir, kind))
	child.mu.Lock()
	defer child.mu.Unlock()
	if err := child.write(lyricKey(id), doc); err != nil {
		return err
	}
	if !local {
		entries, _ := os.ReadDir(child.dir)
		if len(entries) > 3000 {
			type item struct {
				name string
				time time.Time
			}
			var files []item
			for _, e := range entries {
				if !e.IsDir() && len(e.Name()) == 69 && strings.HasSuffix(e.Name(), ".json") {
					if info, err := e.Info(); err == nil {
						files = append(files, item{e.Name(), info.ModTime()})
					}
				}
			}
			sort.Slice(files, func(i, j int) bool { return files[i].time.Before(files[j].time) })
			for i := 0; i < len(files)-3000; i++ {
				_ = os.Remove(filepath.Join(child.dir, files[i].name))
			}
		}
	}
	return nil
}
func (s *Store) LoadLyrics(id string) (LyricDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, kind := range []string{"lyrics-local", "lyrics-cache"} {
		child := New(filepath.Join(s.dir, kind))
		var doc LyricDocument
		if err := child.read(lyricKey(id), &doc); err == nil && len(doc.Text) <= 512*1024 && (kind == "lyrics-local" || time.Since(doc.SavedAt) < 90*24*time.Hour) {
			return doc, nil
		}
	}
	return LyricDocument{}, os.ErrNotExist
}
func (s *Store) RemoveLocalLyrics(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(filepath.Join(s.dir, "lyrics-local", lyricKey(id)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (s *Store) ClearLyrics(local bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kind := "lyrics-cache"
	if local {
		kind = "lyrics-local"
	}
	dir := filepath.Join(s.dir, kind)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() && len(entry.Name()) == 69 && strings.HasSuffix(entry.Name(), ".json") {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
