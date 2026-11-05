package canonical

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
)

type pageAdapter struct {
	// Only ID/Describe/Auth/Library are used by the import pipeline.
	source.Source
	id    music.SourceID
	pages []source.ImportPage
	calls int
}

func (a *pageAdapter) ID() music.SourceID { return a.id }

func (a *pageAdapter) Describe() source.Descriptor {
	return source.Descriptor{
		SchemaVersion: 1,
		ID:            a.id,
		Capabilities: source.Capabilities{
			Import: true, MaxPageSize: 50,
			Collections: []source.Collection{{Kind: "favorites", Name: "我的收藏"}},
		},
	}
}

func (a *pageAdapter) Auth() source.AuthSource { return pageAuth{} }

func (a *pageAdapter) Library() source.LibrarySource { return pageLibrary{a} }

type pageAuth struct{}

func (pageAuth) Status(context.Context) (source.AuthStatus, error) {
	return source.AuthStatus{State: source.AuthSignedIn}, nil
}
func (a pageAuth) Verify(ctx context.Context) (source.AuthStatus, error) { return a.Status(ctx) }
func (pageAuth) Login(context.Context, source.LoginRequest) (source.AuthResult, error) {
	return source.AuthResult{Succeeded: true}, nil
}
func (pageAuth) PersistSession(context.Context, source.PersistRequest) (bool, error) {
	return true, nil
}
func (pageAuth) Logout(context.Context) error { return nil }

type pageLibrary struct{ a *pageAdapter }

func (l pageLibrary) Import(context.Context, source.ImportRequest) (source.ImportPage, error) {
	if l.a.calls >= len(l.a.pages) {
		return source.ImportPage{}, errors.New("unexpected import call")
	}
	page := l.a.pages[l.a.calls]
	l.a.calls++
	return page, nil
}

func (l pageLibrary) Resolve(context.Context, source.ResolveRequest) (music.Playback, error) {
	return music.Playback{}, errors.New("not used")
}

type recordingJudge struct {
	score float64
	err   error
	calls int
}

func (j *recordingJudge) SameRecording(context.Context, music.Track, music.Track) (float64, error) {
	j.calls++
	return j.score, j.err
}

func track(id, title string, duration float64) music.Track {
	return music.Track{
		Source:   music.SourceHuawei,
		ID:       id,
		Title:    title,
		Artists:  []music.Artist{{ID: "a1", Name: "周杰伦"}},
		Album:    &music.Album{ID: "al1", Title: "七里香"},
		Duration: &duration,
	}
}

func netTrack(id, title string, duration float64) music.Track {
	track := track(id, title, duration)
	track.Source = music.SourceNetEase
	return track
}

func page(tracks ...music.Track) source.ImportPage {
	return source.ImportPage{Tracks: tracks}
}

// openForTest opens a library and closes it during cleanup.
func openForTest(t *testing.T, dir string) *Library {
	t.Helper()
	library, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = library.Close() })
	return library
}

func TestImportMergesAcrossSourcesAndPreservesOrder(t *testing.T) {
	dir := t.TempDir()
	library := openForTest(t, dir)
	first := &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{
		page(track("h1", "七里香", 299), track("h2", "晴天", 269)),
	}}
	if _, err := library.Import(context.Background(), first, source.Collection{}, nil, music.MatchPolicy{}, nil); err != nil {
		t.Fatal(err)
	}
	second := &pageAdapter{id: music.SourceNetEase, pages: []source.ImportPage{
		page(netTrack("n1", "七里香", 299), netTrack("n2", "以父之名", 342)),
	}}
	if _, err := library.Import(context.Background(), second, source.Collection{}, nil, music.MatchPolicy{}, nil); err != nil {
		t.Fatal(err)
	}
	rows := library.Snapshot()
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[0].Title != "七里香" || len(rows[0].Links) != 2 {
		t.Fatalf("first row = %+v", rows[0])
	}
	if rows[0].Links[0].Source != music.SourceHuawei || rows[0].Links[1].Source != music.SourceNetEase {
		t.Fatalf("links = %+v", rows[0].Links)
	}
	if rows[0].Links[0].ImportSeq >= rows[0].Links[1].ImportSeq {
		t.Fatalf("import seq not increasing: %+v", rows[0].Links)
	}
	if rows[1].Title != "晴天" || rows[2].Title != "以父之名" {
		t.Fatalf("order = %q, %q", rows[1].Title, rows[2].Title)
	}
}

