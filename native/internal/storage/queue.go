package storage

import (
	"errors"
	"os"
	"path/filepath"

	"tinglan/native/internal/huawei"
)

// Queue file names. The v2 document carries a stable track id plus the source
// bindings that produced the entry, so a queue survives a rename, a merge or a
// temporarily missing platform. Playback URLs are never written: they expire,
// they leak the account, and the resolver re-probes on every play.
const (
	queueFileName       = "queue.local.json"
	legacyQueueFileName = "queue.json"
	queueSchemaVersion  = 2
)

// SavedQueue is the in-memory queue. Callers keep using Tracks so the queue
// stays a plain list of playable rows; the v2 document is built around them.
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

// sourceForTrack reports the platform a transient queue entry came from. The
// queue only ever holds Huawei tracks today; the helper keeps the field honest
// if another adapter starts filling it.
func sourceForTrack(huawei.Track) string { return "huawei" }

// queueSourceRef names one platform binding of a queue entry.
type queueSourceRef struct {
	Source  string `json:"source"`
	TrackID string `json:"trackId"`
}

// queueItem is one v2 queue entry. Metadata is inlined so the row renders
// before any network call, and trackId may be empty for a transient search
// result that was never collected.
type queueItem struct {
	TrackID    string           `json:"trackId,omitempty"`
	Title      string           `json:"title"`
	Artists    []huawei.Artist  `json:"artists,omitempty"`
	Album      *huawei.Album    `json:"album,omitempty"`
	CoverURL   string           `json:"coverUrl,omitempty"`
	Duration   *float64         `json:"durationSeconds,omitempty"`
	OnlySource string           `json:"onlySource,omitempty"`
	Sources    []queueSourceRef `json:"sources,omitempty"`
}

type queueDocument struct {
	SchemaVersion int         `json:"schemaVersion"`
	Index         int         `json:"index"`
	Position      float64     `json:"positionSeconds"`
	Items         []queueItem `json:"items"`
}

func (s *Store) LoadQueue() (SavedQueue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var doc queueDocument
	if err := s.read(queueFileName, &doc); err == nil {
		value := SavedQueue{Index: doc.Index, Position: doc.Position}
		value.Tracks = tracksFromItems(doc.Items)
		value.Sanitize()
		return value, nil
	}
	var legacy SavedQueue
	err := s.read(legacyQueueFileName, &legacy)
	legacy.Sanitize()
	return legacy, err
}

func (s *Store) SaveQueue(value SavedQueue) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value.Sanitize()
	doc := queueDocument{SchemaVersion: queueSchemaVersion, Index: value.Index, Position: value.Position}
	doc.Items = itemsFromTracks(value.Tracks)
	if err := s.write(queueFileName, doc); err != nil {
		return err
	}
	// The legacy file is kept as a backup but never read again once a v2
	// document exists, so an old attach cannot resurrect a stale queue.
	legacy := filepath.Join(s.dir, legacyQueueFileName)
	if _, err := os.Stat(legacy); err == nil {
		_ = os.Rename(legacy, legacy+".migrated")
	}
	return nil
}

func (s *Store) ClearQueue() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	for _, name := range []string{queueFileName, legacyQueueFileName} {
		if err := os.Remove(filepath.Join(s.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func itemsFromTracks(tracks []huawei.Track) []queueItem {
	items := make([]queueItem, 0, len(tracks))
	for _, track := range tracks {
		item := queueItem{
			Title: track.Title, Artists: track.Artists, Album: track.Album,
			CoverURL: track.CoverURL, Duration: track.Duration, OnlySource: track.OnlySource,
		}
		// A collected row carries the stable local id; anything else keeps the
		// platform id as its single source ref so it can still be re-resolved.
		if track.Collected {
			item.TrackID = track.ID
		} else if track.ID != "" {
			item.Sources = []queueSourceRef{{Source: sourceForTrack(track), TrackID: track.ID}}
		}
		items = append(items, item)
	}
	return items
}

func tracksFromItems(items []queueItem) []huawei.Track {
	tracks := make([]huawei.Track, 0, len(items))
	for _, item := range items {
		id, collected := item.TrackID, true
		if id == "" && len(item.Sources) == 1 {
			id, collected = item.Sources[0].TrackID, false
		}
		if id == "" {
			continue
		}
		tracks = append(tracks, huawei.Track{
			ID: id, Title: item.Title, Artists: item.Artists, Album: item.Album,
			CoverURL: item.CoverURL, Duration: item.Duration,
			OnlySource: item.OnlySource, Collected: collected,
		})
	}
	return tracks
}
