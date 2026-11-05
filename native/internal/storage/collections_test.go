package storage

import (
	"testing"
	"tinglan/native/internal/music"
)

func TestCollectionsAccountIsolationAndLogout(t *testing.T) {
	root := New(t.TempDir())
	a, b := root.Account("synthetic-a"), root.Account("synthetic-b")
	value := Collection{Tracks: []music.Track{{ID: "synthetic", Title: "Test"}}}
	if err := a.SaveCollection("detail", "../test", value); err != nil {
		t.Fatal(err)
	}
	if _, err := b.LoadCollection("detail", "../test"); err == nil {
		t.Fatal("account cache leaked")
	}
	got, err := a.LoadCollection("detail", "../test")
	if err != nil || len(got.Tracks) != 1 || got.SavedAt.IsZero() {
		t.Fatal("cache missing")
	}
	if err = a.ClearPersonal(); err != nil {
		t.Fatal(err)
	}
	if _, err = a.LoadCollection("detail", "../test"); err == nil {
		t.Fatal("cache survived logout")
	}
}
