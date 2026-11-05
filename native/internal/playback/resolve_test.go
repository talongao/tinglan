package playback

import (
	"context"
	"errors"
	"testing"

	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
)

type stubSource struct {
	// Embedded contract is never called directly by the resolver; only the
	// methods below are used.
	source.Source
	id      music.SourceID
	results map[string]music.Playback
	errs    map[string]error
	calls   []string
}

func (s *stubSource) ID() music.SourceID { return s.id }

func (s *stubSource) Describe() source.Descriptor {
	return source.Descriptor{SchemaVersion: 1, ID: s.id, Capabilities: source.Capabilities{Resolve: true}}
}

func (s *stubSource) Library() source.LibrarySource { return stubLibrary{s} }

type stubLibrary struct{ s *stubSource }

func (l stubLibrary) Import(context.Context, source.ImportRequest) (source.ImportPage, error) {
	return source.ImportPage{}, nil
}

func (l stubLibrary) Resolve(_ context.Context, req source.ResolveRequest) (music.Playback, error) {
	l.s.calls = append(l.s.calls, string(req.Track.Source)+":"+req.Track.ID)
	if err := l.s.errs[req.Track.ID]; err != nil {
		return music.Playback{}, err
	}
	return l.s.results[req.Track.ID], nil
}

type lookup map[music.SourceID]source.Source

func (l lookup) Source(id music.SourceID) (source.Source, bool) {
	src, ok := l[id]
	return src, ok
}

func link(sourceID music.SourceID, id string, seq int64) music.SourceLink {
	return music.SourceLink{Source: sourceID, TrackID: id, ImportSeq: seq, Title: "七里香", Artists: []music.Artist{{Name: "周杰伦"}}}
}

func row(links ...music.SourceLink) music.CanonicalTrack {
	return music.CanonicalTrack{ID: "row", Title: "七里香", Links: links}
}

func TestFirstFullInImportOrderWins(t *testing.T) {
	first := &stubSource{id: music.SourceHuawei, results: map[string]music.Playback{
		"h1": {URL: "https://a/1", Access: music.AccessFull, Quality: "lossless"},
	}}
	second := &stubSource{id: music.SourceNetEase, results: map[string]music.Playback{
		"n1": {URL: "https://b/1", Access: music.AccessFull, Quality: "standard"},
	}}
	resolver := New(lookup{music.SourceHuawei: first, music.SourceNetEase: second})
	result, err := resolver.Resolve(context.Background(), row(link(music.SourceHuawei, "h1", 1), link(music.SourceNetEase, "n1", 2)), "lossless")
	if err != nil {
		t.Fatal(err)
	}
	if result.Link.TrackID != "h1" || result.Access != music.AccessFull || result.Preview() {
		t.Fatalf("result = %+v", result)
	}
	if len(second.calls) != 0 {
		t.Fatalf("later source probed after a full playback: %v", second.calls)
	}
}

func TestPreviewSkippedForLaterFull(t *testing.T) {
	first := &stubSource{id: music.SourceHuawei, results: map[string]music.Playback{
		"h1": {URL: "https://a/1", Access: music.AccessPreview, Quality: "standard"},
	}}
	second := &stubSource{id: music.SourceNetEase, results: map[string]music.Playback{
		"n1": {URL: "https://b/1", Access: music.AccessFull, Quality: "standard"},
	}}
	resolver := New(lookup{music.SourceHuawei: first, music.SourceNetEase: second})
	result, err := resolver.Resolve(context.Background(), row(link(music.SourceHuawei, "h1", 1), link(music.SourceNetEase, "n1", 2)), "standard")
	if err != nil {
		t.Fatal(err)
	}
	if result.Link.TrackID != "n1" || result.Access != music.AccessFull {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Attempts) != 2 || result.Attempts[0].Access != music.AccessPreview {
		t.Fatalf("attempts = %+v", result.Attempts)
	}
}

func TestAllPreviewPlaysEarliest(t *testing.T) {
	first := &stubSource{id: music.SourceHuawei, results: map[string]music.Playback{
		"h1": {URL: "https://a/1", Access: music.AccessPreview},
	}}
	second := &stubSource{id: music.SourceNetEase, results: map[string]music.Playback{
		"n1": {URL: "https://b/1", Access: music.AccessPreview},
	}}
	resolver := New(lookup{music.SourceHuawei: first, music.SourceNetEase: second})
	result, err := resolver.Resolve(context.Background(), row(link(music.SourceHuawei, "h1", 1), link(music.SourceNetEase, "n1", 2)), "standard")
	if err != nil {
		t.Fatal(err)
	}
	if result.Link.TrackID != "h1" || !result.Preview() {
		t.Fatalf("result = %+v", result)
	}
}

