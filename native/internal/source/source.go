// Package source defines the contract every music source implements.
//
// The contract is deliberately small: import one collection page, resolve one
// track to a playable stream, and describe capabilities. Anything a source
// cannot do must be reported as unsupported instead of silently degrading.
//
// Implementations live in their own packages (huawei today, netease later) and
// are the only places allowed to know provider payloads.
package source

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"tinglan/native/internal/music"
)

// Collection identifies one importable list on a source ("favorites").
type Collection struct {
	// Kind is a provider-neutral name such as "favorites". Providers that only
	// expose one list still report it so the import pipeline stays uniform.
	Kind string `json:"kind"`
	// ID is the provider-side identifier when one exists (playlist id, user id).
	ID string `json:"id,omitempty"`
	// Name is a display label supplied by the provider.
	Name string `json:"name,omitempty"`
	// PlaylistKind classifies a "playlist" collection as owned|favorite|chart.
	PlaylistKind music.PlaylistKind `json:"playlistKind,omitempty"`
}

// ImportRequest asks for one page of a collection.
type ImportRequest struct {
	Collection Collection
	// Cursor is the provider cursor returned by the previous page. Empty starts
	// from the beginning; callers must treat it as opaque.
	Cursor string
	Limit  int
	// RunID identifies the whole snapshot attempt. Sources only use it for
	// diagnostics; publishing is the caller's job.
	RunID string
}

// ImportPage is one page of imported metadata.
type ImportPage struct {
	Tracks []music.Track
	// Next is the cursor for the following page. Empty means the list ended.
	Next string
	// Partial reports that the page is usable but the provider signalled that
	// more data exists and the caller should retry to get it.
	Partial bool
	// RemoteDeleted reports that the source verified the collection no longer
	// exists. It is only valid on the last page of a normally completed run;
	// a network error or an empty page must not set it.
	RemoteDeleted bool
}

// Done reports whether the collection is exhausted.
func (p ImportPage) Done() bool { return p.Next == "" }

// ResolveRequest asks a source for a playable stream.
type ResolveRequest struct {
	Track music.Track
	// Quality is the user's preference ("standard", "lossless"). Sources may
	// return a lower quality, but must report what they actually delivered.
	Quality string
}

// Capabilities is static: it describes what the adapter can do, not whether it
// is currently logged in.
type Capabilities struct {
	Import   bool `json:"import"`
	Resolve  bool `json:"resolve"`
	Search   bool `json:"search"`
	Lyrics   bool `json:"lyrics"`
	Playlist bool `json:"playlist"`
	// Collections lists the importable collections this source exposes.
	Collections []Collection `json:"collections,omitempty"`
	// MaxPageSize is the largest limit Import accepts. Zero means unlimited.
	MaxPageSize int `json:"maxPageSize,omitempty"`
	// LoginKinds lists the authentication methods this source supports.
	LoginKinds []LoginKind `json:"loginKinds,omitempty"`
	// LoginRequired lists operations that need a signed-in account.
	LoginRequired []string `json:"loginRequired,omitempty"`
}

// Kind classifies failures so the UI and resolver can react without inspecting
// provider payloads.
type Kind string

const (
	KindLogin       Kind = "login"
	KindPermission  Kind = "permission"
	KindNotFound    Kind = "notfound"
	KindRateLimit   Kind = "ratelimit"
	KindTransient   Kind = "transient"
	KindUnsupported Kind = "unsupported"
	KindCanceled    Kind = "canceled"
	KindProtocol    Kind = "protocol"
	// Contract v1 additions: the shell reacts to these without provider types.
	KindAuthRequired Kind = "authRequired"
	KindUnavailable  Kind = "unavailable"
	KindInvalid      Kind = "invalid"
	KindNetwork      Kind = "network"
)

// Error is the only error type adapters return. Err is kept for logs but must
// never contain credentials or stream URLs.
type Error struct {
	Source music.SourceID
	Op     string
	Kind   Kind
	Err    error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s %s failed: %s", e.Source, e.Op, e.Kind)
}

func (e *Error) Unwrap() error { return e.Err }

// NewError wraps err with a kind. A nil err yields nil so call sites stay terse.
func NewError(source music.SourceID, op string, kind Kind, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Source: source, Op: op, Kind: kind, Err: err}
}

// KindOf reports the classified kind, or KindTransient for unknown errors.
func KindOf(err error) Kind {
	var target *Error
	if errors.As(err, &target) {
		return target.Kind
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return KindCanceled
	}
	return KindTransient
}

// Retryable reports whether a retry could plausibly succeed later.
func Retryable(err error) bool {
	switch KindOf(err) {
	case KindTransient, KindRateLimit:
		return true
	default:
		return false
	}
}

// Registry holds the configured adapters in the order the user wants them
// probed. It is safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
	sources map[music.SourceID]Source
	order   []music.SourceID
}

func NewRegistry() *Registry {
	return &Registry{sources: make(map[music.SourceID]Source)}
}

// RegisterSource adds a full-contract source. Legacy adapters and full sources
// share one id space so the shell can migrate call sites incrementally.
func (r *Registry) RegisterSource(src Source) error {
	if src == nil {
		return errors.New("source: nil source")
	}
	id := src.ID()
	if !id.Valid() {
		return fmt.Errorf("source: invalid source id %q", id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sources == nil {
		r.sources = make(map[music.SourceID]Source)
	}
	if _, exists := r.sources[id]; !exists {
		r.order = append(r.order, id)
	}
	r.sources[id] = src
	return nil
}

// UnregisterSource removes a source and its probe-order slot.
func (r *Registry) UnregisterSource(id music.SourceID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sources, id)
	for i, registered := range r.order {
		if registered == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

// Source returns the full-contract source for id.
func (r *Registry) Source(id music.SourceID) (Source, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	src, ok := r.sources[id]
	return src, ok
}

// Order returns the registered source ids in probe order.
func (r *Registry) Order() []music.SourceID {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]music.SourceID(nil), r.order...)
}

// Reorder applies a user-defined probe order. Unknown ids are ignored; sources
// missing from the list keep their relative order at the end.
func (r *Registry) Reorder(order []music.SourceID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := make(map[music.SourceID]bool, len(order))
	next := make([]music.SourceID, 0, len(r.order))
	for _, id := range order {
		if _, ok := r.sources[id]; !ok || seen[id] {
			continue
		}
		seen[id] = true
		next = append(next, id)
	}
	for _, id := range r.order {
		if !seen[id] {
			next = append(next, id)
		}
	}
	r.order = next
}

// SourceList returns registered contract sources in registration order.
func (r *Registry) SourceList() []Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Source, 0, len(r.order))
	for _, id := range r.order {
		if src, ok := r.sources[id]; ok {
			out = append(out, src)
		}
	}
	return out
}

// SortedIDs is a helper for deterministic UI rendering and tests.
func SortedIDs(ids []music.SourceID) []music.SourceID {
	out := append([]music.SourceID(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// SearchRequest asks one source for a page of search results.
type SearchRequest struct {
	Query  string
	Cursor string
	Limit  int
}
