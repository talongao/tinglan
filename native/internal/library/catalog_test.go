package library

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"tinglan/native/internal/music"
)

func Test965DedupRetryAndTotal(t *testing.T) {
	calls, fail := 0, true
	c := New(func(ctx context.Context, start, limit int) (music.Page, error) {
		calls++
		if start == 100 && fail {
			fail = false
			return music.Page{}, errors.New("synthetic network failure")
		}
		end := min(965, start+limit)
		p := music.Page{Next: end, More: end < 965}
		for i := start; i < end; i++ {
			p.Items = append(p.Items, music.Track{ID: fmt.Sprint(i), Title: "synthetic"})
		}
		return p, nil
	})
	if err := c.LoadAll(context.Background()); err == nil {
		t.Fatal("failure hidden")
	}
	tracks, done := c.Snapshot()
	if len(tracks) != 100 || done {
		t.Fatal("first page mistaken for total or lost on error")
	}
	if err := c.LoadAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	tracks, done = c.Snapshot()
	if len(tracks) != 965 || !done || calls != 21 {
		t.Fatalf("tracks=%d done=%v calls=%d", len(tracks), done, calls)
	}
	if err := c.LoadAll(context.Background()); err != nil || calls != 21 {
		t.Fatal("completed catalog fetched again")
	}
}

func TestResetDiscardsInFlightAndNoProgressStops(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	c := New(func(context.Context, int, int) (music.Page, error) {
		close(started)
		<-release
		return music.Page{Items: []music.Track{{ID: "old", Title: "old"}}}, nil
	})
	result := make(chan error, 1)
	go func() { result <- c.LoadNext(context.Background()) }()
	<-started
	c.Reset()
	close(release)
	if <-result == nil {
		t.Fatal("old request accepted")
	}
	tracks, done := c.Snapshot()
	if len(tracks) != 0 || done {
		t.Fatal("old account entered catalog")
	}
	c = New(func(context.Context, int, int) (music.Page, error) {
		return music.Page{Next: 50, More: true, Items: []music.Track{{ID: "same", Title: "same"}}}, nil
	})
	if err := c.LoadNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.LoadNext(context.Background()); err == nil {
		t.Fatal("no-progress page accepted")
	}
}