func TestUnknownBeatsPreviewButNotFull(t *testing.T) {
	first := &stubSource{id: music.SourceHuawei, results: map[string]music.Playback{
		"h1": {URL: "https://a/1", Access: music.AccessPreview},
	}}
	second := &stubSource{id: music.SourceNetEase, results: map[string]music.Playback{
		"n1": {URL: "https://b/1", Access: music.AccessUnknown},
	}}
	resolver := New(lookup{music.SourceHuawei: first, music.SourceNetEase: second})
	result, err := resolver.Resolve(context.Background(), row(link(music.SourceHuawei, "h1", 1), link(music.SourceNetEase, "n1", 2)), "standard")
	if err != nil {
		t.Fatal(err)
	}
	if result.Link.TrackID != "n1" || result.Access != music.AccessUnknown || result.Preview() {
		t.Fatalf("result = %+v", result)
	}
}

func TestErrorsContinueAndAggregate(t *testing.T) {
	first := &stubSource{id: music.SourceHuawei, errs: map[string]error{"h1": source.NewError(music.SourceHuawei, "resolve", source.KindRateLimit, errors.New("slow down"))}}
	second := &stubSource{id: music.SourceNetEase, results: map[string]music.Playback{
		"n1": {URL: "https://b/1", Access: music.AccessFull},
	}}
	resolver := New(lookup{music.SourceHuawei: first, music.SourceNetEase: second})
	result, err := resolver.Resolve(context.Background(), row(link(music.SourceHuawei, "h1", 1), link(music.SourceNetEase, "n1", 2)), "standard")
	if err != nil {
		t.Fatal(err)
	}
	if result.Link.TrackID != "n1" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Attempts) != 2 || result.Attempts[0].Err == nil {
		t.Fatalf("attempts = %+v", result.Attempts)
	}
}

func TestAllFailReturnsError(t *testing.T) {
	first := &stubSource{id: music.SourceHuawei, errs: map[string]error{"h1": errors.New("boom")}}
	resolver := New(lookup{music.SourceHuawei: first})
	_, err := resolver.Resolve(context.Background(), row(link(music.SourceHuawei, "h1", 1)), "standard")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMissingAdapterReportsUnsupported(t *testing.T) {
	resolver := New(lookup{})
	_, err := resolver.Resolve(context.Background(), row(link(music.SourceHuawei, "h1", 1)), "standard")
	if source.KindOf(err) != source.KindUnsupported {
		t.Fatalf("err = %v", err)
	}
}

func TestEmptyRowIsNoSource(t *testing.T) {
	resolver := New(lookup{})
	if _, err := resolver.Resolve(context.Background(), row(), "standard"); !errors.Is(err, ErrNoSource) {
		t.Fatalf("err = %v", err)
	}
}

func TestImportOrderNotSourceOrder(t *testing.T) {
	// NetEase was imported first, so it must be probed first even though
	// Huawei is registered first in the lookup.
	net := &stubSource{id: music.SourceNetEase, results: map[string]music.Playback{
		"n1": {URL: "https://b/1", Access: music.AccessFull},
	}}
	huawei := &stubSource{id: music.SourceHuawei, results: map[string]music.Playback{
		"h1": {URL: "https://a/1", Access: music.AccessFull},
	}}
	resolver := New(lookup{music.SourceHuawei: huawei, music.SourceNetEase: net})
	result, err := resolver.Resolve(context.Background(), row(link(music.SourceNetEase, "n1", 1), link(music.SourceHuawei, "h1", 2)), "standard")
	if err != nil {
		t.Fatal(err)
	}
	if result.Link.TrackID != "n1" {
		t.Fatalf("result = %+v", result)
	}
}

func TestCancelStopsProbing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolver := New(lookup{music.SourceHuawei: &stubSource{id: music.SourceHuawei}})
	if _, err := resolver.Resolve(ctx, row(link(music.SourceHuawei, "h1", 1)), "standard"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestDisabledBindingIsSkipped(t *testing.T) {
	adapter := &stubSource{id: music.SourceHuawei, results: map[string]music.Playback{
		"h1": {URL: "https://a/1", Access: music.AccessFull},
		"h2": {URL: "https://a/2", Access: music.AccessFull},
	}}
	disabled := link(music.SourceHuawei, "h1", 1)
	disabled.Disabled = true
	resolver := New(lookup{music.SourceHuawei: adapter})
	result, err := resolver.Resolve(context.Background(), row(disabled, link(music.SourceHuawei, "h2", 2)), "standard")
	if err != nil {
		t.Fatal(err)
	}
	if result.Link.TrackID != "h2" || len(adapter.calls) != 1 || adapter.calls[0] != "huawei:h2" {
		t.Fatalf("result=%+v calls=%v", result, adapter.calls)
	}
}
