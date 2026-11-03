package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"tinglan/native/internal/huawei"
)

func syntheticSession(t *testing.T) *huawei.Session {
	t.Helper()
	key := huawei.NewVaultKey()
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, aead.NonceSize())
	blob := aead.Seal(nonce, nonce, []byte(`{"Token":"synthetic","User":"fixture","Expiry":""}`), []byte("hwmusic-viva-session-v1"))
	session, err := huawei.OpenSession(blob, key)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestLocalSessionRestartPermissionsAndLogout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profile")
	s := New(dir)
	session := syntheticSession(t)
	if err := s.SaveSession(session); err != nil {
		t.Fatal(err)
	}
	restored, err := New(dir).LoadSession()
	if err != nil || restored.AccountKey() != session.AccountKey() {
		t.Fatal("restart failed", err)
	}
	if runtime.GOOS != "windows" {
		for name, mode := range map[string]os.FileMode{dir: 0700, filepath.Join(dir, sessionFile): 0600} {
			info, err := os.Stat(name)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatal("incorrect permissions", name, err)
			}
		}
	}
	// A failed save must preserve the last valid document.
	if err := s.SaveSession(nil); err == nil {
		t.Fatal("invalid session saved")
	}
	if _, err := New(dir).LoadSession(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.enc"), []byte("old synthetic ciphertext"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{sessionFile, "session.enc"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("logout left credentials", err)
		}
	}
	if err := s.DeleteSession(); err != nil {
		t.Fatal("logout not idempotent", err)
	}
}

func TestLocalSessionIgnoresLegacyAndRejectsInvalidData(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := os.WriteFile(filepath.Join(dir, "session.enc"), []byte("synthetic legacy ciphertext"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadSession(); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("legacy session was imported", err)
	}
	if err := s.SaveSession(syntheticSession(t)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, sessionFile))
	var doc localSession
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	doc.Data[len(doc.Data)-1] ^= 1
	bad, _ := json.Marshal(doc)
	for _, data := range [][]byte{bad, []byte(`{"version":9}`), []byte("broken"), make([]byte, 65537)} {
		if err := os.WriteFile(filepath.Join(dir, sessionFile), data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LoadSession(); err == nil {
			t.Fatal("invalid document accepted")
		}
	}
	if runtime.GOOS != "windows" {
		path := filepath.Join(dir, sessionFile)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "session.enc"), path); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LoadSession(); err == nil {
			t.Fatal("symlink accepted")
		}
	}
}
