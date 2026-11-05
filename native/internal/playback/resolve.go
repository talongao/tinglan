// Package playback resolves a canonical row to a playable stream.
//
// The rule is fixed: probe the row's source bindings in import order, skip
// preview-only results while a full playback may still exist, and only fall
// back to a preview when every binding is preview-only. Unknown completeness is
// never treated as preview; it is kept as a last-resort candidate and reported
// as unknown.
package playback

import (
	"context"
	"errors"
	"fmt"

	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
)

// Lookup resolves a source id to its registered source.
type Lookup interface {
	Source(music.SourceID) (source.Source, bool)
}

// Attempt records one probe so the UI can explain why a source was skipped.
type Attempt struct {
	Source music.SourceID
	ID     string
	Access music.Access
	Err    error
}

// Result is the chosen stream plus the probing trail.
type Result struct {
	Playback music.Playback
	Link     music.SourceLink
	Access   music.Access
	Attempts []Attempt
}

// Preview reports whether the chosen stream is a preview-only excerpt.
func (r Result) Preview() bool { return r.Access == music.AccessPreview }

// ErrNoSource reports that no binding produced a stream.
var ErrNoSource = errors.New("playback: no source produced a stream")

// Resolver probes adapters for one canonical row.
type Resolver struct {
	lookup Lookup
}

// New returns a resolver over lookup.
func New(lookup Lookup) *Resolver { return &Resolver{lookup: lookup} }

// Resolve walks the row's bindings in import order.
func (r *Resolver) Resolve(ctx context.Context, track music.CanonicalTrack, quality string) (Result, error) {
	if r == nil || r.lookup == nil {
		return Result{}, ErrNoSource
	}
	links := track.SortedLinks()
	if len(links) == 0 {
		return Result{}, ErrNoSource
	}

	result := Result{}
	var unknown, preview *Result
	var lastErr error
	for _, link := range links {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if link.Disabled {
			result.Attempts = append(result.Attempts, Attempt{Source: link.Source, ID: link.TrackID, Err: fmt.Errorf("source %q binding is disabled", link.Source)})
			lastErr = source.NewError(link.Source, "resolve", source.KindUnsupported, errors.New("source binding is disabled"))
			continue
		}
		src, ok := r.lookup.Source(link.Source)
		if !ok {
			result.Attempts = append(result.Attempts, Attempt{Source: link.Source, ID: link.TrackID, Err: fmt.Errorf("source %q is not configured", link.Source)})
			lastErr = source.NewError(link.Source, "resolve", source.KindUnsupported, errors.New("source is not configured"))
			continue
		}
		playback, err := src.Library().Resolve(ctx, source.ResolveRequest{Track: link.Track(), Quality: quality})
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return Result{}, ctxErr
			}
			result.Attempts = append(result.Attempts, Attempt{Source: link.Source, ID: link.TrackID, Err: err})
			lastErr = err
			continue
		}
		access := playback.Access
		result.Attempts = append(result.Attempts, Attempt{Source: link.Source, ID: link.TrackID, Access: access})
		candidate := Result{Playback: playback, Link: link, Access: access, Attempts: result.Attempts}
		if access == music.AccessFull {
			// First full playback in import order wins.
			return candidate, nil
		}
		switch access {
		case music.AccessPreview:
			if preview == nil {
				saved := candidate
				preview = &saved
			}
		default:
			// Unknown completeness is not a preview: keep it ahead of a known
			// excerpt, but never ahead of a confirmed full playback.
			if unknown == nil {
				saved := candidate
				unknown = &saved
			}
		}
	}

	if unknown != nil {
		unknown.Attempts = result.Attempts
		return *unknown, nil
	}
	if preview != nil {
		preview.Attempts = result.Attempts
		return *preview, nil
	}
	if lastErr != nil {
		return result, lastErr
	}
	return result, ErrNoSource
}