func TestReimportRefreshesMetadataWithoutNewSeq(t *testing.T) {
	library := openForTest(t, t.TempDir())
	adapter := &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{page(track("h1", "七里香", 299))}}
	if _, err := library.Import(context.Background(), adapter, source.Collection{}, nil, music.MatchPolicy{}, nil); err != nil {
		t.Fatal(err)
	}
	before := library.ImportSeq()
	updated := track("h1", "七里香 (Remastered)", 300)
	adapter = &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{page(updated)}}
	if _, err := library.Import(context.Background(), adapter, source.Collection{}, nil, music.MatchPolicy{}, nil); err != nil {
		t.Fatal(err)
	}
	if library.ImportSeq() != before {
		t.Fatalf("import seq changed: %d -> %d", before, library.ImportSeq())
	}
	rows := library.Snapshot()
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	// The row keeps its display metadata; the binding records the fresh title.
	if rows[0].Title != "七里香" {
		t.Fatalf("row title = %q", rows[0].Title)
	}
	if len(rows[0].Links) != 1 || rows[0].Links[0].Title != "七里香 (Remastered)" {
		t.Fatalf("links = %+v", rows[0].Links)
	}
}

func TestJevOnlyCalledForGrayZone(t *testing.T) {
	library := openForTest(t, t.TempDir())
	adapter := &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{page(track("h1", "七里香", 299))}}
	if _, err := library.Import(context.Background(), adapter, source.Collection{}, nil, music.MatchPolicy{}, nil); err != nil {
		t.Fatal(err)
	}
	judge := &recordingJudge{score: 0.95}
	gray := track("n1", "七里香 重新编曲", 299)
	gray.Source = music.SourceNetEase
	adapter = &pageAdapter{id: music.SourceNetEase, pages: []source.ImportPage{page(gray)}}
	if _, err := library.Import(context.Background(), adapter, source.Collection{}, judge, music.MatchPolicy{}, nil); err != nil {
		t.Fatal(err)
	}
	if judge.calls != 1 {
		t.Fatalf("judge calls = %d, want 1", judge.calls)
	}
	if len(library.Snapshot()) != 1 {
		t.Fatalf("gray pair was not merged: %+v", library.Snapshot())
	}
}

func TestJudgeErrorKeepsRowsSeparate(t *testing.T) {
	library := openForTest(t, t.TempDir())
	adapter := &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{page(track("h1", "七里香", 299))}}
	if _, err := library.Import(context.Background(), adapter, source.Collection{}, nil, music.MatchPolicy{}, nil); err != nil {
		t.Fatal(err)
	}
	judge := &recordingJudge{err: errors.New("offline")}
	gray := track("n1", "七里香 重新编曲", 299)
	gray.Source = music.SourceNetEase
	adapter = &pageAdapter{id: music.SourceNetEase, pages: []source.ImportPage{page(gray)}}
	if _, err := library.Import(context.Background(), adapter, source.Collection{}, judge, music.MatchPolicy{}, nil); err != nil {
		t.Fatal(err)
	}
	if len(library.Snapshot()) != 2 {
		t.Fatalf("judge failure merged rows: %+v", library.Snapshot())
	}
}

func TestUnfavoriteKeepsCanonicalRowAndBindings(t *testing.T) {
	library := openForTest(t, t.TempDir())
	adapter := &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{page(track("h1", "七里香", 299))}}
	if _, err := library.Import(context.Background(), adapter, source.Collection{}, nil, music.MatchPolicy{}, nil); err != nil {
		t.Fatal(err)
	}
	rows := library.Snapshot()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	id := rows[0].ID
	if err := library.RemoveLink(music.SourceHuawei, "h1"); err != nil {
		t.Fatal(err)
	}
	if got := len(library.Snapshot()); got != 0 {
		t.Fatalf("favorites = %d, want 0 after un-favoriting", got)
	}
	row, ok := library.Row(id)
	if !ok {
		t.Fatal("canonical row dropped when un-favoriting")
	}
	if len(row.Links) != 1 {
		t.Fatalf("links = %d, want the source binding kept", len(row.Links))
	}
	if _, err := library.Add(context.Background(), track("h1", "七里香", 299), nil, music.MatchPolicy{}); err != nil {
		t.Fatal(err)
	}
	if got := len(library.Snapshot()); got != 1 {
		t.Fatalf("favorites = %d, want re-added", got)
	}
}

