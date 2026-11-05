package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
)

const secretsFile = "secrets.local.json"

// localSecrets stores named secrets under one local key, beside the ciphertext.
// Like the session vault this is local storage: filesystem permissions are the
// boundary, not protection against programs running as the same user.
type localSecrets struct {
	Version int               `json:"version"`
	Key     []byte            `json:"key"`
	Data    map[string][]byte `json:"data"`
}

func sealSecret(value, key []byte, name string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("invalid secret key")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, value, []byte("hwmusic-viva-secret-v1:"+name)), nil
}

func openSecret(blob, key []byte, name string) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("invalid secret key")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(blob) < aead.NonceSize() {
		return nil, errors.New("invalid encrypted secret")
	}
	return aead.Open(nil, blob[:aead.NonceSize()], blob[aead.NonceSize():], []byte("hwmusic-viva-secret-v1:"+name))
}

func (s *Store) loadSecrets() (localSecrets, error) {
	doc := localSecrets{Version: 1, Data: map[string][]byte{}}
	err := s.read(secretsFile, &doc)
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return doc, err
	}
	if doc.Version != 1 || len(doc.Key) != 32 || doc.Data == nil {
		return localSecrets{}, errors.New("invalid local secrets document")
	}
	return doc, nil
}

// SaveSecret stores one named secret encrypted at rest.
func (s *Store) SaveSecret(name string, value []byte) error {
	if name == "" || len(value) == 0 {
		return errors.New("invalid secret")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	doc, err := s.loadSecrets()
	if err != nil {
		return err
	}
	if len(doc.Key) != 32 {
		doc.Key = make([]byte, 32)
		if _, err := rand.Read(doc.Key); err != nil {
			return err
		}
	}
	blob, err := sealSecret(value, doc.Key, name)
	if err != nil {
		return err
	}
	doc.Data[name] = blob
	return s.write(secretsFile, doc)
}

// LoadSecret returns one named secret, or os.ErrNotExist when absent.
func (s *Store) LoadSecret(name string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.loadSecrets()
	if err != nil {
		return nil, err
	}
	blob, ok := doc.Data[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return openSecret(blob, doc.Key, name)
}

// DeleteSecret removes one named secret.
func (s *Store) DeleteSecret(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.loadSecrets()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, ok := doc.Data[name]; !ok {
		return nil
	}
	delete(doc.Data, name)
	if len(doc.Data) == 0 {
		err := os.Remove(filepath.Join(s.dir, secretsFile))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return s.write(secretsFile, doc)
}

// SecretFingerprint returns a short, non-reversible reference for a secret so
// caches can be scoped without storing the raw value.
func SecretFingerprint(value []byte) string {
	sum := sha256.Sum256(value)
	return fmtHex(sum[:8])
}

func fmtHex(data []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(data)*2)
	for i, b := range data {
		out[i*2] = digits[b>>4]
		out[i*2+1] = digits[b&0x0f]
	}
	return string(out)
}
