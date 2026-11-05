// Package library owns a session-scoped favorites index shared by count and UI.
package library

import (
	"context"
	"errors"
	"sync"

	"tinglan/native/internal/music"
)

type Fetch func(context.Context, int, int) (music.Page, error)
type request struct {
	done chan struct{}
	err  error
}
type Catalog struct {
	mu         sync.Mutex
	fetch      Fetch
	tracks     []music.Track
	seen       map[string]bool
	next       int
	done       bool
	generation uint64
	pending    *request
}

func New(fetch Fetch) *Catalog { return &Catalog{fetch: fetch, seen: make(map[string]bool)} }
func (c *Catalog) Snapshot() ([]music.Track, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]music.Track(nil), c.tracks...), c.done
}
func (c *Catalog) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	c.tracks = nil
	c.seen = make(map[string]bool)
	c.next = 0
	c.done = false
}

func (c *Catalog) LoadNext(ctx context.Context) error {
	c.mu.Lock()
	if c.done {
		c.mu.Unlock()
		return nil
	}
	if pending := c.pending; pending != nil {
		c.mu.Unlock()
		select {
		case <-pending.done:
			if err := ctx.Err(); err != nil {
				return err
			}
			return pending.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	start, generation := c.next, c.generation
	pending := &request{done: make(chan struct{})}
	c.pending = pending
	c.mu.Unlock()
	page, err := c.fetch(ctx, start, 50)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = nil
	defer func() { pending.err = err; close(pending.done) }()
	if generation != c.generation {
		err = errors.New("favorites request superseded")
		return err
	}
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if page.More && page.Next <= start {
		err = errors.New("favorites pagination made no progress")
		return err
	}
	added := 0
	for _, track := range page.Items {
		if !c.seen[track.ID] {
			c.seen[track.ID] = true
			c.tracks = append(c.tracks, track)
			added++
		}
	}
	if page.More && added == 0 {
		err = errors.New("favorites page contained no new IDs")
		return err
	}
	c.next = page.Next
	c.done = !page.More
	return nil
}

func (c *Catalog) LoadAll(ctx context.Context) error {
	for {
		_, done := c.Snapshot()
		if done {
			return nil
		}
		if err := c.LoadNext(ctx); err != nil {
			return err
		}
	}
}
