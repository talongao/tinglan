// Package music holds provider-neutral track metadata shared by every source
// adapter, the local canonical library and the playback resolver.
//
// Nothing in this package may import a source adapter: adapters convert their
// provider payloads into these types, and the rest of the app only sees them.
package music

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

// SourceID identifies one provider instance ("huawei", "netease", ...).
type SourceID string

const (
	SourceHuawei  SourceID = "huawei"
	SourceNetEase SourceID = "netease"
)

// Valid reports whether the identifier can be persisted and used as a map key.
// String returns the stable source identifier used in settings and caches.
func (s SourceID) String() string { return string(s) }

func (s SourceID) Valid() bool {
	if s == "" || len(s) > 40 {
		return false
	}
	for _, r := range string(s) {
		if r <= ' ' || r == '/' || r == '\\' || r == ':' {
			return false
		}
	}
	return true
}

type Artist struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	CoverURL string `json:"coverUrl,omitempty"`
}

type Album struct {
	ID       string   `json:"id,omitempty"`
	Title    string   `json:"title"`
	CoverURL string   `json:"coverUrl,omitempty"`
	Artists  []Artist `json:"artists,omitempty"`
}

// Detail is a provider-neutral entity page: album, artist or playlist.
type Detail struct {
	ID       string
	Title    string
	CoverURL string
	Tracks   []Track
	Albums   []Album
	More     bool
	Next     int
}

// Page is one page of provider results with an opaque cursor. Catalog code
// keeps it neutral so no provider type leaks into the library index.
type Page struct {
	Items []Track
	Next  int
	More  bool
}

// PlaylistKind distinguishes user-owned, favorited and ranked lists on a source.
type PlaylistKind string

const (
	PlaylistOwned    PlaylistKind = "owned"
	PlaylistFavorite PlaylistKind = "favorite"
	PlaylistChart    PlaylistKind = "chart"
)

