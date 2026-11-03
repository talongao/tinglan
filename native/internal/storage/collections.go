package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"tinglan/native/internal/huawei"
)

type Collection struct {
	Playlists []huawei.Playlist
	Tracks    []huawei.Track
	SavedAt   time.Time
}

func collectionName(kind, id string) string {
	sum := sha256.Sum256([]byte(kind + ":" + id))
	return "collection-" + hex.EncodeToString(sum[:]) + ".json"
}
func (s *Store) SaveCollection(kind, id string, value Collection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value.SavedAt = time.Now()
	return s.write(collectionName(kind, id), value)
}
func (s *Store) LoadCollection(kind, id string) (Collection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var value Collection
	err := s.read(collectionName(kind, id), &value)
	return value, err
}

func (s *Store) ClearCollections() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clearCollections()
}

func (s *Store) clearCollections() error {
	paths, err := filepath.Glob(filepath.Join(s.dir, "collection-*.json"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