func TestRejectsNewerSchema(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(`{"schemaVersion":99,"tracks":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("newer schema accepted")
	}
}

func TestRejectsCorruptDocument(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(`{not json`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("corrupt document accepted")
	}
}

func TestFailedImportPublishesNothing(t *testing.T) {
	dir := t.TempDir()
	library := openForTest(t, dir)
	adapter := &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{
		{Tracks: []music.Track{track("h1", "七里香", 299)}, Next: "50"},
		{Tracks: []music.Track{track("h2", "晴天", 269)}, Next: "100"},
	}}
	if _, err := library.Import(context.Background(), adapter, source.Collection{}, nil, music.MatchPolicy{}, nil); err == nil {
		t.Fatal("expected the exhausted adapter to fail the import")
	}
	reopened := openForTest(t, dir)
	if reopened.Len() != 0 {
		t.Fatalf("failed import published %d rows", reopened.Len())
	}
	if reopened.ImportSeq() != 0 {
		t.Fatalf("failed import advanced seq to %d", reopened.ImportSeq())
	}
}

func TestImportSkipsInvalidTracks(t *testing.T) {
	library := openForTest(t, t.TempDir())
	bad := music.Track{Source: music.SourceHuawei, Title: "no id"}
	adapter := &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{page(bad, track("h1", "七里香", 299))}}
	result, err := library.Import(context.Background(), adapter, source.Collection{}, nil, music.MatchPolicy{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 1 || result.Added != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestSaveUsesRestrictedPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX file modes")
	}
	dir := filepath.Join(t.TempDir(), "profile")
	library := openForTest(t, dir)
	adapter := &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{page(track("h1", "七里香", 299))}}
	if _, err := library.Import(context.Background(), adapter, source.Collection{}, nil, music.MatchPolicy{}, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(dir, fileName)); !os.IsNotExist(err) {
		t.Fatalf("legacy file should be archived: %v", err)
	}
}

func TestDocumentUpdatedAt(t *testing.T) {
	library := openForTest(t, t.TempDir())
	library.now = func() time.Time { return time.Unix(100, 0) }
	adapter := &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{page(track("h1", "七里香", 299))}}
	if _, err := library.Import(context.Background(), adapter, source.Collection{}, nil, music.MatchPolicy{}, nil); err != nil {
		t.Fatal(err)
	}
	if library.doc.UpdatedAt.Unix() != 100 {
		t.Fatalf("updatedAt = %v", library.doc.UpdatedAt)
	}
}

func TestAddMergesSearchResultAndIsIdempotent(t *testing.T) {
	library := openForTest(t, t.TempDir())
	judge := &recordingJudge{score: .95}
	policy := music.DefaultMatchPolicy()
	if added, err := library.Add(context.Background(), track("hw-1", "夜曲", 226), judge, policy); err != nil || !added {
		t.Fatalf("first add = %v, %v", added, err)
	}
	if added, err := library.Add(context.Background(), netTrack("ne-1", "夜曲", 226), judge, policy); err != nil || !added {
		t.Fatalf("merge add = %v, %v", added, err)
	}
	if library.Len() != 1 {
		t.Fatalf("rows = %d, want 1", library.Len())
	}
	row, ok := library.RowForLink(music.SourceNetEase, "ne-1")
	if !ok || len(row.Links) != 2 {
		t.Fatalf("row = %+v", row)
	}
	if added, err := library.Add(context.Background(), track("hw-1", "夜曲", 226), judge, policy); err != nil || added {
		t.Fatalf("idempotent add = %v, %v", added, err)
	}
	if library.Len() != 1 {
		t.Fatalf("rows after refresh = %d", library.Len())
	}
}

func TestDisableSourceAndReimportReactivates(t *testing.T) {
	library := openForTest(t, t.TempDir())
	first := &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{page(track("hw-1", "夜曲", 226))}}
	if _, err := library.Import(context.Background(), first, source.Collection{}, nil, music.DefaultMatchPolicy(), nil); err != nil {
		t.Fatal(err)
	}
	if err := library.DisableSource(music.SourceHuawei); err != nil {
		t.Fatal(err)
	}
	row, ok := library.RowForLink(music.SourceHuawei, "hw-1")
	if !ok || len(row.Links) != 1 || !row.Links[0].Disabled {
		t.Fatalf("row after disable = %+v", row)
	}
	second := &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{page(track("hw-1", "夜曲", 226))}}
	if _, err := library.Import(context.Background(), second, source.Collection{}, nil, music.DefaultMatchPolicy(), nil); err != nil {
		t.Fatal(err)
	}
	row, _ = library.RowForLink(music.SourceHuawei, "hw-1")
	if row.Links[0].Disabled {
		t.Fatal("re-import did not reactivate binding")
	}
}

func TestFailedUpdateKeepsPublishedData(t *testing.T) {
	dir := t.TempDir()
	library := openForTest(t, dir)
	first := &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{
		{Tracks: []music.Track{track("h1", "七里香", 299), track("h2", "晴天", 269)}},
	}}
	if _, err := library.Import(context.Background(), first, source.Collection{}, nil, music.MatchPolicy{}, nil); err != nil {
		t.Fatal(err)
	}

	// A later snapshot fails halfway: nothing may be deleted or rewritten.
	second := &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{
		{Tracks: []music.Track{track("h1", "七里香", 299)}, Next: "50"},
	}}
	if _, err := library.Import(context.Background(), second, source.Collection{}, nil, music.MatchPolicy{}, nil); err == nil {
		t.Fatal("expected the exhausted adapter to fail the update")
	}
	if library.Len() != 2 {
		t.Fatalf("failed update changed the library: len = %d", library.Len())
	}
	for _, row := range library.Snapshot() {
		for _, link := range row.Links {
			if link.Disabled {
				t.Fatalf("failed update disabled %s", link.TrackID)
			}
		}
	}
}

func TestReimportDropsRemovedBindingButKeepsFavorite(t *testing.T) {
	dir := t.TempDir()
	library := openForTest(t, dir)
	first := &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{
		{Tracks: []music.Track{track("h1", "七里香", 299), track("h2", "晴天", 269)}},
	}}
	if _, err := library.Import(context.Background(), first, source.Collection{}, nil, music.MatchPolicy{}, nil); err != nil {
		t.Fatal(err)
	}
	second := &pageAdapter{id: music.SourceHuawei, pages: []source.ImportPage{
		{Tracks: []music.Track{track("h1", "七里香", 299)}},
	}}
	if _, err := library.Import(context.Background(), second, source.Collection{}, nil, music.MatchPolicy{}, nil); err != nil {
		t.Fatal(err)
	}
	if library.Len() != 2 {
		t.Fatalf("favorites shrank to %d rows", library.Len())
	}
	row, ok := library.RowForLink(music.SourceHuawei, "h2")
	if !ok {
		t.Fatal("removed track lost its canonical row")
	}
	if len(row.Links) != 1 || !row.Links[0].Disabled {
		t.Fatalf("stale binding should be unavailable: %+v", row.Links)
	}
	kept, ok := library.RowForLink(music.SourceHuawei, "h1")
	if !ok {
		t.Fatal("kept track disappeared")
	}
	if kept.Links[0].Disabled {
		t.Fatal("kept track became unavailable")
	}
}

// TestLegacyMigrationRaisesImportSeqHighWater checks that importing an old JSON
// document keeps its import_seq values and moves the clock past them, so a
// later import cannot reuse an already taken sequence number.
func TestLegacyMigrationRaisesImportSeqHighWater(t *testing.T) {
	dir := t.TempDir()
	doc := Document{SchemaVersion: SchemaVersion, ImportSeq: 960, Tracks: []music.CanonicalTrack{
		music.NewCanonical(track("h1", "七里香", 299), 5, time.Unix(100, 0)),
		music.NewCanonical(track("h2", "夜曲", 226), 960, time.Unix(101, 0)),
	}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), data, 0600); err != nil {
		t.Fatal(err)
	}
	library := openForTest(t, dir)
	defer library.Close()
	if library.Len() != 2 {
		t.Fatalf("len = %d", library.Len())
	}
	if got := library.ImportSeq(); got != 960 {
		t.Fatalf("import seq = %d, want 960", got)
	}
	adapter := &pageAdapter{id: music.SourceNetEase, pages: []source.ImportPage{page(netTrack("n1", "七里香", 299))}}
	if _, err := library.Import(context.Background(), adapter, source.Collection{}, nil, music.MatchPolicy{}, nil); err != nil {
		t.Fatal(err)
	}
	if got := library.ImportSeq(); got != 961 {
		t.Fatalf("import seq after import = %d, want 961", got)
	}
}