// Playlist is provider-neutral list metadata. Local playlists live in the
// library database; this type only describes lists a source can return.
type Playlist struct {
	Source      SourceID     `json:"source,omitempty"`
	ID          string       `json:"id"`
	Kind        PlaylistKind `json:"kind,omitempty"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	CoverURL    string       `json:"coverUrl,omitempty"`
	TrackCount  int          `json:"trackCount,omitempty"`
}

// Access describes how much of a track a source can actually deliver. It is
// deliberately three-state: a provider that cannot prove either way must not be
// reported as preview-only.
type Access uint8

const (
	AccessUnknown Access = iota
	AccessFull
	AccessPreview
)

func (a Access) String() string {
	switch a {
	case AccessFull:
		return "full"
	case AccessPreview:
		return "preview"
	default:
		return "unknown"
	}
}

func ParseAccess(text string) Access {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "full":
		return AccessFull
	case "preview":
		return AccessPreview
	default:
		return AccessUnknown
	}
}

// Version marks the performance/recording variant. Two tracks whose markers
// disagree (studio vs live) are never the same recording.
type Version struct {
	Live         bool `json:"live,omitempty"`
	Remix        bool `json:"remix,omitempty"`
	Acoustic     bool `json:"acoustic,omitempty"`
	Instrumental bool `json:"instrumental,omitempty"`
	Cover        bool `json:"cover,omitempty"`
	Demo         bool `json:"demo,omitempty"`
	// Edition separates releases that share a title but are different
	// recordings: a chorus cut, a re-recorded "rebirth" version, a remaster.
	// Two tracks may only merge when their editions match.
	Edition string `json:"edition,omitempty"`
}

func (v Version) IsZero() bool {
	return !v.Live && !v.Remix && !v.Acoustic && !v.Instrumental && !v.Cover && !v.Demo
}

// Key is a stable, order-independent representation of the markers.
func (v Version) Key() string {
	parts := make([]string, 0, 6)
	if v.Live {
		parts = append(parts, "live")
	}
	if v.Remix {
		parts = append(parts, "remix")
	}
	if v.Acoustic {
		parts = append(parts, "acoustic")
	}
	if v.Instrumental {
		parts = append(parts, "instrumental")
	}
	if v.Cover {
		parts = append(parts, "cover")
	}
	if v.Demo {
		parts = append(parts, "demo")
	}
	if v.Edition != "" {
		parts = append(parts, v.Edition)
	}
	if len(parts) == 0 {
		return "studio"
	}
	return strings.Join(parts, "+")
}

// Compatible reports whether two versions may describe the same recording.
func (v Version) Compatible(other Version) bool { return v.Key() == other.Key() }

// Track is provider-neutral metadata for one track on one source.
type Track struct {
	Source    SourceID `json:"source"`
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Artists   []Artist `json:"artists,omitempty"`
	Album     *Album   `json:"album,omitempty"`
	CoverURL  string   `json:"coverUrl,omitempty"`
	CoverURLs []string `json:"coverUrls,omitempty"`
	Duration  *float64 `json:"durationSeconds,omitempty"`
	Version   Version  `json:"version,omitempty"`
	RawSource string   `json:"-"` // provider payload never leaves the adapter

	// OnlySource restricts playback to one source binding. It is queue state,
	// not source metadata, so adapters must leave it empty.
	OnlySource SourceID `json:"onlySource,omitempty"`
	// Collected marks a row whose ID is the stable local canonical id rather
	// than a platform track id. Also queue state.
	Collected bool `json:"collected,omitempty"`
}

// CoverCandidates lists distinct covers, primary first, for the cover cache.
func (t Track) CoverCandidates() []string {
	seen := make(map[string]bool, len(t.CoverURLs)+1)
	out := make([]string, 0, len(t.CoverURLs)+1)
	for _, url := range append([]string{t.CoverURL}, t.CoverURLs...) {
		if url == "" || seen[url] {
			continue
		}
		seen[url] = true
		out = append(out, url)
	}
	return out
}

func (t Track) ArtistText() string {
	names := make([]string, 0, len(t.Artists))
	for _, artist := range t.Artists {
		if name := strings.TrimSpace(artist.Name); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "未知艺人"
	}
	return strings.Join(names, " / ")
}

func (t Track) Valid() bool {
	return t.Source.Valid() && strings.TrimSpace(t.ID) != "" && strings.TrimSpace(t.Title) != "" && len(t.ID) <= 200
}

// SourceLink records that one canonical track exists on one source, together
// with the order in which it was imported. ImportSeq is what the playback
// resolver follows: earlier import wins.
type SourceLink struct {
	Source     SourceID  `json:"source"`
	TrackID    string    `json:"trackId"`
	ImportSeq  int64     `json:"importSeq"`
	ImportedAt time.Time `json:"importedAt"`
	Access     Access    `json:"access"`
	Quality    string    `json:"quality,omitempty"`
	Title      string    `json:"title"`
	Artists    []Artist  `json:"artists,omitempty"`
	Album      *Album    `json:"album,omitempty"`
	CoverURL   string    `json:"coverUrl,omitempty"`
	Duration   *float64  `json:"durationSeconds,omitempty"`
	Version    Version   `json:"version,omitempty"`
	// Disabled marks a binding whose source account signed out. The local row
	// and its import order survive; a later import of the same source id
	// refreshes the binding and clears this flag.
	Disabled bool `json:"disabled,omitempty"`
}

// Link converts provider-neutral metadata into a persisted source link.
func Link(t Track, seq int64, now time.Time) SourceLink {
	return SourceLink{
		Source: t.Source, TrackID: t.ID, ImportSeq: seq, ImportedAt: now.UTC(),
		Title: t.Title, Artists: append([]Artist(nil), t.Artists...), Album: t.Album,
		CoverURL: t.CoverURL, Duration: t.Duration, Version: t.Version,
	}
}

func (l SourceLink) Track() Track {
	return Track{
		Source: l.Source, ID: l.TrackID, Title: l.Title, Artists: append([]Artist(nil), l.Artists...),
		Album: l.Album, CoverURL: l.CoverURL, Duration: l.Duration, Version: l.Version,
	}
}

func (l SourceLink) Valid() bool {
	return l.Source.Valid() && strings.TrimSpace(l.TrackID) != "" && strings.TrimSpace(l.Title) != ""
}

// CanonicalTrack is the local truth: one row in the favorites list, with one or
// more source links. Display metadata follows the first imported link and is
// never rewritten by a later source.
type CanonicalTrack struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Artists  []Artist     `json:"artists,omitempty"`
	Album    *Album       `json:"album,omitempty"`
	CoverURL string       `json:"coverUrl,omitempty"`
	Duration *float64     `json:"durationSeconds,omitempty"`
	Version  Version      `json:"version,omitempty"`
	Links    []SourceLink `json:"links"`
	// Favorite records whether the row is currently in 我的收藏. It is state,
	// not identity: un-favoriting keeps the canonical row and its bindings.
	Favorite  bool      `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CanonicalID derives a stable local identifier from the identity that first
// created the row, so re-importing the same favorites is idempotent.
func CanonicalID(t Track) string {
	sum := sha256.Sum256([]byte(string(t.Source) + "\x00" + t.ID))
	return hex.EncodeToString(sum[:16])
}

// NewCanonical creates a canonical row from the first source link.
func NewCanonical(t Track, seq int64, now time.Time) CanonicalTrack {
	stamp := now.UTC()
	return CanonicalTrack{
		ID: CanonicalID(t), Title: t.Title, Artists: append([]Artist(nil), t.Artists...),
		Album: t.Album, CoverURL: t.CoverURL, Duration: t.Duration, Version: t.Version,
		Links: []SourceLink{Link(t, seq, stamp)}, CreatedAt: stamp, UpdatedAt: stamp,
	}
}

func (c CanonicalTrack) Valid() bool {
	return strings.TrimSpace(c.ID) != "" && strings.TrimSpace(c.Title) != "" && len(c.Links) > 0
}

// Sources lists the distinct sources backing this row, in import order.
func (c CanonicalTrack) Sources() []SourceID {
	seen := make(map[SourceID]bool, len(c.Links))
	result := make([]SourceID, 0, len(c.Links))
	for _, link := range c.SortedLinks() {
		if !seen[link.Source] {
			seen[link.Source] = true
			result = append(result, link.Source)
		}
	}
	return result
}

func (c CanonicalTrack) Link(source SourceID) (SourceLink, bool) {
	for _, link := range c.Links {
		if link.Source == source {
			return link, true
		}
	}
	return SourceLink{}, false
}

// SortedLinks returns links ordered by import sequence (ties broken by source).
func (c CanonicalTrack) SortedLinks() []SourceLink {
	links := append([]SourceLink(nil), c.Links...)
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].ImportSeq != links[j].ImportSeq {
			return links[i].ImportSeq < links[j].ImportSeq
		}
		return links[i].Source < links[j].Source
	})
	return links
}

