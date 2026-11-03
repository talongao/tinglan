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
}

// ImportRequest asks for one page of a collection.
type ImportRequest struct {
	Collection Collection
	// Cursor is the provider cursor returned by the previous page. Empty starts
	// from the beginning; callers must treat it as opaque.
	Cursor string
	Limit  int
}

// ImportPage is one page of imported metadata.
type ImportPage struct {
	Tracks []music.Track
	// Next is the cursor for the following page. Empty means the list ended.
	Next string
	// Partial reports that the page is usable but the provider signalled that
	// more data exists and the caller should retry to get it.
	Partial bool
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

// Adapter is implemented once per music source.
//
// Implementations must be safe for concurrent use and must never persist or log
// stream URLs, credentials or provider payloads.
type Adapter interface {
	// ID is the stable identifier persisted in the local library.
	ID() music.SourceID
	// Capabilities describes what this source supports right now. It must be
	// static metadata: login state is reported by Login below, never here.
	Capabilities() Capabilities
	// Login reports whether the adapter currently holds a usable session. A
	// source that needs no login returns true.
	Login(ctx context.Context) (LoginState, error)
	// Import returns one page of a collection.
	Import(ctx context.Context, request ImportRequest) (ImportPage, error)
	// Resolve returns a playable stream for one track.
	Resolve(ctx context.Context, request ResolveRequest) (music.Playback, error)
}

// Capabilities is static: it describes what the adapter can do, not whether it
// is currently logged in.
type Capabilities struct {
	Import   bool
	Resolve  bool
	Search   bool
	Lyrics   bool
	Playlist bool
	// Collections lists the importable collections this source exposes.
	Collections []Collection
	// MaxPageSize is the largest limit Import accepts. Zero means unlimited.
	MaxPageSize int
}

// LoginState is the dynamic half of capability reporting.
type LoginState struct {
	LoggedIn bool
	// Account is a display label only (never a credential).
	Account string
	// ExpiresAt is zero when the source does not expose expiry.
	ExpiresAt string
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
	mu       sync.RWMutex
	adapters map[music.SourceID]Adapter
	order    []music.SourceID
}

func NewRegistry() *Registry {
	return &Registry{adapters: make(map[music.SourceID]Adapter)}
}

// Register adds an adapter. Registering the same id twice replaces the adapter
// but keeps its position, so a settings change cannot reorder playback probes.
func (r *Registry) Register(adapter Adapter) error {
	if adapter == nil {
		return errors.New("source: nil adapter")
	}
	id := adapter.ID()
	if !id.Valid() {
		return fmt.Errorf("source: invalid adapter id %q", id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.adapters[id]; !exists {
		r.order = append(r.order, id)
	}
	r.adapters[id] = adapter
	return nil
}

// Adapter returns the adapter for id.
func (r *Registry) Adapter(id music.SourceID) (Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[id]
	return adapter, ok
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
		if _, ok := r.adapters[id]; !ok || seen[id] {
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

// Adapters returns every registered adapter in probe order.
func (r *Registry) Adapters() []Adapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Adapter, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.adapters[id])
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

// SearchPage is one page of search results. Next is empty on the last page.
type SearchPage struct {
	Tracks []music.Track
	Next   string
}

// Searcher is implemented by adapters that can search their own catalogue.
// Search stays out of Adapter so a source can be import/resolve only.
type Searcher interface {
	Search(ctx context.Context, request SearchRequest) (SearchPage, error)
}

// Search runs one page against the adapter when it supports searching.
func Search(ctx context.Context, adapter Adapter, request SearchRequest) (SearchPage, error) {
	searcher, ok := adapter.(Searcher)
	if !ok {
		return SearchPage{}, NewError(adapter.ID(), "search", KindUnsupported, errors.New("source cannot search"))
	}
	return searcher.Search(ctx, request)
}
