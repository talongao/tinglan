package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"tinglan/native/internal/huawei"
	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
)

const sessionFile = "session.local.json"

// The key lives beside the ciphertext. This is local storage, not protection
// against programs running as the same user; filesystem permissions are the boundary.
type localSession struct {
	Version int    `json:"version"`
	Key     []byte `json:"key"`
	Data    []byte `json:"data"`
}

func (s *Store) SaveSession(session *source.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := huawei.NewVaultKey()
	defer clear(key)
	blob, err := huawei.SealSession(huawei.ProviderSession(session), key)
	if err != nil {
		return err
	}
	if err := s.privateSessionDir(); err != nil {
		return err
	}
	// One atomic document prevents mismatched key/session pairs after a crash.
	return s.write(sessionFile, localSession{Version: 1, Key: key, Data: blob})
}

func (s *Store) privateSessionDir() error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(s.dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid session directory")
	}
	return os.Chmod(s.dir, 0700)
}

func (s *Store) LoadSession() (*source.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, sessionFile)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return nil, errors.New("invalid local session file")
	}
	if err := s.privateSessionDir(); err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if err != nil {
		return nil, err
	}
	defer clear(data)
	var doc localSession
	if len(data) > 64*1024 || json.Unmarshal(data, &doc) != nil || doc.Version != 1 || len(doc.Key) != 32 {
		return nil, errors.New("invalid local session document")
	}
	defer clear(doc.Key)
	session, err := huawei.OpenSession(doc.Data, doc.Key)
	if err != nil {
		return nil, err
	}
	return huawei.NeutralSession(session), nil
}

func (s *Store) DeleteSession() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Retire the old encrypted file too, without accessing its keychain key.
	var result error
	for _, name := range []string{sessionFile, "session.enc"} {
		if err := os.Remove(filepath.Join(s.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	return result
}
func (s *Store) Account(key string) *Store {
	sum := sha256.Sum256([]byte(key))
	return New(filepath.Join(s.dir, "accounts", hex.EncodeToString(sum[:])))
}
func (s *Store) SaveLibrary(tracks []music.Track) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write("library.json", tracks)
}
func (s *Store) LoadLibrary() ([]music.Track, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var tracks []music.Track
	err := s.read("library.json", &tracks)
	return tracks, err
}
func (s *Store) SaveHistory(tracks []music.Track) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write("history.json", tracks)
}
func (s *Store) LoadHistory() ([]music.Track, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var tracks []music.Track
	err := s.read("history.json", &tracks)
	return tracks, err
}
func (s *Store) ClearPersonal() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range []string{"queue.json", "library.json", "history.json"} {
		if err := os.Remove(filepath.Join(s.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return s.clearCollections()
}

func (s *Store) SetCreationPending(value bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write("creation-pending.json", value)
}
func (s *Store) CreationPending() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var value bool
	err := s.read("creation-pending.json", &value)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return value, err
}

func (s *Store) LoadCreationIntent() (*huawei.CreationIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var intent *huawei.CreationIntent
	err := s.read("creation-intent.json", &intent)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return intent, err
}
func (s *Store) SaveCreationIntent(intent *huawei.CreationIntent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write("creation-intent.json", intent)
}