// PutLink adds or refreshes a link without changing its original import order.
// It reports whether the row changed.
func (c *CanonicalTrack) PutLink(link SourceLink, now time.Time) bool {
	for i := range c.Links {
		if c.Links[i].Source == link.Source && c.Links[i].TrackID == link.TrackID {
			// Re-import refreshes metadata only; the sequence number is stable.
			c.Links[i].Title = link.Title
			c.Links[i].Artists = append([]Artist(nil), link.Artists...)
			c.Links[i].Album = link.Album
			c.Links[i].CoverURL = link.CoverURL
			c.Links[i].Duration = link.Duration
			c.Links[i].Version = link.Version
			c.Links[i].Disabled = link.Disabled
			c.UpdatedAt = now.UTC()
			return true
		}
	}
	c.Links = append(c.Links, link)
	c.UpdatedAt = now.UTC()
	return true
}

// Track projects the canonical row back into a display track. The primary
// source is the earliest imported link, which is also the first playback probe.
func (c CanonicalTrack) Track() Track {
	links := c.SortedLinks()
	source := SourceID("")
	if len(links) > 0 {
		source = links[0].Source
	}
	return Track{
		Source: source, ID: c.ID, Title: c.Title, Artists: append([]Artist(nil), c.Artists...),
		Album: c.Album, CoverURL: c.CoverURL, Duration: c.Duration, Version: c.Version,
	}
}

// CoverCandidates lists distinct covers, primary first, for the cover cache.
func (c CanonicalTrack) CoverCandidates() []string {
	covers := make([]string, 0, len(c.Links)+1)
	covers = append(covers, c.CoverURL)
	for _, link := range c.SortedLinks() {
		covers = append(covers, link.CoverURL)
	}
	seen := make(map[string]bool, len(covers))
	result := make([]string, 0, len(covers))
	for _, raw := range covers {
		if raw == "" || seen[raw] {
			continue
		}
		seen[raw] = true
		result = append(result, raw)
	}
	return result
}

// Playback is transient: it must never be persisted in the library, queue or
// logs. The URL belongs to the source and is only handed to the player.
type Playback struct {
	URL       string   `json:"-"`
	Quality   string   `json:"quality,omitempty"`
	Access    Access   `json:"access"`
	Evidence  string   `json:"evidence,omitempty"`
	Duration  *float64 `json:"durationSeconds,omitempty"`
	LyricURL  string   `json:"-"`
	LyricText string   `json:"-"`
}

func (Playback) String() string   { return "[transient playback]" }
func (Playback) GoString() string { return "[transient playback]" }
